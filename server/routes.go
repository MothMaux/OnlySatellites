package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"OnlySats/com/shared"
	"OnlySats/handlers"
)

func (s *Server) setupUpdateRoutes(r *mux.Router) {
	cd := time.Minute
	if settingVal, err := s.cfg.LocalStore.GetSetting(context.Background(), "update_cd"); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(settingVal), 10, 64); err == nil && n > 0 {
			cd = time.Duration(n) * time.Second
		}
	}

	upd := &handlers.UpdateHandler{
		Cfg:      s.cfg.AppConfig,
		Pass:     s.cfg.PassConfig,
		Cooldown: cd,
	}
	rpl := &handlers.RepopulateHandler{
		Cfg:      s.cfg.AppConfig,
		Pass:     s.cfg.PassConfig,
		Cooldown: time.Minute,
	}

	r.Handle("/api/update", upd).Methods("POST")
	r.Handle("/api/repopulate", s.requireAuth(3, rpl)).Methods("POST")
}

func (s *Server) setupMiscRoutes(r *mux.Router) {
	htmlFS := s.mustSubHTMLFS()
	partialFS := s.mustSubPFS()

	// Settings handler
	settings := &handlers.SettingsHandler{Store: s.cfg.LocalStore}
	r.Handle("/api/config/theme", s.requireAuth(1, http.HandlerFunc(settings.PostTheme))).Methods("POST")
	r.Handle("/local/api/settings", s.requireAuth(1, http.HandlerFunc(settings.PostSettings))).Methods("POST")
	r.Handle("/local/api/settings", s.requireAuth(1, http.HandlerFunc(settings.GetSettings))).Methods("GET")

	r.Handle("/local/configure-passes", s.requireAuth(1, s.serveEmbeddedHTML("template_editor.html", htmlFS))).Methods("GET")
	tapi := handlers.NewTemplatesAdminAPI(s.cfg.LocalStore)
	tapi.Register(r, s.requireAuth)

	// Hardware monitor handler
	hw := &handlers.HardwareHandler{
		Cfg:     s.cfg.AppConfig,
		Store:   s.cfg.LocalStore,
		Timeout: 3 * time.Second,
	}
	r.Handle("/local/api/hardware", s.requireAuth(3, hw)).Methods("GET")
	info := handlers.NewInfoHandler(s.cfg.StartTime)
	r.Handle("/local/api/info", info).Methods("GET")

	// CSS and admin routes
	r.Handle("/colors.css", &handlers.ColorsCSSHandler{Store: s.cfg.LocalStore})
	r.Handle("/local/stats", s.requireAuth(3, s.serveEmbeddedHTML("stats.html", htmlFS))).Methods("GET")
	r.Handle("/local/admin", s.requireAuth(1, s.serveEmbeddedHTML("admin-center.html", htmlFS))).Methods("GET")
	r.Handle("/local/admin/general", s.requireAuth(1, s.serveEmbeddedHTML("admin-gen.html", partialFS))).Methods("GET")
	r.Handle("/local/admin/net", s.requireAuth(1, s.serveEmbeddedHTML("admin-net.html", partialFS))).Methods("GET")
	r.Handle("/local/admin/storage", s.requireAuth(1, s.serveEmbeddedHTML("admin-stg.html", partialFS))).Methods("GET")
	r.Handle("/local/admin/satdump", s.requireAuth(1, s.serveEmbeddedHTML("admin-sat.html", partialFS))).Methods("GET")
	r.Handle("/local/admin/passes", s.requireAuth(1, s.serveEmbeddedHTML("admin-pss.html", partialFS))).Methods("GET")
	r.Handle("/local/admin/images", s.requireAuth(1, s.serveEmbeddedHTML("admin-img.html", partialFS))).Methods("GET")
	r.Handle("/local/api/disk-stats", s.requireAuth(3, http.HandlerFunc(handlers.ServeDiskStats(s.cfg.AppConfig.Paths.LiveOutputDir)))).Methods("GET")
	r.Handle("/local/api/rotate-pass", s.requireAuth(3, http.HandlerFunc(handlers.ServeRotatePass180(s.cfg.AppConfig.Paths.LiveOutputDir, s.cfg.AppConfig.Paths.ThumbnailDir)))).Methods("POST")

	// API endpoints
	r.Handle("/api/stats", s.requireAuth(3, http.HandlerFunc(s.handleStats))).Methods("GET")

	// About page configuration & read APIs
	about := &handlers.AboutHandler{Store: s.cfg.LocalStore}

	// Public about endpoints
	r.Handle("/api/about", http.HandlerFunc(about.Get)).Methods("GET")
	r.Handle("/api/about/body", http.HandlerFunc(about.GetBody)).Methods("GET")
	r.Handle("/api/about/images", http.HandlerFunc(about.ListImages)).Methods("GET")
	r.Handle("/api/about/meta", http.HandlerFunc(about.GetMeta)).Methods("GET")

	// Admin about endpoints
	r.Handle("/local/configure-about", s.requireAuth(1, s.serveEmbeddedHTML("about_editor.html", htmlFS))).Methods("GET")
	r.Handle("/local/api/about/body", s.requireAuth(1, http.HandlerFunc(about.PutBody))).Methods("PUT")
	r.Handle("/local/api/about/body", s.requireAuth(1, http.HandlerFunc(about.DeleteBody))).Methods("DELETE")
	r.Handle("/api/about/images/{id:[0-9]+}/raw", http.HandlerFunc(about.RawImage)).Methods("GET")
	r.Handle("/local/api/about/images/upload", s.requireAuth(1, http.HandlerFunc(about.UploadImage))).Methods("POST")
	r.Handle("/local/api/about/images/{id:[0-9]+}", s.requireAuth(1, http.HandlerFunc(about.UpdateImage))).Methods("PUT")
	r.Handle("/local/api/about/images/{id:[0-9]+}", s.requireAuth(1, http.HandlerFunc(about.DeleteImage))).Methods("DELETE")
	r.Handle("/local/api/about/meta/{key}", s.requireAuth(1, http.HandlerFunc(about.PutMeta))).Methods("PUT")
	r.Handle("/local/api/about/meta/{key}", s.requireAuth(1, http.HandlerFunc(about.DeleteMeta))).Methods("DELETE")

	// Users
	users := &handlers.UsersHandler{Store: s.cfg.LocalStore}

	r.Handle("/local/api/users", s.requireAuth(0, http.HandlerFunc(users.List))).Methods("GET")
	r.Handle("/local/api/users", s.requireAuth(0, http.HandlerFunc(users.Create))).Methods("POST")
	r.Handle("/local/api/users/{id:[0-9]+}", s.requireAuth(0, http.HandlerFunc(users.Delete))).Methods("DELETE")
	r.Handle("/local/api/users/{id:[0-9]+}/username", s.requireAuth(0, http.HandlerFunc(users.SetUsername))).Methods("PUT")
	r.Handle("/local/api/users/{id:[0-9]+}/level", s.requireAuth(0, http.HandlerFunc(users.SetLevel))).Methods("PUT")
	r.Handle("/local/api/users/{id:[0-9]+}/reset-password", s.requireAuth(0, http.HandlerFunc(users.ResetPassword))).Methods("POST")

	// Satdump config
	satdump := &handlers.SatdumpHandler{Store: s.cfg.LocalStore}

	r.Handle("/local/api/satdump", s.requireAuth(0, http.HandlerFunc(satdump.List))).Methods("GET")
	r.Handle("/local/api/satdump", s.requireAuth(0, http.HandlerFunc(satdump.Create))).Methods("POST")
	r.Handle("/local/api/satdump/{name}", s.requireAuth(0, http.HandlerFunc(satdump.Get))).Methods("GET")
	r.Handle("/local/api/satdump/{name}", s.requireAuth(0, http.HandlerFunc(satdump.Update))).Methods("PUT")
	r.Handle("/local/api/satdump/{name}", s.requireAuth(0, http.HandlerFunc(satdump.Delete))).Methods("DELETE")

	// Message Posting/Getting
	r.Handle("/local/messages-admin", s.requireAuth(1, s.serveEmbeddedHTML("messages.html", htmlFS))).Methods("GET")

	msgs := &handlers.MessagesHandler{Store: s.cfg.LocalStore}
	r.Handle("/api/messages", http.HandlerFunc(msgs.List)).Methods("GET")
	r.Handle("/api/messages/latest", http.HandlerFunc(msgs.Latest)).Methods("GET")
	r.Handle("/api/messages/{id:[0-9]+}", http.HandlerFunc(msgs.Get)).Methods("GET")
	r.Handle("/api/messages/{id:[0-9]+}/image", http.HandlerFunc(msgs.RawImage)).Methods("GET")
	r.Handle("/local/api/messages", s.requireAuth(1, http.HandlerFunc(msgs.Create))).Methods("POST")
	r.Handle("/local/api/messages/{id:[0-9]+}", s.requireAuth(1, http.HandlerFunc(msgs.Update))).Methods("PUT")
	r.Handle("/local/api/messages/{id:[0-9]+}", s.requireAuth(1, http.HandlerFunc(msgs.Delete))).Methods("DELETE")
	r.Handle("/messages/{id:[0-9]+}", s.serveEmbeddedHTML("message_viewer.html", htmlFS)).Methods("GET")
}

// handleStats returns server statistics
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	stats := map[string]interface{}{
		"startTime": s.cfg.StartTime.Unix(),
		"uptime":    time.Since(s.cfg.StartTime).Seconds(),
	}

	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("Failed to encode stats: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

func (s *Server) setupSatdumpRoutes(r *mux.Router) {
	htmlFS := s.mustSubHTMLFS()

	tmpl := template.Must(template.New("satdump.html").Funcs(template.FuncMap{
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	}).ParseFS(htmlFS, "satdump.html"))

	// cookie helpers
	const cookieName = "satdump_instance"
	const cookiePath = "/local/satdump"

	getActive := func(r *http.Request) (string, bool) {
		c, err := r.Cookie(cookieName)
		if err != nil {
			return "", false
		}
		v := strings.TrimSpace(c.Value)
		if v == "" {
			return "", false
		}
		// normalize: '+' => ' ', then unescape
		v = strings.ReplaceAll(v, "+", " ")
		if u, err := url.QueryUnescape(v); err == nil {
			v = strings.TrimSpace(u)
		}
		if v == "" {
			return "", false
		}
		return v, true
	}

	setActive := func(w http.ResponseWriter, name string) {
		name = strings.TrimSpace(name)
		http.SetCookie(w, &http.Cookie{
			Name:     cookieName,
			Value:    url.QueryEscape(name),
			Path:     cookiePath,
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
	}

	// resolving helpers
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " ")) }

	resolveByName := func(ctx context.Context, name string) (string, int, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", 0, fmt.Errorf("empty")
		}

		if row, err := s.cfg.LocalStore.GetSatdump(ctx, name); err == nil && row != nil {
			ip := strings.TrimSpace(row.Address)
			if ip == "" {
				ip = shared.GetHostIPv4()
			}
			port := row.Port
			if port == 0 {
				port = 8081
			}
			return ip, port, nil
		}
		want := norm(name)
		list, _ := s.cfg.LocalStore.ListSatdump(ctx)
		for _, sd := range list {
			if norm(sd.Name) == want {
				ip := strings.TrimSpace(sd.Address)
				if ip == "" {
					ip = shared.GetHostIPv4()
				}
				port := sd.Port
				if port == 0 {
					port = 8081
				}
				return ip, port, nil
			}
		}
		return "", 0, fmt.Errorf("not found: %s", name)
	}

	firstInstance := func(ctx context.Context) (string, bool) {
		list, err := s.cfg.LocalStore.ListSatdump(ctx)
		if err != nil || len(list) == 0 {
			return "", false
		}
		sort.Slice(list, func(i, j int) bool {
			return strings.ToLower(strings.TrimSpace(list[i].Name)) <
				strings.ToLower(strings.TrimSpace(list[j].Name))
		})
		return strings.TrimSpace(list[0].Name), true
	}

	resolveFromCookieOrFirst := func(w http.ResponseWriter, r *http.Request) (string, string, int, bool) {
		if n, ok := getActive(r); ok {
			if ip, port, err := resolveByName(r.Context(), n); err == nil {
				return n, ip, port, true
			}
		}
		if n, ok := firstInstance(r.Context()); ok {
			setActive(w, n)
			if ip, port, err := resolveByName(r.Context(), n); err == nil {
				return n, ip, port, true
			}
		}
		return "", "", 0, false
	}

	// Index: use cookie or first instance
	r.Handle("/local/satdump", s.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := getActive(r)
		if !ok {
			if first, ok2 := firstInstance(r.Context()); ok2 {
				setActive(w, first)
				name = first
			} else {
				http.Error(w, "No SatDump instances configured", http.StatusNotFound)
				return
			}
		}
		if _, _, err := resolveByName(r.Context(), name); err != nil {
			if first, ok2 := firstInstance(r.Context()); ok2 {
				setActive(w, first)
				name = first
			} else {
				http.Error(w, "No SatDump instances configured", http.StatusNotFound)
				return
			}
		}
		http.Redirect(w, r, "/local/satdump/"+url.PathEscape(name), http.StatusFound) // 302
	}))).Methods("GET")

	r.Handle("/local/satdump/live", s.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ip, port, ok := resolveFromCookieOrFirst(w, r); ok {
			handlers.SatdumpLive(ip, port).ServeHTTP(w, r)
			return
		}
		http.Error(w, "No SatDump instances configured", http.StatusNotFound)
	}))).Methods("GET")

	r.Handle("/local/satdump/html", s.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ip, port, ok := resolveFromCookieOrFirst(w, r); ok {
			handlers.SatdumpHTML(ip, port).ServeHTTP(w, r)
			return
		}
		http.Error(w, "No SatDump instances configured", http.StatusNotFound)
	}))).Methods("GET")

	// match name
	r.Handle("/local/satdump/{name:[^/.]+}", s.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := mux.Vars(r)["name"]
		if u, err := url.PathUnescape(name); err == nil {
			name = u
		}
		if _, _, err := resolveByName(r.Context(), name); err != nil {
			http.Error(w, "Unknown SatDump instance", http.StatusNotFound)
			return
		}
		setActive(w, name)

		// Defaults (refresh=500ms, duration=5min)
		rateMS := 500
		spanSec := 300

		if v, _ := s.cfg.LocalStore.GetSetting(r.Context(), "satdump_rate"); strings.TrimSpace(v) != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				rateMS = n
			}
		}
		if v, _ := s.cfg.LocalStore.GetSetting(r.Context(), "satdump_span"); strings.TrimSpace(v) != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				spanSec = n
			}
		}

		data := map[string]any{
			"Title":         fmt.Sprintf("SatDump: %s", name),
			"StatusHTML":    template.HTML(""),
			"ApiDataJSON":   "",
			"SatdumpRateMS": rateMS,
			"SatdumpSpanMS": spanSec * 1000,
		}

		if err := tmpl.Execute(w, data); err != nil {
			log.Printf("Template rendering failed for satdump: %v", err)
			http.Error(w, "Template rendering failed", http.StatusInternalServerError)
			return
		}
	}))).Methods("GET")

	// asset proxy
	r.PathPrefix("/local/satdump/").Handler(s.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ip, port, ok := resolveFromCookieOrFirst(w, r); ok {
			r2 := r.Clone(r.Context())
			r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/local/satdump")
			if r2.URL.Path == "" {
				r2.URL.Path = "/"
			}
			handlers.SatdumpAssetProxy(ip, port).ServeHTTP(w, r2)
			return
		}
		http.Error(w, "No SatDump instances configured", http.StatusNotFound)
	})))

	ah := &handlers.SatdumpHandler{Store: s.cfg.LocalStore, AnalDB: s.cfg.AnalDB}
	r.Handle("/api/satdump/names", http.HandlerFunc(ah.Names)).Methods("GET")
	r.Handle("/api/analytics/tracks", http.HandlerFunc(ah.PolarPlot)).Methods("GET")
	r.Handle("/api/analytics/decoder", http.HandlerFunc(ah.GEOProgress)).Methods("GET")
}
