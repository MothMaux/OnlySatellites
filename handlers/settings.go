package handlers

import (
	"OnlySats/com"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type SettingsHandler struct {
	Store *sql.DB
}

var cssVarKeyRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

type themePayload map[string]string

func (h *SettingsHandler) PostTheme(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Store == nil {
		http.Error(w, "store not ready", http.StatusServiceUnavailable)
		return
	}

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	updated := 0
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var mp themePayload
	if err := dec.Decode(&mp); err == nil && len(mp) > 0 {
		for k, v := range mp {
			if !cssVarKeyRe.MatchString(k) {
				http.Error(w, "invalid variable name: "+k, http.StatusBadRequest)
				return
			}
			if err := com.SetColor(h.Store, ctx, k, v); err != nil {
				http.Error(w, "failed to save: "+err.Error(), http.StatusInternalServerError)
				return
			}
			updated++
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"updated": updated,
		})
		return
	}

	// Reset decoder, try pairs form
	r.Body.Close()
	http.Error(w, "invalid payload (expected JSON object of name:value or {pairs:[...]})", http.StatusBadRequest)
}

func (h *SettingsHandler) PostSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Store == nil {
		http.Error(w, "server misconfigured: nil store", http.StatusInternalServerError)
		return
	}

	// Decode body as a generic map
	var payload map[string]any
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if len(payload) == 0 {
		http.Error(w, "empty payload", http.StatusBadRequest)
		return
	}

	// Short timeout-bound context for DB
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	type setResult struct {
		Key   string `json:"key"`
		Value string `json:"value"`
		Err   string `json:"error,omitempty"`
	}
	results := make([]setResult, 0, len(payload))
	updated := 0

	for k, v := range payload {
		key := strings.TrimSpace(k)
		if key == "" {
			results = append(results, setResult{Key: k, Err: "empty key"})
			continue
		}
		// Stringify the value to store arbitrary simple JSON values.
		valBytes, err := json.Marshal(v)
		if err != nil {
			results = append(results, setResult{Key: key, Err: "value marshal failed"})
			continue
		}
		// If the incoming is a plain string, store without quotes for convenience.
		val := strings.TrimSpace(string(valBytes))
		if s, ok := v.(string); ok {
			val = strings.TrimSpace(s)
		}

		if err := com.SetSetting(h.Store, ctx, key, val); err != nil {
			results = append(results, setResult{Key: key, Value: val, Err: err.Error()})
			continue
		}
		updated++
		results = append(results, setResult{Key: key, Value: val})
	}

	resp := struct {
		Updated int         `json:"updated"`
		Results []setResult `json:"results"`
	}{
		Updated: updated,
		Results: results,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *SettingsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	if h.Store == nil {
		http.Error(w, "settings store not ready", http.StatusServiceUnavailable)
		return
	}
	ctx := r.Context()
	settings, err := com.ListSettings(h.Store, ctx)
	if err != nil {
		http.Error(w, "failed to list settings: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(settings)
}
