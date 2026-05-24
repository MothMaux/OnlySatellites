package handlers

import (
	"OnlySats/com"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

type SatdumpPageData struct {
	Title       string
	StatusHTML  template.HTML
	ApiDataJSON string
}

type Satdump struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	Logging int    `json:"log"`
}

type SatdumpHandler struct {
	Store  *sql.DB
	AnalDB *sql.DB
}

type Store interface {
	ListSatdump(ctx context.Context) ([]Satdump, error)
	GetSatdump(ctx context.Context, name string) (*Satdump, error)
	CreateSatdump(ctx context.Context, name, address string, port int, log int) error
	UpdateSatdump(ctx context.Context, oldName string, newName string, address *string, port *int, log *int) error
	DeleteSatdump(ctx context.Context, name string) error
}

func SatdumpIndex(tmpl *template.Template, hostIP string, port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := &http.Client{Timeout: 5 * time.Second}
		base := "http://" + hostIP + ":" + itoa(port)

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()

		var (
			statusHTML []byte
			apiJSON    []byte
			err1, err2 error
		)

		done := make(chan struct{}, 2)

		go func() {
			defer func() { done <- struct{}{} }()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/status", nil)
			resp, err := client.Do(req)
			if err != nil {
				err1 = err
				return
			}
			defer resp.Body.Close()
			statusHTML, err1 = io.ReadAll(resp.Body)
		}()

		go func() {
			defer func() { done <- struct{}{} }()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api", nil)
			resp, err := client.Do(req)
			if err != nil {
				err2 = err
				return
			}
			defer resp.Body.Close()
			apiJSON, err2 = io.ReadAll(resp.Body)
		}()

		<-done
		<-done

		if err1 != nil || err2 != nil {
			log.Printf("satdump fetch error: statusErr=%v apiErr=%v", err1, err2)
			http.Error(w, "Failed to fetch satdump data", http.StatusInternalServerError)
			return
		}

		// pretty print JSON
		var tmp any
		if err := json.Unmarshal(apiJSON, &tmp); err == nil {
			if pretty, e := json.MarshalIndent(tmp, "", "  "); e == nil {
				apiJSON = pretty
			}
		}

		data := SatdumpPageData{
			Title:       "SatDump Viewer",
			StatusHTML:  template.HTML(statusHTML),
			ApiDataJSON: string(apiJSON),
		}

		// Render into a buffer
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, data); err != nil {
			log.Printf("template render failed: %v", err)
			http.Error(w, "template rendering failed", http.StatusInternalServerError)
			return
		}

		// write headers/body
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
	}
}

func SatdumpLive(hostIP string, port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + hostIP + ":" + itoa(port) + "/api")
		if err != nil {
			http.Error(w, `{"error":"Failed to fetch live data"}`, http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		io.Copy(w, resp.Body)
	}
}

func SatdumpHTML(hostIP string, port int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get("http://" + hostIP + ":" + itoa(port) + "/status")
		if err != nil {
			http.Error(w, "Failed to fetch status fragment", http.StatusInternalServerError)
			return
		}
		defer resp.Body.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.Copy(w, resp.Body)
	}
}

func (a *SatdumpHandler) List(w http.ResponseWriter, r *http.Request) {
	rows, err := com.ListSatdump(a.Store, r.Context())
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (a *SatdumpHandler) Get(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(mux.Vars(r)["name"])
	if name == "" {
		badRequest(w, "missing name")
		return
	}
	row, err := com.GetSatdump(a.Store, r.Context(), name)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			notFound(w, "satdump not found")
			return
		}
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (a *SatdumpHandler) Create(w http.ResponseWriter, r *http.Request) {
	var in Satdump
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid json")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	if in.Name == "" {
		badRequest(w, "name is required")
		return
	}
	if in.Port < 0 || in.Port > 65535 {
		badRequest(w, "port must be 0..65535")
		return
	}
	if in.Logging != 0 {
		in.Logging = 1
	}

	if err := com.CreateSatdump(a.Store, r.Context(), in.Name, in.Address, in.Port, in.Logging); err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (a *SatdumpHandler) Update(w http.ResponseWriter, r *http.Request) {
	oldName := strings.TrimSpace(mux.Vars(r)["name"])
	if oldName == "" {
		badRequest(w, "missing name")
		return
	}

	var in map[string]any
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid json")
		return
	}

	rawName, ok := in["name"]
	if !ok {
		badRequest(w, `"name" field required`)
		return
	}
	newName, ok := rawName.(string)
	if !ok {
		badRequest(w, `"name" must be string`)
		return
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		badRequest(w, "name cannot be empty")
		return
	}

	var addrPtr *string
	var portPtr *int
	var logPtr *int

	if v, ok := in["address"]; ok {
		if s, ok := v.(string); ok {
			tmp := strings.TrimSpace(s)
			addrPtr = &tmp
		} else {
			badRequest(w, "address must be string")
			return
		}
	}
	if v, ok := in["port"]; ok {
		pi, err := jsonToInt(v)
		if err != nil || pi < 0 || pi > 65535 {
			badRequest(w, "port must be 0..65535")
			return
		}
		portPtr = &pi
	}
	if v, ok := in["log"]; ok {
		lv, err := jsonToInt(v)
		if err != nil {
			badRequest(w, err.Error())
			return
		}
		logPtr = &lv
	}

	if err := com.UpdateSatdump(a.Store, r.Context(), oldName, newName, addrPtr, portPtr, logPtr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			notFound(w, "satdump not found")
			return
		}
		serverErr(w, err)
		return
	}

	row, err := com.GetSatdump(a.Store, r.Context(), newName)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"updated": newName})
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (a *SatdumpHandler) Delete(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(mux.Vars(r)["name"])
	if name == "" {
		badRequest(w, "missing name")
		return
	}
	if err := com.DeleteSatdump(a.Store, r.Context(), name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			notFound(w, "satdump not found")
			return
		}
		serverErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Public APIs (data page)

func (h *SatdumpHandler) Names(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	out := com.GetSatdumpActive(r.Context(), h.AnalDB)
	_ = json.NewEncoder(w).Encode(out)
}

func (h *SatdumpHandler) PolarPlot(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		badRequest(w, "name required")
		return
	}
	from := parseInt64Default(r.URL.Query().Get("from"), time.Now().Add(-6*time.Hour).Unix())
	to := parseInt64Default(r.URL.Query().Get("to"), time.Now().Unix())

	points, err := com.TracksSNR(r.Context(), h.AnalDB, name, from, to)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (h *SatdumpHandler) GEOProgress(w http.ResponseWriter, r *http.Request) {
	decoder := strings.TrimSpace(r.URL.Query().Get("decoder"))
	if decoder == "" {
		badRequest(w, "decoder required")
		return
	}
	from := parseInt64Default(r.URL.Query().Get("from"), time.Now().Add(-6*time.Hour).Unix())
	to := parseInt64Default(r.URL.Query().Get("to"), time.Now().Unix())

	points, err := com.DecoderSNRStats(r.Context(), h.AnalDB, decoder, from, to)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, points)
}
