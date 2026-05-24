package handlers

import (
	"bytes"
	"crypto/sha1"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"io"
	"log"
	"mime"
	"net/http"
	"strconv"
	"time"

	"OnlySats/com"

	"github.com/gorilla/mux"
	_ "golang.org/x/image/webp"
)

// AboutHandler wires HTTP to LocalDataStore About* methods
type AboutHandler struct {
	Store *sql.DB
}

// ---------- DTOs ----------

type aboutAggregate struct {
	Body    string            `json:"body"`
	Updated int64             `json:"updated"` // unix seconds (0 if unknown)
	Images  []com.AboutImage  `json:"images"`
	Meta    map[string]string `json:"meta"`
}

type setBodyReq struct {
	Body string `json:"body"`
}

// Use pointer fields so omitted values are not overwritten on update
type updateImageReq struct {
	Path    *string `json:"path,omitempty"`
	Caption *string `json:"caption,omitempty"`
	Sort    *int    `json:"sort,omitempty"`
}

type setMetaReq struct {
	Value string `json:"value"`
}

// Public (read) endpoints

func (h *AboutHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, updated, _ := com.GetAboutBody(h.Store, ctx)
	imgs, _ := com.ListAboutImages(h.Store, ctx)
	meta, _ := com.GetAllAboutMeta(h.Store, ctx)

	resp := aboutAggregate{
		Body: body,
		Updated: func(t time.Time) int64 {
			if t.IsZero() {
				return 0
			}
			return t.Unix()
		}(updated),
		Images: imgs,
		Meta:   meta,
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AboutHandler) GetBody(w http.ResponseWriter, r *http.Request) {
	body, updated, err := com.GetAboutBody(h.Store, r.Context())
	if err != nil {
		http.Error(w, "failed to read about body", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"body": body,
		"updated": func(t time.Time) int64 {
			if t.IsZero() {
				return 0
			}
			return t.Unix()
		}(updated),
	})
}

func (h *AboutHandler) ListImages(w http.ResponseWriter, r *http.Request) {
	imgs, err := com.ListAboutImages(h.Store, r.Context())
	if err != nil {
		http.Error(w, "failed to list images", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, imgs)
}

func (h *AboutHandler) GetMeta(w http.ResponseWriter, r *http.Request) {
	meta, err := com.GetAllAboutMeta(h.Store, r.Context())
	if err != nil {
		http.Error(w, "failed to read metadata", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

// Admin (write) endpoints

func (h *AboutHandler) PutBody(w http.ResponseWriter, r *http.Request) {
	var req setBodyReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := com.SetAboutBody(h.Store, r.Context(), req.Body); err != nil {
		http.Error(w, "failed to save body", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AboutHandler) DeleteBody(w http.ResponseWriter, r *http.Request) {
	if err := com.DeleteAboutBody(h.Store, r.Context()); err != nil {
		http.Error(w, "failed to delete body", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AboutHandler) UploadImage(w http.ResponseWriter, r *http.Request) {
	const maxFile = int64(10 << 20) // 10 MB file limit
	const reqCap = int64(11 << 20)  // a little headroom for multipart

	r.Body = http.MaxBytesReader(w, r.Body, reqCap)
	if err := r.ParseMultipartForm(reqCap); err != nil {
		http.Error(w, "payload too large or invalid multipart", http.StatusRequestEntityTooLarge)
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		http.Error(w, "image file required", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Read the file with a hard limit
	lr := &io.LimitedReader{R: file, N: maxFile + 1}
	var in bytes.Buffer
	if _, err := io.Copy(&in, lr); err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if lr.N <= 0 {
		http.Error(w, "file exceeds 10MB", http.StatusRequestEntityTooLarge)
		return
	}

	// Decode & re-encode as JPEG to strip EXIF
	src, _, err := image.Decode(bytes.NewReader(in.Bytes()))
	if err != nil {
		http.Error(w, "unsupported or corrupt image", http.StatusBadRequest)
		return
	}
	bounds := src.Bounds()
	wpx, hpx := bounds.Dx(), bounds.Dy()

	var out bytes.Buffer
	if err := jpeg.Encode(&out, src, &jpeg.Options{Quality: 85}); err != nil {
		http.Error(w, "encode error", http.StatusInternalServerError)
		return
	}
	if out.Len() > int(maxFile) {
		http.Error(w, "re-encoded image exceeds 10MB", http.StatusRequestEntityTooLarge)
		return
	}
	mimeType := "image/jpeg"

	id, err := com.AddAboutImageBlobFlexible(h.Store, r.Context(), out.Bytes(), mimeType, wpx, hpx, "", 0)
	if err != nil {
		log.Printf("UploadImage: insert failed: %v", err)
		http.Error(w, "db insert failed", http.StatusInternalServerError)
		return
	}

	// Respond with a virtual path that points to the raw-serving endpoint
	rawURL := "api/about/images/" + strconv.FormatInt(id, 10) + "/raw"
	writeJSON(w, http.StatusCreated, map[string]any{
		"id":     id,
		"path":   rawURL,
		"name":   header.Filename,
		"size":   out.Len(),
		"width":  wpx,
		"height": hpx,
	})
}

func (h *AboutHandler) UpdateImage(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(mux.Vars(r), "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var req updateImageReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Path == nil && req.Caption == nil && req.Sort == nil {
		http.Error(w, "no fields to update", http.StatusBadRequest)
		return
	}
	if err := com.UpdateAboutImage(h.Store, r.Context(), id, req.Path, req.Caption, req.Sort); err != nil {
		http.Error(w, "failed to update image", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AboutHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(mux.Vars(r), "id")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := com.RemoveAboutImage(h.Store, r.Context(), id); err != nil {
		http.Error(w, "failed to delete image", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AboutHandler) RawImage(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(mux.Vars(r), "id")
	if err != nil {
		http.Error(w, "bad id", http.StatusBadRequest)
		return
	}
	data, mimeType, createdAt, err := com.GetAboutImageBlob(h.Store, r.Context(), id)
	if err != nil || len(data) == 0 {
		http.NotFound(w, r)
		return
	}

	// Basic caching headers
	sum := sha1.Sum(data) // weak ETag is fine here
	etag := `W/"` + strconv.FormatInt(int64(len(data)), 10) + `-` + fmt.Sprintf("%x", sum[:8]) + `"`
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if createdAt > 0 {
		w.Header().Set("Last-Modified", time.Unix(createdAt, 0).UTC().Format(http.TimeFormat))
	}
	if mimeType == "" {
		mimeType = mime.TypeByExtension(".jpg")
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=86400") // Cache for 1 day

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *AboutHandler) PutMeta(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	var req setMetaReq
	if key == "" || json.NewDecoder(r.Body).Decode(&req) != nil {
		http.Error(w, "key and value required", http.StatusBadRequest)
		return
	}
	if err := com.SetAboutMeta(h.Store, r.Context(), key, req.Value); err != nil {
		http.Error(w, "failed to save metadata", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *AboutHandler) DeleteMeta(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	if key == "" {
		http.Error(w, "key required", http.StatusBadRequest)
		return
	}
	if err := com.DeleteAboutMeta(h.Store, r.Context(), key); err != nil {
		http.Error(w, "failed to delete metadata", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
