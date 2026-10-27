package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"tisak/internal/auth"
	"tisak/internal/store"
)

// ---------- signing in and out ----------

func (a *Admin) signInPage(w http.ResponseWriter, r *http.Request) {
	if _, err := a.Auth.Current(r); err == nil {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}
	a.signInForm(w, r, "", "")
}

func (a *Admin) signInForm(w http.ResponseWriter, r *http.Request, username, problem string) {
	has, err := a.Store.HasAdmin(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, "signin", "page", struct {
		view
		Username, Problem, Next string
		NoAdmin                 bool
	}{view{Title: "Prijava", Flash: takeFlash(w, r)}, username, problem, next(r), !has})
}

func (a *Admin) signIn(w http.ResponseWriter, r *http.Request) {
	if !a.Auth.SameOrigin(r) {
		http.Error(w, "Zahtjev je odbijen.", http.StatusForbidden)
		return
	}
	username := r.PostFormValue("username")
	err := a.Auth.SignIn(w, r, username, r.PostFormValue("password"))
	switch {
	case errors.Is(err, auth.ErrWrong):
		a.signInForm(w, r, username, "Krivo ime ili lozinka.")
	case errors.Is(err, auth.ErrTooMany):
		a.signInForm(w, r, username, "Previše pokušaja. Pričekaj 15 minuta.")
	case err != nil:
		a.fail(w, err)
	default:
		a.Log.Printf("admin: %s signed in from %s", strings.TrimSpace(username), a.Auth.ClientIP(r))
		http.Redirect(w, r, next(r), http.StatusSeeOther)
	}
}

// next is where to go after signing in: back where the admin was headed, but only within the dashboard.
func next(r *http.Request) string {
	to := r.FormValue("dalje")
	if strings.HasPrefix(to, "/admin/") && !strings.HasPrefix(to, "//") && !strings.Contains(to, "\\") &&
		!strings.HasPrefix(to, signIn) {
		return to
	}
	return "/admin/"
}

func (a *Admin) signOut(w http.ResponseWriter, r *http.Request) {
	if err := a.Auth.SignOut(w, r); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, signIn, "ok", "Odjavljen si.")
}

// ---------- the account ----------

type accountForm struct {
	Username string
	Errors   map[string]string
}

func (a *Admin) account(w http.ResponseWriter, r *http.Request) {
	who, _ := auth.From(r.Context())
	a.accountPage(w, r, accountForm{Username: who.Admin.Username})
}

func (a *Admin) accountPage(w http.ResponseWriter, r *http.Request, f accountForm) {
	who, _ := auth.From(r.Context())
	sessions, err := a.Store.Sessions(r.Context(), who.Admin.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, "account", "page", struct {
		view
		Form     accountForm
		Sessions []store.Session
		This     string
		Min      int
	}{a.base(r, w, "Račun", "racun"), f, sessions, who.Session.TokenHash, auth.MinPassword})
}

// saveAccount changes the name or the password. Either needs the current password,
// and a new password signs out every other session.
func (a *Admin) saveAccount(w http.ResponseWriter, r *http.Request) {
	who, _ := auth.From(r.Context())
	f := accountForm{Username: strings.TrimSpace(r.PostFormValue("username")), Errors: map[string]string{}}
	current, pw, pw2 := r.PostFormValue("current"), r.PostFormValue("password"), r.PostFormValue("password2")

	switch {
	case f.Username == "":
		f.Errors["username"] = "Upiši ime."
	case utf8.RuneCountInString(f.Username) > 40 || strings.ContainsAny(f.Username, " \t"):
		f.Errors["username"] = "Do 40 znakova, bez razmaka."
	}
	if !auth.CheckPassword(who.Admin.PasswordHash, current) {
		f.Errors["current"] = "Trenutna lozinka nije točna."
	}
	hash := who.Admin.PasswordHash
	if pw != "" || pw2 != "" {
		switch {
		case pw != pw2:
			f.Errors["password2"] = "Lozinke se ne podudaraju."
		default:
			h, err := auth.HashPassword(pw)
			if errors.Is(err, auth.ErrShort) {
				f.Errors["password"] = fmt.Sprintf("Barem %d znakova.", auth.MinPassword)
			} else if err != nil {
				f.Errors["password"] = err.Error()
			}
			hash = h
		}
	}
	if len(f.Errors) > 0 {
		a.accountPage(w, r, f)
		return
	}
	err := a.Store.UpdateAdmin(r.Context(), who.Admin.ID, f.Username, hash)
	if errors.Is(err, store.ErrTaken) {
		f.Errors["username"] = "To ime je zauzeto."
		a.accountPage(w, r, f)
		return
	}
	if err != nil {
		a.fail(w, err)
		return
	}
	msg := "Spremljeno."
	if hash != who.Admin.PasswordHash {
		n, err := a.Store.DeleteOtherSessions(r.Context(), who.Admin.ID, who.Session.TokenHash)
		if err != nil {
			a.fail(w, err)
			return
		}
		msg = "Lozinka je promijenjena."
		if n > 0 {
			msg += fmt.Sprintf(" Odjavljeno ostalih prijava: %d.", n)
		}
		a.Log.Printf("admin: password changed from %s", a.Auth.ClientIP(r))
	}
	back(w, r, "/admin/racun", "ok", msg)
}

func (a *Admin) signOutOthers(w http.ResponseWriter, r *http.Request) {
	who, _ := auth.From(r.Context())
	n, err := a.Store.DeleteOtherSessions(r.Context(), who.Admin.ID, who.Session.TokenHash)
	if err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/racun", "ok", fmt.Sprintf("Odjavljeno ostalih prijava: %d.", n))
}
