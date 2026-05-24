package handlers

import (
	"OnlySats/com"
	"context"
	"database/sql"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
)

// wires message APIs to the LocalDataStore.
type MessagesHandler struct {
	Store *sql.DB
}

func (h *MessagesHandler) List(w http.ResponseWriter, r *http.Request) {
	// pagination: ?limit=50&offset=0
	limit := 50
	offset := 0
	if v := strings.TrimSpace(r.URL.Query().Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	if v := strings.TrimSpace(r.URL.Query().Get("offset")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}

	rows, err := com.ListMessages(h.Store, r.Context(), limit, offset)
	if err != nil {
		serverErr(w, err) // uses your helpers
		return
	}

	// For list view, omit the actual image bytes.
	type item struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Message   string `json:"message"`
		Type      string `json:"type"`
		Timestamp int64  `json:"timestamp"`
		HasImage  bool   `json:"hasImage"`
		ImageURL  string `json:"imageUrl,omitempty"`
	}
	out := make([]item, 0, len(rows))
	for _, m := range rows {
		it := item{
			ID:        m.ID,
			Title:     m.Title,
			Message:   m.Message,
			Type:      m.Type,
			Timestamp: m.Timestamp.Unix(),
			HasImage:  len(m.Image) > 0,
		}
		if it.HasImage {
			it.ImageURL = "/api/messages/" + strconv.FormatInt(m.ID, 10) + "/image"
		}
		out = append(out, it)
	}

	writeJSON(w, http.StatusOK, apiOK[any]{OK: true, Data: map[string]any{
		"messages": out,
	}})
}

func (h *MessagesHandler) Create(w http.ResponseWriter, r *http.Request) {
	// Limit total body to ~20MB to be safe
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)

	if err := r.ParseMultipartForm(20 << 20); err != nil {
		badRequest(w, "invalid form: "+err.Error())
		return
	}

	title := strings.TrimSpace(r.FormValue("title"))
	body := strings.TrimSpace(r.FormValue("message"))
	typ := strings.TrimSpace(r.FormValue("type"))
	if title == "" || body == "" {
		badRequest(w, "title and message are required")
		return
	}

	var when time.Time
	if tsStr := strings.TrimSpace(r.FormValue("ts")); tsStr != "" {
		if sec, err := strconv.ParseInt(tsStr, 10, 64); err == nil && sec > 0 {
			when = time.Unix(sec, 0).UTC()
		}
	}
	if when.IsZero() {
		when = time.Now().UTC()
	}

	var imgBytes []byte
	if file, hdr, err := r.FormFile("image"); err == nil {
		defer file.Close()
		// Re-encode image to strip EXIF/metadata.
		imgBytes, err = stripMetadata(file, hdr)
		if err != nil {
			badRequest(w, "image decode/encode failed: "+err.Error())
			return
		}
	} else if err != http.ErrMissingFile && !errors.Is(err, http.ErrMissingFile) {
		badRequest(w, "image upload error: "+err.Error())
		return
	}

	id, err := com.AddMessage(h.Store, r.Context(), title, body, typ, imgBytes, when)
	if err != nil {
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, apiOK[any]{OK: true, Data: map[string]any{
		"id": id,
	}})
}

// streams the stored image, if present.
func (h *MessagesHandler) RawImage(w http.ResponseWriter, r *http.Request) {
	vars := getVars(r)
	id, err := parseID(vars, "id")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	data, mime, err := h.getMessageImage(r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) || errors.Is(err, errNoImage) {
			notFound(w, "image not found")
			return
		}
		serverErr(w, err)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// helpers

var errNoImage = errors.New("no image")

func (h *MessagesHandler) getMessageImage(ctx context.Context, id int64) ([]byte, string, error) {
	m, err := com.GetMessage(h.Store, ctx, id)
	if err != nil {
		return nil, "", err
	}
	if m == nil || len(m.Image) == 0 {
		return nil, "", errNoImage
	}
	// Try to sniff MIME; default to JPEG
	mt := http.DetectContentType(m.Image)
	if !strings.HasPrefix(mt, "image/") {
		mt = "image/jpeg"
	}
	return m.Image, mt, nil
}

// re-encodes JPEG/PNG to drop EXIF/ancillary chunks.
func stripMetadata(f multipart.File, hdr *multipart.FileHeader) ([]byte, error) {
	// read into memory for single-image payloads
	// replace strings.Builder with byte buffer
	var (
		tmp []byte
		err error
	)
	tmp, err = io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	ct := http.DetectContentType(tmp)
	// decode
	var img image.Image
	switch {
	case strings.Contains(ct, "jpeg"):
		img, err = jpeg.Decode(strings.NewReader(string(tmp)))
	case strings.Contains(ct, "png"):
		img, err = png.Decode(strings.NewReader(string(tmp)))
	default:
		// try jpeg by extension, then png
		lc := strings.ToLower(hdr.Filename)
		if strings.HasSuffix(lc, ".jpg") || strings.HasSuffix(lc, ".jpeg") {
			img, err = jpeg.Decode(strings.NewReader(string(tmp)))
			ct = "image/jpeg"
		} else if strings.HasSuffix(lc, ".png") {
			img, err = png.Decode(strings.NewReader(string(tmp)))
			ct = "image/png"
		} else {
			// fallback: attempt jpeg
			img, err = jpeg.Decode(strings.NewReader(string(tmp)))
			ct = "image/jpeg"
		}
	}
	if err != nil {
		return nil, err
	}

	// choose encoder by content type
	pr := &bytesWriter{b: make([]byte, 0, len(tmp))}
	switch ct {
	case "image/png":
		err = png.Encode(pr, img)
	default:
		// default to jpeg with decent quality
		err = jpeg.Encode(pr, img, &jpeg.Options{Quality: 90})
		ct = "image/jpeg"
	}
	if err != nil {
		return nil, err
	}
	return pr.b, nil
}

type bytesWriter struct{ b []byte }

func (w *bytesWriter) Write(p []byte) (int, error) { w.b = append(w.b, p...); return len(p), nil }

// wrapper for mux vars to decouple import
func getVars(r *http.Request) map[string]string {
	return mux.Vars(r)
}

func (h *MessagesHandler) Latest(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	rows, err := com.ListMessagesBefore(h.Store, r.Context(), now, 10)
	if err != nil {
		serverErr(w, err)
		return
	}

	type item struct {
		ID        int64  `json:"id"`
		Title     string `json:"title"`
		Message   string `json:"message"`
		Type      string `json:"type"`
		Timestamp int64  `json:"timestamp"`
		HasImage  bool   `json:"hasImage"`
		ImageURL  string `json:"imageUrl,omitempty"`
	}
	out := make([]item, 0, len(rows))
	for _, m := range rows {
		it := item{
			ID:        m.ID,
			Title:     m.Title,
			Message:   m.Message,
			Type:      m.Type,
			Timestamp: m.Timestamp.Unix(),
			HasImage:  len(m.Image) > 0,
		}
		if it.HasImage {
			it.ImageURL = "api/messages/" + strconv.FormatInt(m.ID, 10) + "/image"
		}
		out = append(out, it)
	}

	writeJSON(w, http.StatusOK, apiOK[any]{OK: true, Data: map[string]any{
		"messages": out,
	}})
}

func (h *MessagesHandler) Get(w http.ResponseWriter, r *http.Request) {
	vars := getVars(r) // maps "id" from /api/messages/{id}
	id, err := parseID(vars, "id")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	m, err := com.GetMessage(h.Store, r.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			notFound(w, "message not found")
			return
		}
		serverErr(w, err)
		return
	}

	// Shape for client
	resp := map[string]any{
		"id":        m.ID,
		"title":     m.Title,
		"message":   m.Message,
		"type":      m.Type,
		"timestamp": m.Timestamp.Unix(),
		"hasImage":  len(m.Image) > 0,
		"imageUrl":  "",
	}
	if len(m.Image) > 0 {
		resp["imageUrl"] = "/api/messages/" + strconv.FormatInt(m.ID, 10) + "/image"
	}
	writeJSON(w, http.StatusOK, apiOK[any]{OK: true, Data: resp})
}

func (h *MessagesHandler) Update(w http.ResponseWriter, r *http.Request) {
	vars := getVars(r)
	id, err := parseID(vars, "id")
	if err != nil {
		badRequest(w, err.Error())
		return
	}

	// body up to 20MB
	r.Body = http.MaxBytesReader(w, r.Body, 20<<20)
	if err := r.ParseMultipartForm(20 << 20); err != nil {
		badRequest(w, "invalid form: "+err.Error())
		return
	}

	// optional fields (nil means "leave unchanged")
	var titlePtr, msgPtr, typePtr *string
	if v := strings.TrimSpace(r.FormValue("title")); v != "" {
		titlePtr = &v
	}
	if v := r.FormValue("message"); v != "" {
		msgPtr = &v
	}
	if v := strings.TrimSpace(r.FormValue("type")); v != "" {
		typePtr = &v
	}

	var tsPtr *time.Time
	if tsStr := strings.TrimSpace(r.FormValue("ts")); tsStr != "" {
		if sec, err := strconv.ParseInt(tsStr, 10, 64); err == nil && sec > 0 {
			t := time.Unix(sec, 0).UTC()
			tsPtr = &t
		}
	}

	// image: only update if the field is present, empty field clears
	var imgBytes []byte
	var imgSet bool
	if f, hdr, err := r.FormFile("image"); err == nil {
		defer f.Close()
		data, err := stripMetadata(f, hdr)
		if err != nil {
			badRequest(w, "image decode/encode failed: "+err.Error())
			return
		}
		imgBytes = data
		imgSet = true
	} else if err == http.ErrMissingFile {
	} else if err != nil {
		badRequest(w, "image upload error: "+err.Error())
		return
	}

	if err := com.UpdateMessage(h.Store, r.Context(), id, titlePtr, msgPtr, typePtr, func() []byte {
		if imgSet {
			return imgBytes
		}
		return nil
	}(), tsPtr); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			notFound(w, "not found")
			return
		}
		serverErr(w, err)
		return
	}

	writeJSON(w, http.StatusOK, apiOK[any]{OK: true, Data: map[string]any{"id": id}})
}

// Delete by id
func (h *MessagesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	vars := getVars(r)
	id, err := parseID(vars, "id")
	if err != nil {
		badRequest(w, err.Error())
		return
	}
	if err := com.DeleteMessage(h.Store, r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			notFound(w, "not found")
			return
		}
		serverErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apiOK[any]{OK: true})
}
