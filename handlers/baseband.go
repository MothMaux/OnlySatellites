package handlers

import (
	"OnlySats/config"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type BasebandHandler struct {
	liveout string
}

type SharedFile struct {
	Filename string
}

var (
	shareStore   = make(map[string]SharedFile)
	shareStoreMu sync.RWMutex
)

func (h *BasebandHandler) GetBasebands(w http.ResponseWriter, r *http.Request) {
	var filetypes = [3]string{".ziq", ".cs8", ".cs16"}
	h.liveout = config.GetString("paths.live_output")

	entries, err := os.ReadDir(h.liveout)
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to read directory: %v", err), http.StatusInternalServerError)
		return
	}

	var matched []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		for _, ft := range filetypes {
			if ext == ft {
				matched = append(matched, name)
				break
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(matched); err != nil {
		http.Error(w, fmt.Sprintf("failed to encode response: %v", err), http.StatusInternalServerError)
	}
}

func (h *BasebandHandler) DownloadBaseband(w http.ResponseWriter, r *http.Request) {

	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "missing required query param: file", http.StatusBadRequest)
		return
	}

	fullPath := filepath.Join(h.liveout, filename)

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to open file: %v", err), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Type", "application/octet-stream")

	if _, err := io.Copy(w, f); err != nil {
		log.Printf("DownloadBaseband: error streaming file %s: %v", filename, err)
	}
}

func (h *BasebandHandler) ShareBaseband(w http.ResponseWriter, r *http.Request) {

	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "missing required query param: file", http.StatusBadRequest)
		return
	}

	filename = filepath.Base(filename)
	fullPath := filepath.Join(h.liveout, filename)
	if !strings.HasPrefix(fullPath, filepath.Clean(h.liveout)+string(os.PathSeparator)) {
		http.Error(w, "invalid file path", http.StatusBadRequest)
		return
	}
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		http.Error(w, "file not found", http.StatusNotFound)
		return
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		http.Error(w, "failed to generate share token", http.StatusInternalServerError)
		return
	}
	token := hex.EncodeToString(tokenBytes)

	shareStoreMu.Lock()
	shareStore[token] = SharedFile{Filename: filename}
	shareStoreMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"token": token})
}

func (h *BasebandHandler) DownloadPubBaseband(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		http.Error(w, "missing required query param: token", http.StatusBadRequest)
		return
	}

	shareStoreMu.RLock()
	entry, ok := shareStore[token]
	shareStoreMu.RUnlock()
	if !ok {
		http.Error(w, "invalid or expired token", http.StatusNotFound)
		return
	}

	fullPath := filepath.Join(h.liveout, entry.Filename)
	if !strings.HasPrefix(fullPath, filepath.Clean(h.liveout)+string(os.PathSeparator)) {
		http.Error(w, "invalid file path", http.StatusInternalServerError)
		return
	}

	f, err := os.Open(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("failed to open file: %v", err), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// Strip filename to 'baseband'
	ext := filepath.Ext(entry.Filename)
	downloadName := "baseband" + ext

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", downloadName))
	w.Header().Set("Content-Type", "application/octet-stream")

	if _, err := io.Copy(w, f); err != nil {
		log.Printf("DownloadPubBaseband: error streaming file %s: %v", entry.Filename, err)
	}
}
