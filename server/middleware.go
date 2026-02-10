package server

import (
	"log"
	"net/http"
	"time"

	com "OnlySats/com"
)

// middleware for authorization
func (s *Server) requireAuth(minLevel int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := s.cfg.SessionStore.Get(r, "session")
		if err != nil {
			log.Printf("Session error: %v", err)
			http.Error(w, "Session error", http.StatusInternalServerError)
			return
		}

		authenticated, ok := session.Values["authenticated"].(bool)
		if !ok || !authenticated {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}

		level, ok := session.Values["level"].(int)
		if !ok || level > minLevel {
			http.Error(w, "Access denied", http.StatusForbidden)
			return
		}

		const idleSeconds = 30 * 60 // 30 minutes idle timeout

		last, _ := session.Values["lastActive"].(int64)
		now := time.Now().Unix()
		if last == 0 {
			session.Values["lastActive"] = now
			_ = session.Save(r, w) // best-effort
		} else if now-last > idleSeconds {
			// idle expired -> kill and redirect to login
			session.Options.MaxAge = -1
			_ = session.Save(r, w)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		} else {
			// refresh activity timestamp
			session.Values["lastActive"] = now
			_ = session.Save(r, w) // best-effort; ignore error to avoid breaking request
		}

		next.ServeHTTP(w, r)
	})
}

// processes login form submissions
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	// DB auth first
	user, level, ok, err := s.cfg.LocalStore.AuthenticateUser(r.Context(), username, password)
	if err != nil {
		http.Error(w, "Auth error", http.StatusInternalServerError)
		return
	}

	// Ephemeral admin fallback ONLY if no admin users exist
	if !ok && s.cfg.TempAdmin != nil {
		if lvl, eok := s.cfg.TempAdmin.Try(r.Context(), s.cfg.LocalStore, username, password); eok {
			user = "admin"
			level = lvl // 0
			ok = true
		}
	}

	if !ok {
		http.Error(w, "Invalid username or password", http.StatusUnauthorized)
		return
	}

	// Write session (regenerate + set values)
	if err := com.CookieLogin(s.cfg.SessionStore, w, r, user, level); err != nil {
		http.Error(w, "Session error", http.StatusInternalServerError)
		return
	}

	// Redirect based on user level
	if level == 0 {
		http.Redirect(w, r, "/local/admin", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/local/satdump", http.StatusSeeOther)
	}
}

// handleLogout clears the session and redirects to login
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	session, err := s.cfg.SessionStore.Get(r, "session")
	if err != nil {
		log.Printf("Session error during logout: %v", err)
	}

	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to clear session: %v", err)
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
