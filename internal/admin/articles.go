package admin

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"tisak/internal/store"
)

// Articles are written here, block by block: paragraphs, subheadings, quotes and pictures.
// One article can be a day's main article; one can be pinned for every day without its own.

// articlePicture is how wide an article's pictures are kept: the main column, full screen in the viewer.
const articlePicture = 1600

const maxBlocks = 80

var mediaHash = regexp.MustCompile(`^[0-9a-f]{64}$`)

var blockKinds = map[string]string{"p": "Odlomak", "h": "Podnaslov", "quote": "Citat", "img": "Slika"}

type articleForm struct {
	store.Article
	Errors map[string]string
}

func (a *Admin) articles(w http.ResponseWriter, r *http.Request) {
	list, err := a.Store.Articles(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, "articles", "page", struct {
		view
		Articles []store.Article
		Today    string
	}{a.base(r, w, "Članci", "clanci"), list, a.todayDate().Format("2006-01-02")})
}

func (a *Admin) newArticle(w http.ResponseWriter, r *http.Request) {
	title := strings.TrimSpace(r.PostFormValue("title"))
	switch {
	case title == "":
		back(w, r, "/admin/clanci", "bad", "Upiši naslov članka.")
		return
	case len([]rune(title)) > 200:
		back(w, r, "/admin/clanci", "bad", "Naslov je predug: najviše 200 znakova.")
		return
	}
	n, err := a.Store.NewArticle(r.Context(), store.Article{Title: title, Blocks: []store.Block{{Type: "p"}}})
	if err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, fmt.Sprintf("/admin/clanci/%d", n), "ok", "Članak je napravljen. Napiši ga, pa ga postavi za dan.")
}

func (a *Admin) article(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	x, err := a.Store.Article(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	a.articlePage(w, r, articleForm{Article: x, Errors: map[string]string{}})
}

func (a *Admin) articlePage(w http.ResponseWriter, r *http.Request, f articleForm) {
	day := ""
	if f.Day != nil {
		day = f.Day.Format("2006-01-02")
	}
	a.render(w, "articles", "edit", struct {
		view
		Form  articleForm
		Kinds map[string]string
		Day   string
		Today string
	}{a.base(r, w, f.Title, "clanci"), f, blockKinds, day, a.todayDate().Format("2006-01-02")})
}

// readArticle reads the editor: the head of the article and every block, in order.
// Pictures are stored as they arrive, so a form sent back with a mistake keeps them.
func (a *Admin) readArticle(r *http.Request, x store.Article) articleForm {
	f := articleForm{Article: x, Errors: map[string]string{}}
	f.Title = strings.TrimSpace(r.PostFormValue("title"))
	f.Description = strings.TrimSpace(r.PostFormValue("description"))
	f.Author = strings.TrimSpace(r.PostFormValue("author"))
	f.PictureAlt = strings.TrimSpace(r.PostFormValue("picture_alt"))
	f.PictureCredit = strings.TrimSpace(r.PostFormValue("picture_credit"))
	limit := func(field, value string, max int) {
		if len([]rune(value)) > max {
			f.Errors[field] = "Najviše " + strconv.Itoa(max) + " znakova."
		}
	}
	if f.Title == "" {
		f.Errors["title"] = "Upiši naslov."
	}
	limit("title", f.Title, 200)
	limit("description", f.Description, 600)
	limit("author", f.Author, 120)
	limit("picture_alt", f.PictureAlt, 300)
	limit("picture_credit", f.PictureCredit, 200)
	if r.PostFormValue("remove_picture") != "" {
		f.Picture = ""
	}
	if hash, msg := a.uploadUpTo(r, "picture", articlePicture, maxPhoto); msg != "" {
		f.Errors["picture"] = msg
	} else if hash != "" {
		f.Picture = hash
	}

	kinds, texts := r.PostForm["block_type"], r.PostForm["block_text"]
	captions, pictures := r.PostForm["block_caption"], r.PostForm["block_picture"]
	if len(texts) != len(kinds) || len(captions) != len(kinds) || len(pictures) != len(kinds) {
		f.Errors["blocks"] = "Obrazac je stigao nepotpun. Osvježi stranicu i pokušaj ponovo."
		return f
	}
	f.Blocks = nil
	for i, k := range kinds {
		if _, ok := blockKinds[k]; !ok {
			continue
		}
		b := store.Block{Type: k, Text: strings.TrimSpace(texts[i]), Caption: strings.TrimSpace(captions[i])}
		if k == "img" {
			b.Text = ""
			if mediaHash.MatchString(pictures[i]) {
				b.Picture = pictures[i]
			}
			if hash, msg := a.uploadUpTo(r, fmt.Sprintf("block_file_%d", i), articlePicture, maxPhoto); msg != "" {
				f.Errors[fmt.Sprintf("b%d", i)] = msg
			} else if hash != "" {
				b.Picture = hash
			}
		} else {
			b.Caption = ""
		}
		if len([]rune(b.Text)) > 10000 {
			f.Errors[fmt.Sprintf("b%d", i)] = "Najviše 10 000 znakova u jednom bloku."
		}
		if len([]rune(b.Caption)) > 300 {
			f.Errors[fmt.Sprintf("b%d", i)] = "Opis slike: najviše 300 znakova."
		}
		f.Blocks = append(f.Blocks, b)
	}
	return f
}

// saveArticle saves the editor. A button beside a block (move, remove) or below them (add) saves too,
// then does its one thing; the page comes back at that block.
func (a *Admin) saveArticle(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseMultipartForm(maxUpload); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		a.fail(w, err)
		return
	}
	x, err := a.Store.Article(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	f := a.readArticle(r, x)
	at := -1
	op, arg, _ := strings.Cut(r.PostFormValue("op"), ":")
	i, _ := strconv.Atoi(arg)
	switch op {
	case "add":
		if _, ok := blockKinds[arg]; ok && len(f.Blocks) < maxBlocks {
			f.Blocks = append(f.Blocks, store.Block{Type: arg})
			at = len(f.Blocks) - 1
		} else if len(f.Blocks) >= maxBlocks {
			f.Errors["blocks"] = fmt.Sprintf("Najviše %d blokova u članku.", maxBlocks)
		}
	case "up":
		if i > 0 && i < len(f.Blocks) {
			f.Blocks[i-1], f.Blocks[i] = f.Blocks[i], f.Blocks[i-1]
			at = i - 1
		}
	case "down":
		if i >= 0 && i+1 < len(f.Blocks) {
			f.Blocks[i], f.Blocks[i+1] = f.Blocks[i+1], f.Blocks[i]
			at = i + 1
		}
	case "del":
		if i >= 0 && i < len(f.Blocks) {
			f.Blocks = append(f.Blocks[:i], f.Blocks[i+1:]...)
			at = max(0, i-1)
		}
	}
	if len(f.Errors) > 0 {
		a.articlePage(w, r, f)
		return
	}
	if err := a.Store.UpdateArticle(r.Context(), f.Article); a.notFound(w, r, err) {
		return
	}
	to := fmt.Sprintf("/admin/clanci/%d", n)
	if at >= 0 {
		http.Redirect(w, r, fmt.Sprintf("%s#b%d", to, at), http.StatusSeeOther)
		return
	}
	back(w, r, to, "ok", "Spremljeno.")
}

// setArticleDay makes the article the main article of a day, or takes it off.
func (a *Admin) setArticleDay(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	to := fmt.Sprintf("/admin/clanci/%d", n)
	if r.PostFormValue("makni") != "" {
		if err := a.Store.SetArticleDay(r.Context(), n, nil); a.notFound(w, r, err) {
			return
		}
		back(w, r, to, "ok", "Članak više nije glavni ni za jedan dan.")
		return
	}
	day, ok := parseDay(r.PostFormValue("day"))
	if !ok {
		back(w, r, to, "bad", "Odaberi dan.")
		return
	}
	if err := a.Store.SetArticleDay(r.Context(), n, &day); a.notFound(w, r, err) {
		return
	}
	msg := "Glavni članak za " + shortDay(day)
	if printed, err := a.Store.HasEdition(r.Context(), day); err == nil && printed {
		msg += " Taj dan je već otisnut: da članak izađe, pretisni ga na stranici Danas."
	}
	back(w, r, to, "ok", msg)
}

func (a *Admin) pinArticle(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	pin := r.PostFormValue("pin") != ""
	if err := a.Store.PinArticle(r.Context(), n, pin); a.notFound(w, r, err) {
		return
	}
	msg := "Članak više nije prikvačen."
	if pin {
		msg = "Prikvačeno: ovaj je članak glavni svaki dan koji nema svoj."
	}
	back(w, r, fmt.Sprintf("/admin/clanci/%d", n), "ok", msg)
}

func (a *Admin) deleteArticle(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.DeleteArticle(r.Context(), n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/clanci", "ok", "Članak je obrisan. Izdanja u kojima je izašao ostaju kakva jesu.")
}
