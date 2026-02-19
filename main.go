package main

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/sessions"
	_ "github.com/mattn/go-sqlite3"

	com "OnlySats/com"
	"OnlySats/com/shared"
	"OnlySats/config"
	"OnlySats/server"
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

// Main function
func main() {
	cmdFlag := flag.String("c", "", "command to run (e.g., 'update')")
	flag.Parse()

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

	// Handle -c update command
	if *cmdFlag == "update" {
		log.Println("Running update tasks...")
		if err := app.runStartupTasks(); err != nil {
			log.Fatalf("Update tasks failed: %v", err)
		}
		log.Println("Update tasks completed successfully")
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

	// Create server with all dependencies
	srv := server.New(server.Config{
		AppConfig:    app.config,
		PassConfig:   app.passConfig,
		DB:           app.db,
		AnalDB:       app.anal,
		LocalStore:   app.localStore,
		SessionStore: app.sessionStore,
		TempAdmin:    app.tempAdmin,
		StartTime:    app.startTime,
		EmbeddedFS:   embeddedFiles,
	})

	router := srv.CreateRouter()
	go com.RunScheduledTasks(app.config)

	// start server with proper timeouts
	httpServer := &http.Server{
		Addr:              app.config.Server.Port,
		Handler:           router,
		ReadTimeout:       time.Duration(app.config.Server.ReadTimeout) * time.Second,
		WriteTimeout:      time.Duration(app.config.Server.WriteTimeout) * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("Server running at http://localhost%s", app.config.Server.Port)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
