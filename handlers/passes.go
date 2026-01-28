package handlers

import (
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/gorilla/mux"

	"OnlySats/com"
)

type TemplatesAdminAPI struct {
	Prefs *com.LocalDataStore
}

func NewTemplatesAdminAPI(prefs *com.LocalDataStore) *TemplatesAdminAPI {
	return &TemplatesAdminAPI{Prefs: prefs}
}

func (h *TemplatesAdminAPI) Register(r *mux.Router, requireAuth func(level int, h http.Handler) http.Handler) {
	r.UseEncodedPath()
	// Namespace under /local/api
	s := r.PathPrefix("/local/api").Subrouter()
	s.Handle("/pass-types", requireAuth(1, http.HandlerFunc(h.ListPassTypes))).Methods("GET")
	s.Handle("/pass-types", requireAuth(1, http.HandlerFunc(h.UpsertPassType))).Methods("POST")
	s.Handle("/pass-types/{code}", requireAuth(1, http.HandlerFunc(h.DeletePassType))).Methods("DELETE")

	s.Handle("/folder-includes", requireAuth(1, http.HandlerFunc(h.ListFolderIncludes))).Methods("GET")
	s.Handle("/folder-includes", requireAuth(1, http.HandlerFunc(h.UpsertFolderInclude))).Methods("POST")
	s.Handle("/folder-includes/{prefix}", requireAuth(1, http.HandlerFunc(h.DeleteFolderInclude))).Methods("DELETE")

	s.Handle("/pass-types/{code}/image-dirs", requireAuth(1, http.HandlerFunc(h.ListImageDirRules))).Methods("GET")
	s.Handle("/pass-types/{code}/image-dirs", requireAuth(1, http.HandlerFunc(h.UpsertImageDirRule))).Methods("POST")
	s.Handle("/pass-types/{code}/image-dirs/{dir}", requireAuth(1, http.HandlerFunc(h.DeleteImageDirRule))).Methods("DELETE")

	//Composites handling
	s.Handle("/composites", requireAuth(1, http.HandlerFunc(h.ListComposites))).Methods("GET")
	s.Handle("/composites", requireAuth(1, http.HandlerFunc(h.UpsertComposite))).Methods("POST")
	s.Handle("/composites/{key}", requireAuth(1, http.HandlerFunc(h.DeleteComposite))).Methods("DELETE")
}

type (
	passTypeDTO struct {
		Code        string `json:"code"`
		DatasetFile string `json:"dataset_file"`
		RawDataFile string `json:"rawdata_file"`
		Downlink    string `json:"downlink"`
	}
	folderIncludeDTO struct {
		ID           int64  `json:"id,omitempty"`
		Prefix       string `json:"prefix"`
		PassTypeID   int64  `json:"pass_type_id,omitempty"`
		PassTypeCode string `json:"pass_type_code"`
	}
	imageDirDTO struct {
		ID          int64  `json:"id,omitempty"`
		DirName     string `json:"dir_name"`
		Sensor      string `json:"sensor"`
		IsFilled    bool   `json:"is_filled"`
		VPix        int    `json:"v_pix"`
		IsCorrected bool   `json:"is_corrected"`
		Composite   string `json:"composite"`
	}
	compositeDTO struct {
		Key     string `json:"key"`
		Name    string `json:"name"`
		Enabled *bool  `json:"enabled,omitempty"`
	}
)

func (h *TemplatesAdminAPI) ListPassTypes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.Prefs.ListPassTypes(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := make([]passTypeDTO, 0, len(rows))
	for _, p := range rows {
		out = append(out, passTypeDTO{Code: p.Code, DatasetFile: p.DatasetFile, RawDataFile: p.RawDataFile, Downlink: p.Downlink})
	}
	writeJSON(w, 200, out)
}

func (h *TemplatesAdminAPI) UpsertPassType(w http.ResponseWriter, r *http.Request) {
	var in passTypeDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if in.Code == "" {
		badRequest(w, "code required")
		return
	}
	_, err := h.Prefs.UpsertPassType(r.Context(), in.Code, in.DatasetFile, in.RawDataFile, in.Downlink)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *TemplatesAdminAPI) DeletePassType(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	if code == "" {
		badRequest(w, "code required")
		return
	}
	if u, err := url.PathUnescape(code); err == nil {
		code = u
	}
	if err := h.Prefs.DeletePassType(r.Context(), code); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *TemplatesAdminAPI) ListFolderIncludes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.Prefs.ListFolderIncludes(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := make([]folderIncludeDTO, 0, len(rows))
	for _, f := range rows {
		out = append(out, folderIncludeDTO{ID: f.ID, Prefix: f.Prefix, PassTypeID: f.PassTypeID, PassTypeCode: f.PassTypeCode})
	}
	writeJSON(w, 200, out)
}

func (h *TemplatesAdminAPI) UpsertFolderInclude(w http.ResponseWriter, r *http.Request) {
	var in folderIncludeDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid json")
		return
	}
	if in.Prefix == "" || in.PassTypeCode == "" {
		badRequest(w, "prefix and pass_type_code required")
		return
	}
	_, err := h.Prefs.UpsertFolderInclude(r.Context(), in.Prefix, in.PassTypeCode)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *TemplatesAdminAPI) DeleteFolderInclude(w http.ResponseWriter, r *http.Request) {
	prefix := mux.Vars(r)["prefix"]
	if prefix == "" {
		badRequest(w, "prefix required")
		return
	}
	if u, err := url.PathUnescape(prefix); err == nil {
		prefix = u
	}
	if err := h.Prefs.DeleteFolderInclude(r.Context(), prefix); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *TemplatesAdminAPI) ListImageDirRules(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	if code == "" {
		badRequest(w, "code required")
		return
	}
	if u, err := url.PathUnescape(code); err == nil {
		code = u
	}
	rows, err := h.Prefs.ListImageDirRules(r.Context(), code)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := make([]imageDirDTO, 0, len(rows))
	for _, it := range rows {
		out = append(out, imageDirDTO{
			ID: it.ID, DirName: it.DirName, Sensor: it.Sensor, IsFilled: it.IsFilled, VPix: it.VPix, IsCorrected: it.IsCorrected, Composite: it.Composite,
		})
	}
	writeJSON(w, 200, out)
}

func (h *TemplatesAdminAPI) UpsertImageDirRule(w http.ResponseWriter, r *http.Request) {
	code := mux.Vars(r)["code"]
	if code == "" {
		badRequest(w, "code required")
		return
	}
	if u, err := url.PathUnescape(code); err == nil {
		code = u
	}
	var in imageDirDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badRequest(w, "invalid json")
		return
	}
	// Allow empty dir_name to represent root
	if _, err := h.Prefs.UpsertImageDirRule(r.Context(), code, in.DirName, in.Sensor, in.IsFilled, in.VPix, in.IsCorrected, in.Composite); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *TemplatesAdminAPI) DeleteImageDirRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	code := vars["code"]
	dir := vars["dir"]
	if code == "" {
		badRequest(w, "code required")
		return
	}
	if u, err := url.PathUnescape(code); err == nil {
		code = u
	}
	if dir == "__ROOT__" {
		dir = ""
	}
	if u, err := url.PathUnescape(dir); err == nil {
		dir = u
	}
	if err := h.Prefs.DeleteImageDirRule(r.Context(), code, dir); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *TemplatesAdminAPI) ListComposites(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	rows, err := h.Prefs.ListConfiguredComposites(ctx)
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	out := make([]compositeDTO, 0, len(rows))
	for _, c := range rows {
		en := c.Enabled
		out = append(out, compositeDTO{Key: c.Key, Name: c.Name, Enabled: &en})
	}
	writeJSON(w, 200, out)
}

func (h *TemplatesAdminAPI) UpsertComposite(w http.ResponseWriter, r *http.Request) {
	var in compositeDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if in.Key == "" || in.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key and name required"})
		return
	}
	en := true
	if in.Enabled != nil {
		en = *in.Enabled
	}
	if err := h.Prefs.UpsertComposite(r.Context(), in.Key, in.Name, en); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *TemplatesAdminAPI) DeleteComposite(w http.ResponseWriter, r *http.Request) {
	key := mux.Vars(r)["key"]
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key required"})
		return
	}
	if u, err := url.PathUnescape(key); err == nil {
		key = u
	}
	if err := h.Prefs.DeleteComposite(r.Context(), key); err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
