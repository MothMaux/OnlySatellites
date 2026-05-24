package com

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"OnlySats/com/shared"

	"github.com/gorilla/sessions"
)

// Session keys (signed+encrypted)

type SessionKeys struct {
	Auth []byte `json:"auth"` // HMAC, 32-64 bytes
	Enc  []byte `json:"enc"`  // AES-256, exactly 32 bytes
}

func randBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return b
}

func LoadOrGenerateSessionKeys(persistDir string) (SessionKeys, error) {
	// Environment wins
	if ak := os.Getenv("SESSION_AUTH_KEY"); ak != "" {
		if ek := os.Getenv("SESSION_ENC_KEY"); ek != "" {
			return SessionKeys{Auth: []byte(ak), Enc: []byte(ek)}, nil
		}
		return SessionKeys{Auth: []byte(ak), Enc: randBytes(32)}, nil
	}

	// Optional disk persistence
	if persistDir != "" {
		keyPath := filepath.Join(persistDir, "session_keys.json")
		if f, err := os.Open(keyPath); err == nil {
			defer f.Close()
			var k SessionKeys
			if json.NewDecoder(f).Decode(&k) == nil && len(k.Auth) >= 32 && len(k.Enc) == 32 {
				return k, nil
			}
		}
	}

	// 3) Generate fresh keys in-memory unless set to persist
	k := SessionKeys{
		Auth: randBytes(64),
		Enc:  randBytes(32),
	}

	// Persist to disk if requested
	if persistDir != "" {
		_ = os.MkdirAll(persistDir, 0o755)
		keyPath := filepath.Join(persistDir, "session_keys.json")
		if f, err := os.Create(keyPath); err == nil {
			_ = json.NewEncoder(f).Encode(k)
			_ = f.Close()
		}
	}
	return k, nil
}

// build a hardened CookieStore.
// Set secure=true in production (HTTPS). maxAgeSeconds is absolute lifetime.
func NewCookieStore(keys SessionKeys, secure bool, maxAgeSeconds int) *sessions.CookieStore {
	store := sessions.NewCookieStore(keys.Auth, keys.Enc)
	store.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   maxAgeSeconds,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
	return store
}

// drop undecodable/legacy cookies and returns a fresh session instead of erroring.
func GetSessionOrReset(store *sessions.CookieStore, w http.ResponseWriter, r *http.Request) (*sessions.Session, error) {
	s, err := store.Get(r, "session")
	if err == nil {
		return s, nil
	}
	// Clear bad cookie
	s = sessions.NewSession(store, "session")
	s.Options = store.Options
	s.Options.MaxAge = -1
	_ = s.Save(r, w)
	// Return a brand-new empty session
	return store.New(r, "session")
}

// RegenerateSession destroys the old session and returns a fresh one (anti-fixation).
func RegenerateSession(store *sessions.CookieStore, w http.ResponseWriter, r *http.Request) (*sessions.Session, error) {
	old, _ := store.Get(r, "session")
	old.Options.MaxAge = -1
	_ = old.Save(r, w)
	return store.New(r, "session")
}

// Security headers middleware
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		// Enable HSTS only when serving HTTPS
		w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
		next.ServeHTTP(w, r)
	})
}

// Ephemeral Admin Bootstrap
type EphemeralAdmin struct {
	Username string
	Password string
	enabled  bool
}

// returns true if there is any user with level <= 1 in DB.
func hasAdmins(ctx context.Context, db *sql.DB) (bool, error) {
	var n int64
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE level <= 1`).Scan(&n)
	return n > 0, err
}

// create an in-memory admin ("admin"/random) ONLY if there are no level<=1 users.
func NewEphemeralAdminIfNoAdmins(ctx context.Context, db *sql.DB) (*EphemeralAdmin, error) {
	ok, err := hasAdmins(ctx, db)
	if err != nil {
		return nil, err
	}
	if ok {
		return &EphemeralAdmin{enabled: false}, nil
	}
	return &EphemeralAdmin{
		Username: "admin",
		Password: shared.GenerateRandomPassword(12),
		enabled:  true,
	}, nil
}

// Try authenticates against the ephemeral admin, but ONLY if there are still no level<=1 users in DB.
func (e *EphemeralAdmin) Try(ctx context.Context, db *sql.DB, username, password string) (level int, ok bool) {
	if e == nil || !e.enabled {
		return 0, false
	}
	has, err := hasAdmins(ctx, db)
	if err != nil || has {
		// Disable once a real admin exists (or on error to be safe)
		e.enabled = false
		return 0, false
	}
	if username == e.Username && password == e.Password {
		return 0, true
	}
	return 0, false
}

// turns off the ephemeral admin after creating a real admin.
func (e *EphemeralAdmin) Disable() { e.enabled = false }

// enforces an idle timeout (seconds). Returns false if expired and clears the cookie.
func RefreshIdle(store *sessions.CookieStore, w http.ResponseWriter, r *http.Request, idleSeconds int64) (alive bool) {
	s, err := GetSessionOrReset(store, w, r)
	if err != nil {
		return false
	}
	last, _ := s.Values["lastActive"].(int64)
	now := time.Now().Unix()
	if last == 0 {
		s.Values["lastActive"] = now
		_ = s.Save(r, w)
		return true
	}
	if now-last > idleSeconds {
		s.Options.MaxAge = -1
		_ = s.Save(r, w)
		return false
	}
	s.Values["lastActive"] = now
	_ = s.Save(r, w)
	return true
}

// helpers for login

// write standard claims into the session
func CookieLogin(store *sessions.CookieStore, w http.ResponseWriter, r *http.Request, username string, level int) error {
	s, _ := RegenerateSession(store, w, r)
	s.Values["authenticated"] = true
	s.Values["username"] = username
	s.Values["level"] = level
	s.Values["lastActive"] = time.Now().Unix()
	return s.Save(r, w)
}

// clear the session cookie
func CookieLogout(store *sessions.CookieStore, w http.ResponseWriter, r *http.Request) error {
	s, err := store.Get(r, "session")
	if err != nil {
		return err
	}
	s.Options.MaxAge = -1
	return s.Save(r, w)
}

// RequireAuthQuick checks cookie claims locally (optional)
func RequireAuthQuick(store *sessions.CookieStore, r *http.Request, minLevel int) (string, int, error) {
	s, err := store.Get(r, "session")
	if err != nil {
		return "", 0, err
	}
	ok, _ := s.Values["authenticated"].(bool)
	if !ok {
		return "", 0, errors.New("unauthenticated")
	}
	level, ok := s.Values["level"].(int)
	if !ok || level > minLevel {
		return "", 0, errors.New("forbidden")
	}
	user, _ := s.Values["username"].(string)
	return user, level, nil
}
