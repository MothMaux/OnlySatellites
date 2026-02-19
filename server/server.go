package server

import (
	"database/sql"
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/sessions"

	com "OnlySats/com"
	"OnlySats/com/shared"
	"OnlySats/config"
	"OnlySats/handlers"
)

// dependencies used by the server
type Config struct {
	AppConfig    *config.AppConfig
	PassConfig   *config.PassConfig
	DB           *shared.Database
	AnalDB       *sql.DB
	LocalStore   *com.LocalDataStore
	SessionStore *sessions.CookieStore
	TempAdmin    *com.EphemeralAdmin
	StartTime    time.Time
	EmbeddedFS   embed.FS
}

type Server struct {
	cfg Config
}

// creates a new Server instance with the config
func New(cfg Config) *Server {
	return &Server{cfg: cfg}
}

// set up and returns the configured router
func (s *Server) CreateRouter() *mux.Router {
	r := mux.NewRouter()
	r.Use(com.SecurityHeaders)

	// Setup all route groups
	s.setupStaticRoutes(r)
	s.setupGalleryRoutes(r)
	s.setupImageRoutes(r)
	s.setupMiscRoutes(r)
	s.setupSatdumpRoutes(r)
	s.setupUpdateRoutes(r)
	s.setupPublicRoutes(r)

	return r
}

func (s *Server) setupStaticRoutes(r *mux.Router) {
	r.PathPrefix("/css/").Handler(http.StripPrefix("/css/", http.FileServer(s.mustSubFS("public/css"))))
	r.PathPrefix("/js/").Handler(http.StripPrefix("/js/", http.FileServer(s.mustSubFS("public/js"))))
	r.PathPrefix("/img/").Handler(http.StripPrefix("/img/", http.FileServer(s.mustSubFS("public/image"))))
}

func (s *Server) setupPublicRoutes(r *mux.Router) {
	htmlFS := s.mustSubHTMLFS()

	r.HandleFunc("/", s.serveEmbeddedHTML("index.html", htmlFS))
	r.HandleFunc("/about", s.serveEmbeddedHTML("about.html", htmlFS))
	r.HandleFunc("/data", s.serveEmbeddedHTML("data.html", htmlFS))
	r.HandleFunc("/login", s.loginPage(htmlFS)).Methods("GET")
	r.HandleFunc("/login", s.handleLogin).Methods("POST")
	r.HandleFunc("/logout", s.handleLogout).Methods("GET")
}

func (s *Server) setupGalleryRoutes(r *mux.Router) {
	htmlFS := s.mustSubHTMLFS()

	apiHandler := handlers.NewAPIHandler(s.cfg.DB)
	gapi := &handlers.GalleryAPI{
		DB:            s.cfg.DB.DB,
		LiveOutputDir: s.cfg.AppConfig.Paths.LiveOutputDir,
		UserContent:   filepath.Join("public", "userContent"),
		LocalStore:    s.cfg.LocalStore,
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

func (s *Server) setupImageRoutes(r *mux.Router) {
	r.PathPrefix("/images/").Handler(handlers.ImageServer(s.cfg.AppConfig.Paths.LiveOutputDir))
	r.PathPrefix("/thumbnails/").Handler(handlers.ThumbnailServer(s.cfg.AppConfig.Paths.LiveOutputDir, s.cfg.AppConfig.Paths.ThumbnailDir))
}

func (s *Server) mustSubFS(dir string) http.FileSystem {
	sub, err := fs.Sub(s.cfg.EmbeddedFS, dir)
	if err != nil {
		log.Fatalf("Failed to create sub filesystem for %q: %v", dir, err)
	}
	return http.FS(sub)
}

func (s *Server) mustSubHTMLFS() fs.FS {
	htmlFS, err := fs.Sub(s.cfg.EmbeddedFS, "public/html")
	if err != nil {
		log.Fatal("Failed to create HTML filesystem:", err)
	}
	return htmlFS
}

func (s *Server) mustSubPFS() fs.FS {
	htmlFS, err := fs.Sub(s.cfg.EmbeddedFS, "public/html/partials")
	if err != nil {
		log.Fatal("Failed to create HTML filesystem:", err)
	}
	return htmlFS
}

func (s *Server) serveEmbeddedHTML(name string, htmlFS fs.FS) http.HandlerFunc {
	t := template.Must(template.New(name).ParseFS(htmlFS, name))
	return func(w http.ResponseWriter, r *http.Request) {
		if err := t.Execute(w, nil); err != nil {
			log.Printf("Template rendering failed for %s: %v", name, err)
			http.Error(w, "Template rendering failed", http.StatusInternalServerError)
		}
	}
}

func (s *Server) loginPage(htmlFS fs.FS) http.HandlerFunc {
	t := template.Must(template.New("login.html").ParseFS(htmlFS, "login.html"))
	return func(w http.ResponseWriter, r *http.Request) {
		if err := t.Execute(w, nil); err != nil {
			log.Printf("Login template rendering failed: %v", err)
			http.Error(w, "Template rendering failed", http.StatusInternalServerError)
		}
	}
}
