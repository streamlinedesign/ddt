package site

import (
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"tisak/internal/store"
)

// How many messages one address may send, and in how long: enough for a reader, too few for a flood.
const (
	contactBurst  = 5
	contactWindow = time.Hour
	contactBytes  = 32 << 10
)

// contact takes a message from Kontakt's form, in any edition, and keeps it for the inbox.
func (s *Site) contact(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, contactBytes)
	if err := r.ParseForm(); err != nil {
		s.note(w, http.StatusBadRequest, "Poruka nije stigla", "Poruka je preduga. Skratite je i pošaljite ponovo.")
		return
	}
	field := func(name string) string { return strings.TrimSpace(r.PostFormValue(name)) }
	if field("website") != "" { // only robots fill the hidden field; they are thanked and forgotten
		s.note(w, http.StatusOK, "Hvala", "Poruka je stigla u uredništvo.")
		return
	}
	m := store.Message{Name: field("name"), Email: field("email"), Subject: field("subject"), Body: field("message")}
	if msg := checkMessage(m); msg != "" {
		s.note(w, http.StatusBadRequest, "Poruka nije stigla", msg+" Vratite se na izdanje i pokušajte ponovo.")
		return
	}
	m.IP = r.RemoteAddr
	if s.ClientIP != nil {
		m.IP = s.ClientIP(r)
	}
	n, err := s.Store.MessagesFrom(r.Context(), m.IP, time.Now().Add(-contactWindow))
	if s.fail(w, err) {
		return
	}
	if n >= contactBurst {
		s.note(w, http.StatusTooManyRequests, "Poruka nije stigla", "S ove adrese stiglo je već nekoliko poruka. Pokušajte ponovo za sat vremena.")
		return
	}
	if _, err := s.Store.SaveMessage(r.Context(), m); s.fail(w, err) {
		return
	}
	s.note(w, http.StatusOK, "Hvala", "Poruka je stigla u uredništvo. Ako treba, javit ćemo se na "+m.Email+".")
}

// checkMessage says what is wrong with a message, in a sentence, or nothing.
func checkMessage(m store.Message) string {
	long := func(s string, max int) bool { return utf8.RuneCountInString(s) > max }
	switch {
	case m.Name == "" || m.Email == "" || m.Body == "":
		return "Upišite ime, e-poštu i poruku."
	case long(m.Name, 100), long(m.Email, 200), long(m.Subject, 200), long(m.Body, 5000):
		return "Poruka je preduga."
	}
	if a, err := mail.ParseAddress(m.Email); err != nil || a.Address != m.Email {
		return "Adresa e-pošte ne izgleda ispravno."
	}
	return ""
}

func (s *Site) note(w http.ResponseWriter, code int, title, text string) {
	page, err := s.Press.RenderNote(title, text)
	if s.fail(w, err) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	w.Write(page)
}
