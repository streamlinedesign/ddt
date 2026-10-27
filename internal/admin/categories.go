package admin

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"tisak/internal/press"
	"tisak/internal/store"
)

type categoryRow struct {
	store.Category
	Sources int
	Layout  string // its label
}

func (a *Admin) categories(w http.ResponseWriter, r *http.Request) {
	a.categoriesPage(w, r, categoryForm{Category: store.Category{Layout: "row3", InMenu: true, Enabled: true}})
}

func (a *Admin) categoriesPage(w http.ResponseWriter, r *http.Request, form categoryForm) {
	cats, err := a.Store.Categories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	srcs, err := a.Store.Sources(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	count := map[int64]int{}
	for _, s := range srcs {
		count[s.CategoryID]++
	}
	labels := map[string]string{}
	for _, l := range press.Layouts() {
		labels[l.Name] = l.Label
	}
	rows := make([]categoryRow, len(cats))
	for i, c := range cats {
		rows[i] = categoryRow{c, count[c.ID], labels[c.Layout]}
	}
	a.render(w, "categories", "page", struct {
		view
		Rows    []categoryRow
		Layouts []press.Layout
		Form    categoryForm
	}{a.base(r, w, "Kategorije", "kategorije"), rows, press.Layouts(), form})
}

// categoryForm is a category as typed, with what is wrong with it.
type categoryForm struct {
	store.Category
	Errors map[string]string
}

var slugOK = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// readCategory reads and checks the category fields of a form.
func readCategory(r *http.Request, c store.Category) categoryForm {
	f := categoryForm{Category: c, Errors: map[string]string{}}
	f.Title = strings.TrimSpace(r.PostFormValue("title"))
	f.Slug = strings.TrimSpace(strings.ToLower(r.PostFormValue("slug")))
	f.Layout = r.PostFormValue("layout")
	f.LeadPicture = r.PostFormValue("lead_picture") != ""
	f.InMenu = r.PostFormValue("in_menu") != ""
	f.Enabled = r.PostFormValue("enabled") != ""
	if f.Slug == "" {
		f.Slug = Slugify(f.Title)
	}
	switch {
	case f.Title == "":
		f.Errors["title"] = "Upiši naslov."
	case len([]rune(f.Title)) > 60:
		f.Errors["title"] = "Najviše 60 znakova."
	}
	switch {
	case f.Slug == "":
		f.Errors["slug"] = "Upiši oznaku, npr. vijesti."
	case len(f.Slug) > 40 || !slugOK.MatchString(f.Slug):
		f.Errors["slug"] = "Samo mala slova bez kvačica, brojke i crtice, najviše 40 znakova."
	}
	known := false
	for _, l := range press.Layouts() {
		known = known || l.Name == f.Layout
	}
	if !known {
		f.Errors["layout"] = "Odaberi raspored."
	}
	return f
}

func (a *Admin) newCategory(w http.ResponseWriter, r *http.Request) {
	f := readCategory(r, store.Category{})
	if len(f.Errors) == 0 {
		_, err := a.Store.NewCategory(r.Context(), f.Category)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["slug"] = "Ta oznaka već postoji."
		} else if err != nil {
			a.fail(w, err)
			return
		} else {
			back(w, r, "/admin/kategorije", "ok", fmt.Sprintf("Dodana kategorija %s. Dodaj joj izvore.", f.Title))
			return
		}
	}
	a.categoriesPage(w, r, f)
}

func (a *Admin) category(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	c, err := a.Store.Category(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	a.categoryPage(w, r, categoryForm{Category: c})
}

func (a *Admin) categoryPage(w http.ResponseWriter, r *http.Request, f categoryForm) {
	srcs, err := a.Store.Sources(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	var mine []store.Source
	for _, s := range srcs {
		if s.CategoryID == f.ID {
			mine = append(mine, s)
		}
	}
	a.render(w, "categories", "edit", struct {
		view
		Form    categoryForm
		Layouts []press.Layout
		Sources []store.Source
	}{a.base(r, w, f.Title, "kategorije"), f, press.Layouts(), mine})
}

func (a *Admin) saveCategory(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	old, err := a.Store.Category(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	if err := r.ParseMultipartForm(maxUpload); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		back(w, r, r.URL.Path, "bad", "Slika je prevelika: najviše 2 MB.")
		return
	}
	f := readCategory(r, old)
	if hash, msg := a.upload(r, "flag", 480); msg != "" {
		f.Errors["flag"] = msg
	} else if hash != "" {
		f.Flag = hash
	} else if r.PostFormValue("remove_flag") != "" {
		f.Flag = ""
	}
	if len(f.Errors) == 0 {
		err := a.Store.UpdateCategory(r.Context(), f.Category)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["slug"] = "Ta oznaka već postoji."
		} else if err != nil {
			a.fail(w, err)
			return
		} else {
			back(w, r, r.URL.Path, "ok", "Spremljeno. Promjena vrijedi od sljedećeg tiska.")
			return
		}
	}
	a.categoryPage(w, r, f)
}

func (a *Admin) moveCategory(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	by := 1
	if r.PostFormValue("smjer") == "gore" {
		by = -1
	}
	if a.notFound(w, r, a.Store.MoveCategory(r.Context(), n, by)) {
		return
	}
	http.Redirect(w, r, "/admin/kategorije", http.StatusSeeOther)
}

func (a *Admin) deleteCategory(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	c, err := a.Store.Category(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	err = a.Store.DeleteCategory(r.Context(), n)
	if errors.Is(err, store.ErrInUse) {
		back(w, r, r.URL.Path[:strings.LastIndex(r.URL.Path, "/")], "bad",
			"Kategorija još ima izvore. Premjesti ih ili obriši, pa onda nju.")
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/kategorije", "ok", "Obrisana kategorija "+c.Title+".")
}

// Slugify makes an address-safe label from a title: "Hrvatski nogomet" gives "hrvatski-nogomet".
func Slugify(s string) string {
	repl := strings.NewReplacer("č", "c", "ć", "c", "đ", "d", "dž", "dz", "š", "s", "ž", "z",
		"Č", "c", "Ć", "c", "Đ", "d", "Š", "s", "Ž", "z")
	s = strings.ToLower(repl.Replace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}
