package main

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"

	com "OnlySats/com"
	"OnlySats/com/shared"
	"OnlySats/config"
	"OnlySats/handlers"
)

//go:embed public/**
var embeddedFiles embed.FS

// Application holds all the application state and dependencies
type Application struct {
	config       *config.AppConfig
	passConfig   *config.PassConfig
	db           *shared.Database
	anal         *sql.DB
	localStore   *com.LocalDataStore
	sessionStore *sessions.CookieStore
	tempAdmin    *com.EphemeralAdmin
	startTime    time.Time
}

// NewApplication creates and initializes a new Application instance
func NewApplication() (*Application, error) {
	app := &Application{
		startTime: time.Now(),
	}

	if err := app.loadConfig(); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if err := app.initializeStores(); err != nil {
		return nil, fmt.Errorf("failed to initialize stores: %w", err)
	}

	return app, nil
}

// Close gracefully shuts down the application
func (app *Application) Close() error {
	var errs []error

	if app.localStore != nil {
		if err := app.localStore.Close(); err != nil {
			errs = append(errs, fmt.Errorf("local store close: %w", err))
		}
	}

	if app.db != nil {
		if err := app.db.Close(); err != nil {
			errs = append(errs, fmt.Errorf("database close: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("multiple close errors: %v", errs)
	}

	return nil
}

func (app *Application) loadConfig() error {
	var err error
	app.config, app.passConfig, err = config.LoadConfig("config.toml")
	return err
}

func (app *Application) initializeStores() error {
	// Init local data store (toml)
	var err error
	app.localStore, err = com.OpenLocalData(app.config)
	if err != nil {
		return fmt.Errorf("local data init: %w", err)
	}

	// Init sqlite3 for image meta and settings
	dbCfg, err := shared.NewConfigFromAppConfig(app.config)
	if err != nil {
		return fmt.Errorf("database config: %w", err)
	}

	app.db, err = shared.OpenDatabase(dbCfg)
	if err != nil {
		return fmt.Errorf("database open: %w", err)
	}

	// Init session store (signed + encrypted)
	keys, err := com.LoadOrGenerateSessionKeys(app.config.Paths.DataDir)
	if err != nil {
		return fmt.Errorf("session key init: %w", err)
	}

	app.anal, err = shared.OpenAnalDB(app.config.Paths.DataDir)
	if err != nil {
		return fmt.Errorf("analytics db open: %w", err)
	}
	if err := shared.InitSchema(app.anal); err != nil {
		return fmt.Errorf("analytics schema: %w", err)
	}

	secure := true
	app.sessionStore = com.NewCookieStore(keys, secure, 60*60*48)

	return nil
}

func (app *Application) runStartupTasks() error {
	// Run database update
	if err := com.RunDBUpdate(app.config, app.passConfig, false); err != nil {
		return fmt.Errorf("database update: %w", err)
	}

	// Generate thumbnails
	if err := com.RunThumbGen(app.config, app.db.DB); err != nil {
		return fmt.Errorf("thumbnail generation: %w", err)
	}
	log.Println("Data initialized")
	return nil
}

func (app *Application) startStationProxy() {
	if !app.config.StationProxy.Enabled {
		return
	}

	log.Printf("Starting station proxy...")
	if err := com.RunStationProxy(app.config); err != nil {
		log.Printf("Station proxy error: %v", err)
	} else {
		log.Printf("Station hosted at stations.onlysatellites.com/%s", app.config.StationProxy.StationId)
	}
}

func (app *Application) createRouter() *mux.Router {
	r := mux.NewRouter()
	r.Use(com.SecurityHeaders)

	// route handlers
	app.setupStaticRoutes(r)
	app.setupGalleryRoutes(r)
	app.setupImageRoutes(r)
	app.setupMiscRoutes(r)
	app.setupSatdumpRoutes(r)
	app.setupUpdateRoutes(r)
	app.setupPublicRoutes(r)

	return r
}

func (app *Application) setupStaticRoutes(r *mux.Router) {
	r.PathPrefix("/css/").Handler(http.StripPrefix("/css/", http.FileServer(app.mustSubFS("public/css"))))
	r.PathPrefix("/js/").Handler(http.StripPrefix("/js/", http.FileServer(app.mustSubFS("public/js"))))
}

func (app *Application) setupPublicRoutes(r *mux.Router) {
	htmlFS, err := fs.Sub(embeddedFiles, "public/html")
	if err != nil {
		log.Fatal("Failed to create HTML filesystem:", err)
	}

	r.HandleFunc("/", app.serveEmbeddedHTML("index.html", htmlFS))
	r.HandleFunc("/about", app.serveEmbeddedHTML("about.html", htmlFS))
	r.HandleFunc("/data", app.serveEmbeddedHTML("data.html", htmlFS))
	r.HandleFunc("/login", app.loginPage(htmlFS)).Methods("GET")
	r.HandleFunc("/login", app.handleLogin).Methods("POST")
	r.HandleFunc("/logout", app.handleLogout).Methods("GET")
}

func (app *Application) setupGalleryRoutes(r *mux.Router) {
	htmlFS, err := fs.Sub(embeddedFiles, "public/html")
	if err != nil {
		log.Fatal("Failed to create HTML filesystem:", err)
	}

	apiHandler := handlers.NewAPIHandler(app.db)
	gapi := &handlers.GalleryAPI{
		DB:            app.db.DB,
		LiveOutputDir: app.config.Paths.LiveOutputDir,
		UserContent:   filepath.Join("public", "userContent"),
		LocalStore:    app.localStore,
	}

	galleryHandler, _, err := handlers.GalleryHandler(htmlFS, gapi)
	if err != nil {
		log.Fatalf("Failed to initialize gallery handler: %v", err)
	}

	// API endpoints
	r.HandleFunc("/api/images", apiHandler.GetImages).Methods("GET")
	r.HandleFunc("/api/share/images/{id:[0-9]+}", apiHandler.ShareImageByID).Methods("GET")
	r.HandleFunc("/api/satellites", gapi.Satellites()).Methods("GET")
	r.HandleFunc("/api/bands", gapi.Bands()).Methods("GET")
	r.HandleFunc("/api/composites", gapi.CompositesList()).Methods("GET")
	r.HandleFunc("/api/export", gapi.ExportCADU()).Methods("GET")
	r.HandleFunc("/api/zip", gapi.ZipPath()).Methods("GET")

	// Gallery page
	r.HandleFunc("/gallery", galleryHandler).Methods("GET")
}

func (app *Application) setupImageRoutes(r *mux.Router) {
	r.PathPrefix("/images/").Handler(handlers.ImageServer(app.config.Paths.LiveOutputDir))
	r.PathPrefix("/thumbnails/").Handler(handlers.ThumbnailServer(app.config.Paths.LiveOutputDir, app.config.Paths.ThumbnailDir))
}

func (app *Application) setupSatdumpRoutes(r *mux.Router) {
	// template
	htmlFS, err := fs.Sub(embeddedFiles, "public/html")
	if err != nil {
		log.Fatal("Failed to create HTML filesystem:", err)
	}
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

		if row, err := app.localStore.GetSatdump(ctx, name); err == nil && row != nil {
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
		list, _ := app.localStore.ListSatdump(ctx)
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
		list, err := app.localStore.ListSatdump(ctx)
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
	r.Handle("/local/satdump", app.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	r.Handle("/local/satdump/live", app.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ip, port, ok := resolveFromCookieOrFirst(w, r); ok {
			handlers.SatdumpLive(ip, port).ServeHTTP(w, r)
			return
		}
		http.Error(w, "No SatDump instances configured", http.StatusNotFound)
	}))).Methods("GET")

	r.Handle("/local/satdump/html", app.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ip, port, ok := resolveFromCookieOrFirst(w, r); ok {
			handlers.SatdumpHTML(ip, port).ServeHTTP(w, r)
			return
		}
		http.Error(w, "No SatDump instances configured", http.StatusNotFound)
	}))).Methods("GET")

	// match name
	r.Handle("/local/satdump/{name:[^/.]+}", app.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		if v, _ := app.localStore.GetSetting(r.Context(), "satdump_rate"); strings.TrimSpace(v) != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
				rateMS = n
			}
		}
		if v, _ := app.localStore.GetSetting(r.Context(), "satdump_span"); strings.TrimSpace(v) != "" {
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
	r.PathPrefix("/local/satdump/").Handler(app.requireAuth(3, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	ah := &handlers.SatdumpHandler{Store: app.localStore, AnalDB: app.anal}
	r.Handle("/api/satdump/names", http.HandlerFunc(ah.Names)).Methods("GET")
	r.Handle("/api/analytics/tracks", http.HandlerFunc(ah.PolarPlot)).Methods("GET")
	r.Handle("/api/analytics/decoder", http.HandlerFunc(ah.GEOProgress)).Methods("GET")
}

func (app *Application) setupUpdateRoutes(r *mux.Router) {
	cd := time.Minute
	if s, err := app.localStore.GetSetting(context.Background(), "update_cd"); err == nil {
		if n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64); err == nil && n > 0 {
			cd = time.Duration(n) * time.Second
		}
	}

	upd := &handlers.UpdateHandler{
		Cfg:      app.config,
		Pass:     app.passConfig,
		Cooldown: cd,
	}
	rpl := &handlers.RepopulateHandler{
		Cfg:      app.config,
		Pass:     app.passConfig,
		Cooldown: time.Minute,
	}

	r.Handle("/api/update", upd).Methods("POST")
	r.Handle("/api/repopulate", app.requireAuth(3, rpl)).Methods("POST")
}

func (app *Application) setupMiscRoutes(r *mux.Router) {
	// Settings handler
	settings := &handlers.SettingsHandler{Store: app.localStore}
	r.Handle("/api/config/theme", app.requireAuth(1, http.HandlerFunc(settings.PostTheme))).Methods("POST")
	r.Handle("/local/api/settings", app.requireAuth(1, http.HandlerFunc(settings.PostSettings))).Methods("POST")
	r.Handle("/local/api/settings", app.requireAuth(1, http.HandlerFunc(settings.GetSettings))).Methods("GET")

	htmlFS, err := fs.Sub(embeddedFiles, "public/html")
	if err != nil {
		log.Fatal("Failed to create HTML filesystem:", err)
	}

	r.Handle("/local/configure-passes", app.requireAuth(1, app.serveEmbeddedHTML("template_editor.html", htmlFS))).Methods("GET")
	tapi := handlers.NewTemplatesAdminAPI(app.localStore) // make sure StationPreferences is opened at startup
	tapi.Register(r, app.requireAuth)

	// Hardware monitor handler
	hw := &handlers.HardwareHandler{
		Cfg:     app.config,
		Store:   app.localStore,
		Timeout: 3 * time.Second,
	}
	r.Handle("/local/api/hardware", app.requireAuth(3, hw)).Methods("GET")
	info := handlers.NewInfoHandler(app.startTime)
	r.Handle("/local/api/info", info).Methods("GET")

	// CSS and admin routes
	r.Handle("/colors.css", &handlers.ColorsCSSHandler{Store: app.localStore})
	r.Handle("/local/stats", app.requireAuth(3, app.serveEmbeddedHTML("stats.html", htmlFS))).Methods("GET")
	r.Handle("/local/admin", app.requireAuth(1, app.serveEmbeddedHTML("admin-center.html", htmlFS))).Methods("GET")
	r.Handle("/local/api/disk-stats", app.requireAuth(3, http.HandlerFunc(handlers.ServeDiskStats(app.config.Paths.LiveOutputDir)))).Methods("GET")

	// API endpoints
	r.Handle("/api/stats", app.requireAuth(3, http.HandlerFunc(app.handleStats))).Methods("GET")

	// About page configuration & read APIs
	about := &handlers.AboutHandler{Store: app.localStore}

	// Public about endpoints
	r.Handle("/api/about", http.HandlerFunc(about.Get)).Methods("GET")
	r.Handle("/api/about/body", http.HandlerFunc(about.GetBody)).Methods("GET")
	r.Handle("/api/about/images", http.HandlerFunc(about.ListImages)).Methods("GET")
	r.Handle("/api/about/meta", http.HandlerFunc(about.GetMeta)).Methods("GET")

	// Admin about endpoints
	r.Handle("/local/configure-about", app.requireAuth(1, app.serveEmbeddedHTML("about_editor.html", htmlFS))).Methods("GET")
	r.Handle("/local/api/about/body", app.requireAuth(1, http.HandlerFunc(about.PutBody))).Methods("PUT")
	r.Handle("/local/api/about/body", app.requireAuth(1, http.HandlerFunc(about.DeleteBody))).Methods("DELETE")
	r.Handle("/api/about/images/{id:[0-9]+}/raw", http.HandlerFunc(about.RawImage)).Methods("GET")
	r.Handle("/local/api/about/images/upload", app.requireAuth(1, http.HandlerFunc(about.UploadImage))).Methods("POST")
	r.Handle("/local/api/about/images/{id:[0-9]+}", app.requireAuth(1, http.HandlerFunc(about.UpdateImage))).Methods("PUT")
	r.Handle("/local/api/about/images/{id:[0-9]+}", app.requireAuth(1, http.HandlerFunc(about.DeleteImage))).Methods("DELETE")
	r.Handle("/local/api/about/meta/{key}", app.requireAuth(1, http.HandlerFunc(about.PutMeta))).Methods("PUT")
	r.Handle("/local/api/about/meta/{key}", app.requireAuth(1, http.HandlerFunc(about.DeleteMeta))).Methods("DELETE")

	// Users
	users := &handlers.UsersHandler{Store: app.localStore}

	r.Handle("/local/api/users", app.requireAuth(0, http.HandlerFunc(users.List))).Methods("GET")
	r.Handle("/local/api/users", app.requireAuth(0, http.HandlerFunc(users.Create))).Methods("POST")
	r.Handle("/local/api/users/{id:[0-9]+}", app.requireAuth(0, http.HandlerFunc(users.Delete))).Methods("DELETE")
	r.Handle("/local/api/users/{id:[0-9]+}/username", app.requireAuth(0, http.HandlerFunc(users.SetUsername))).Methods("PUT")
	r.Handle("/local/api/users/{id:[0-9]+}/level", app.requireAuth(0, http.HandlerFunc(users.SetLevel))).Methods("PUT")
	r.Handle("/local/api/users/{id:[0-9]+}/reset-password", app.requireAuth(0, http.HandlerFunc(users.ResetPassword))).Methods("POST")

	// Satdump config
	satdump := &handlers.SatdumpHandler{Store: app.localStore}

	r.Handle("/local/api/satdump", app.requireAuth(0, http.HandlerFunc(satdump.List))).Methods("GET")
	r.Handle("/local/api/satdump", app.requireAuth(0, http.HandlerFunc(satdump.Create))).Methods("POST")
	r.Handle("/local/api/satdump/{name}", app.requireAuth(0, http.HandlerFunc(satdump.Get))).Methods("GET")
	r.Handle("/local/api/satdump/{name}", app.requireAuth(0, http.HandlerFunc(satdump.Update))).Methods("PUT")
	r.Handle("/local/api/satdump/{name}", app.requireAuth(0, http.HandlerFunc(satdump.Delete))).Methods("DELETE")

	// Message Posting/Getting
	r.Handle("/local/messages-admin", app.requireAuth(1, app.serveEmbeddedHTML("messages.html", htmlFS))).Methods("GET")

	msgs := &handlers.MessagesHandler{Store: app.localStore}
	r.Handle("/api/messages", http.HandlerFunc(msgs.List)).Methods("GET")
	r.Handle("/api/messages/latest", http.HandlerFunc(msgs.Latest)).Methods("GET")
	r.Handle("/api/messages/{id:[0-9]+}", http.HandlerFunc(msgs.Get)).Methods("GET")
	r.Handle("/api/messages/{id:[0-9]+}/image", http.HandlerFunc(msgs.RawImage)).Methods("GET")
	r.Handle("/local/api/messages", app.requireAuth(1, http.HandlerFunc(msgs.Create))).Methods("POST")
	r.Handle("/local/api/messages/{id:[0-9]+}", app.requireAuth(1, http.HandlerFunc(msgs.Update))).Methods("PUT")
	r.Handle("/local/api/messages/{id:[0-9]+}", app.requireAuth(1, http.HandlerFunc(msgs.Delete))).Methods("DELETE")
	r.Handle("/messages/{id:[0-9]+}", app.serveEmbeddedHTML("message_viewer.html", htmlFS)).Methods("GET")
}

// Helper methods

func (app *Application) mustSubFS(dir string) http.FileSystem {
	sub, err := fs.Sub(embeddedFiles, dir)
	if err != nil {
		log.Fatalf("Failed to create sub filesystem for %q: %v", dir, err)
	}
	return http.FS(sub)
}

func (app *Application) serveEmbeddedHTML(name string, htmlFS fs.FS) http.HandlerFunc {
	t := template.Must(template.New(name).ParseFS(htmlFS, name))
	return func(w http.ResponseWriter, r *http.Request) {
		if err := t.Execute(w, nil); err != nil {
			log.Printf("Template rendering failed for %s: %v", name, err)
			http.Error(w, "Template rendering failed", http.StatusInternalServerError)
		}
	}
}

func (app *Application) loginPage(htmlFS fs.FS) http.HandlerFunc {
	t := template.Must(template.New("login.html").ParseFS(htmlFS, "login.html"))
	return func(w http.ResponseWriter, r *http.Request) {
		if err := t.Execute(w, nil); err != nil {
			log.Printf("Login template rendering failed: %v", err)
			http.Error(w, "Template rendering failed", http.StatusInternalServerError)
		}
	}
}

// Authentication middleware
func (app *Application) requireAuth(minLevel int, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := app.sessionStore.Get(r, "session")
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

// Auth handlers
func (app *Application) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Failed to parse form", http.StatusBadRequest)
		return
	}

	username := r.FormValue("username")
	password := r.FormValue("password")

	// DB auth first
	user, level, ok, err := app.localStore.AuthenticateUser(r.Context(), username, password)
	if err != nil {
		http.Error(w, "Auth error", http.StatusInternalServerError)
		return
	}

	// Ephemeral admin fallback ONLY if no admin users exist
	if !ok && app.tempAdmin != nil {
		if lvl, eok := app.tempAdmin.Try(r.Context(), app.localStore, username, password); eok {
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
	if err := com.CookieLogin(app.sessionStore, w, r, user, level); err != nil {
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

func (app *Application) handleLogout(w http.ResponseWriter, r *http.Request) {
	session, err := app.sessionStore.Get(r, "session")
	if err != nil {
		log.Printf("Session error during logout: %v", err)
	}

	session.Options.MaxAge = -1
	if err := session.Save(r, w); err != nil {
		log.Printf("Failed to clear session: %v", err)
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (app *Application) initializeAuthDB() error {
	ctx := context.Background()

	ep, err := com.NewEphemeralAdminIfNoAdmins(ctx, app.localStore)
	if err != nil {
		return fmt.Errorf("bootstrap admin check: %w", err)
	}
	app.tempAdmin = ep

	// If the ephemeral admin is enabled
	// ep.Try(...) will return ok=true when given the generated password.
	if ep != nil {
		if _, ok := ep.Try(ctx, app.localStore, "admin", ep.Password); ok {
			log.Printf(
				"No admin users present (level <= 1). Ephemeral admin enabled.\n   username: admin\n   password: %s\n",
				ep.Password,
			)
		}
	}

	return nil
}

// API handlers
func (app *Application) handleStats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	stats := map[string]interface{}{
		"startTime": app.startTime.Unix(),
		"uptime":    time.Since(app.startTime).Seconds(),
	}

	if err := json.NewEncoder(w).Encode(stats); err != nil {
		log.Printf("Failed to encode stats: %v", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// Main function
func main() {
	app, err := NewApplication()
	if err != nil {
		log.Fatal("Failed to initialize application:", err)
	}
	defer func() {
		if err := app.Close(); err != nil {
			log.Printf("Error during shutdown: %v", err)
		}
	}()

	if shared.IsAdmin() {
		log.Print("Exiting...")
		return
	}

	log.Println("Server starting, please wait...")
	if err := app.runStartupTasks(); err != nil {
		log.Printf("Startup warning: %v", err)
	}

	app.startStationProxy()

	if err := app.initializeAuthDB(); err != nil {
		log.Fatal("failed to initialize auth: %w", err)
	}

	router := app.createRouter()
	go com.RunScheduledTasks(app.config)

	// start server with proper timeouts
	srv := &http.Server{
		Addr:              app.config.Server.Port,
		Handler:           router,
		ReadTimeout:       time.Duration(app.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(app.config.Server.WriteTimeout) * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Server running at http://localhost%s", app.config.Server.Port)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
