// Package auth signs the one admin in and out. Passwords are bcrypt hashes; a session is a random
// token in a cookie, and the database keeps only its sha256, so a leaked table signs nobody in.
// Every change made while signed in must carry the session's CSRF token.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"tisak/internal/store"
)

const (
	Cookie     = "tisak_session"
	CSRFHeader = "X-CSRF-Token"
	CSRFField  = "csrf"
	// MinPassword is the shortest password accepted, in letters.
	MinPassword = 10
)

var (
	ErrWrong     = errors.New("wrong username or password")
	ErrTooMany   = errors.New("too many tries")
	ErrShort     = errors.New("password too short")
	ErrNoSession = errors.New("not signed in")
)

func HashPassword(pw string) (string, error) {
	if utf8.RuneCountInString(pw) < MinPassword {
		return "", ErrShort
	}
	if len(pw) > 72 { // bcrypt reads no further; refuse rather than silently cut
		return "", errors.New("password longer than 72 bytes")
	}
	h, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(h), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummy is checked against when the username is unknown, so a wrong name takes as long as a wrong password.
var dummy, _ = bcrypt.GenerateFromPassword([]byte("never a real password"), 12)

type Manager struct {
	Store    *store.Store
	Lifetime time.Duration // how long a session lasts without use; default 30 days
	// TrustProxy reads the client's address and https from X-Forwarded-For and X-Forwarded-Proto.
	// Turn it on only behind a proxy that sets them.
	TrustProxy bool

	mu       sync.Mutex
	failures map[string][]time.Time
}

type ctxKey struct{}

// Who is the signed-in admin and their session, carried in the request's context.
type Who struct {
	Admin   store.Admin
	Session store.Session
}

// From gives the signed-in admin for a request that passed Require.
func From(ctx context.Context) (Who, bool) {
	w, ok := ctx.Value(ctxKey{}).(Who)
	return w, ok
}

func (m *Manager) lifetime() time.Duration {
	if m.Lifetime > 0 {
		return m.Lifetime
	}
	return 30 * 24 * time.Hour
}

// SignIn checks the password and starts a session. Five wrong tries from one address in
// fifteen minutes lock that address out for the rest of the fifteen minutes.
func (m *Manager) SignIn(w http.ResponseWriter, r *http.Request, username, password string) error {
	ip := m.ClientIP(r)
	if m.locked(ip) {
		return ErrTooMany
	}
	a, err := m.Store.AdminByUsername(r.Context(), strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		bcrypt.CompareHashAndPassword(dummy, []byte(password))
		m.fail(ip)
		return ErrWrong
	}
	if err != nil {
		return err
	}
	if !CheckPassword(a.PasswordHash, password) {
		m.fail(ip)
		return ErrWrong
	}
	m.clear(ip)
	return m.start(w, r, a.ID)
}

func (m *Manager) start(w http.ResponseWriter, r *http.Request, adminID int64) error {
	token, csrf := random(32), random(32)
	ses := store.Session{TokenHash: hash(token), AdminID: adminID, CSRF: csrf,
		UserAgent: trim(r.UserAgent(), 200), IP: m.ClientIP(r), ExpiresAt: time.Now().Add(m.lifetime())}
	if err := m.Store.CreateSession(r.Context(), ses); err != nil {
		return err
	}
	_ = m.Store.DeleteExpiredSessions(r.Context())
	m.setCookie(w, r, token, ses.ExpiresAt)
	return nil
}

// SignOut ends this session.
func (m *Manager) SignOut(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(Cookie); err == nil {
		if err := m.Store.DeleteSession(r.Context(), hash(c.Value)); err != nil {
			return err
		}
	}
	m.setCookie(w, r, "", time.Unix(0, 0))
	return nil
}

// Current finds the session the request's cookie belongs to, and keeps it alive.
func (m *Manager) Current(r *http.Request) (Who, error) {
	c, err := r.Cookie(Cookie)
	if err != nil || c.Value == "" {
		return Who{}, ErrNoSession
	}
	ses, err := m.Store.SessionByToken(r.Context(), hash(c.Value))
	if errors.Is(err, store.ErrNotFound) {
		return Who{}, ErrNoSession
	}
	if err != nil {
		return Who{}, err
	}
	a, err := m.Store.AdminByID(r.Context(), ses.AdminID)
	if errors.Is(err, store.ErrNotFound) {
		return Who{}, ErrNoSession
	}
	if err != nil {
		return Who{}, err
	}
	// refresh at most once an hour, not on every click
	if time.Since(ses.SeenAt) > time.Hour {
		ses.ExpiresAt = time.Now().Add(m.lifetime())
		_ = m.Store.TouchSession(r.Context(), ses.TokenHash, ses.ExpiresAt)
	}
	return Who{Admin: a, Session: ses}, nil
}

// Refresh sends the cookie again with a later expiry, so the browser keeps it as long as the session lasts.
func (m *Manager) Refresh(w http.ResponseWriter, r *http.Request, who Who) {
	if c, err := r.Cookie(Cookie); err == nil {
		m.setCookie(w, r, c.Value, who.Session.ExpiresAt)
	}
}

// Require lets only a signed-in admin through. Anyone else goes to the sign-in page.
// A request that changes something must also come from this site and carry the CSRF token.
func (m *Manager) Require(signIn string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who, err := m.Current(r)
		if errors.Is(err, ErrNoSession) {
			to := signIn
			if r.Method == http.MethodGet && r.Header.Get("HX-Request") == "" {
				to += "?dalje=" + url.QueryEscape(r.URL.RequestURI())
			}
			if r.Header.Get("HX-Request") != "" {
				w.Header().Set("HX-Redirect", to)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, to, http.StatusSeeOther)
			return
		}
		if err != nil {
			http.Error(w, "Greška na poslužitelju.", http.StatusInternalServerError)
			return
		}
		if !safe(r.Method) && (!m.SameOrigin(r) || !validCSRF(r, who.Session.CSRF)) {
			http.Error(w, "Zahtjev je odbijen: osvježi stranicu i pokušaj ponovo.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, who)))
	})
}

// SameOrigin refuses a change sent from another site. Browsers send Origin with every POST;
// a request without it (curl, an old browser) falls back to the CSRF token alone.
func (m *Manager) SameOrigin(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" {
		return true
	}
	u, err := url.Parse(o)
	return err == nil && strings.EqualFold(u.Host, r.Host)
}

func safe(method string) bool {
	return method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions
}

func validCSRF(r *http.Request, want string) bool {
	got := r.Header.Get(CSRFHeader)
	if got == "" {
		got = r.PostFormValue(CSRFField)
	}
	return got != "" && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (m *Manager) setCookie(w http.ResponseWriter, r *http.Request, value string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: Cookie, Value: value, Path: "/admin", Expires: expires,
		HttpOnly: true, Secure: m.https(r), SameSite: http.SameSiteLaxMode,
	})
}

func (m *Manager) https(r *http.Request) bool {
	return r.TLS != nil || m.TrustProxy && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// ClientIP is the address a request came from.
func (m *Manager) ClientIP(r *http.Request) string {
	if m.TrustProxy {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			parts := strings.Split(f, ",")
			return strings.TrimSpace(parts[len(parts)-1]) // the one our proxy saw; earlier ones can be made up
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- throttling ----------

const (
	maxFailures = 5
	failWindow  = 15 * time.Minute
)

func (m *Manager) recent(ip string) []time.Time {
	var keep []time.Time
	for _, t := range m.failures[ip] {
		if time.Since(t) < failWindow {
			keep = append(keep, t)
		}
	}
	return keep
}

func (m *Manager) locked(ip string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.recent(ip)) >= maxFailures
}

func (m *Manager) fail(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failures == nil {
		m.failures = map[string][]time.Time{}
	}
	if len(m.failures) > 10000 { // never let a flood of addresses grow this without end
		m.failures = map[string][]time.Time{}
	}
	m.failures[ip] = append(m.recent(ip), time.Now())
}

func (m *Manager) clear(ip string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.failures, ip)
}

// ---------- helpers ----------

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on a working system
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func trim(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
