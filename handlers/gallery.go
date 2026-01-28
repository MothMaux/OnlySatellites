package handlers

import (
	"OnlySats/com"
	"archive/zip"
	"context"
	"database/sql"
	"encoding/json"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type GalleryAPI struct {
	DB            *sql.DB
	LiveOutputDir string
	UserContent   string
	LocalStore    *com.LocalDataStore
}

type compEntry struct {
	Key     string
	Label   string
	Enabled bool
}

// ---------- HTML Page ----------

type GalleryPageData struct {
	Mode          string
	Simplified    bool
	InitialDataJS template.JS
	Limit         int
}

func getLimit(api *GalleryAPI) (li int) {
	limit := 15
	if api.LocalStore != nil {
		if s, err := api.LocalStore.GetSetting(context.Background(), "pass_limit"); err == nil {
			if v, err2 := strconv.Atoi(strings.TrimSpace(s)); err2 == nil && v > 0 {
				limit = v
			}
		}
	}
	return limit
}

func GalleryHandler(htmlFS fs.FS, api *GalleryAPI) (http.HandlerFunc, *template.Template, error) {
	tpl, err := template.New("gallery.html").
		ParseFS(htmlFS, "gallery.html", "partials/advanced-view.html", "partials/simplified-view.html")
	if err != nil {
		return nil, nil, err
	}

	limit := getLimit(api)

	h := func(w http.ResponseWriter, r *http.Request) {
		mode := strings.ToLower(r.URL.Query().Get("mode"))
		if mode != "advanced" && mode != "simple" {
			mode = "simple" // default simplified view
		}
		data := GalleryPageData{
			Mode:          mode,
			Simplified:    (mode == "simple"),
			InitialDataJS: template.JS("[]"),
			Limit:         limit,
		}
		if data.Simplified {
			if js, err := api.preloadSimplifiedJSON(); err == nil {
				data.InitialDataJS = template.JS(js)
			}
		}
		if err := tpl.Execute(w, data); err != nil {
			http.Error(w, "template rendering failed", http.StatusInternalServerError)
		}
	}
	return h, tpl, nil
}

func (api *GalleryAPI) preloadSimplifiedJSON() (string, error) {
	limit := getLimit(api)

	const q = `
WITH recent_passes AS (
  SELECT DISTINCT p.id, p.timestamp, p.satellite, p.rawDataPath, p.name
  FROM passes p
  JOIN images i ON p.id = i.passId
  WHERE i.corrected = 1 AND i.filled = 1
  ORDER BY p.timestamp DESC
  LIMIT ?
)
SELECT i.id, i.path, i.composite, i.sensor, i.mapOverlay, i.corrected, i.filled, i.vPixels, i.passId,
       rp.timestamp, rp.satellite, rp.rawDataPath, rp.name
FROM images i
JOIN recent_passes rp ON i.passId = rp.id
WHERE i.corrected = 1 AND i.filled = 1
ORDER BY rp.timestamp DESC, i.id ASC;
`
	rows, err := api.DB.Query(q, limit)
	if err != nil {
		return "[]", err
	}
	defer rows.Close()

	type row struct {
		ID         int
		Path       string
		Composite  string
		Sensor     string
		MapOverlay sql.NullInt64
		Corrected  sql.NullInt64
		Filled     sql.NullInt64
		VPixels    sql.NullInt64
		PassID     int
		Timestamp  sql.NullInt64
		Satellite  sql.NullString
		RawData    sql.NullString
		PassName   sql.NullString
	}

	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(
			&r.ID, &r.Path, &r.Composite, &r.Sensor, &r.MapOverlay, &r.Corrected, &r.Filled, &r.VPixels, &r.PassID,
			&r.Timestamp, &r.Satellite, &r.RawData, &r.PassName,
		); err != nil {
			return "[]", err
		}
		if r.Corrected.Valid && r.Corrected.Int64 == 1 && r.Filled.Valid && r.Filled.Int64 == 1 {
			all = append(all, r)
		}
	}
	disabled := api.disabledLabelSet(context.Background())
	if len(disabled) > 0 {
		filtered := make([]row, 0, len(all))
		for _, r := range all {
			keep := true
			rc := strings.ToLower(strings.TrimSpace(r.Composite))
			for key := range disabled {
				if key != "" && strings.Contains(rc, key) {
					keep = false
					break
				}
			}
			if keep {
				filtered = append(filtered, r)
			}
		}
		all = filtered
	}

	type imgOut struct {
		ID         int    `json:"id"`
		Path       string `json:"path"`
		Composite  string `json:"composite"`
		Sensor     string `json:"sensor"`
		MapOverlay int64  `json:"mapOverlay"`
		Corrected  int64  `json:"corrected"`
		Filled     int64  `json:"filled"`
		VPixels    int64  `json:"vPixels"`
		PassID     int    `json:"passId"`
	}

	type passOut struct {
		Satellite string   `json:"satellite"`
		Timestamp int64    `json:"timestamp"`
		Name      string   `json:"name"`
		RawData   string   `json:"rawDataPath"`
		Images    []imgOut `json:"images"`
	}

	grouped := map[int]*passOut{}

	for _, r := range all {
		p := grouped[r.PassID]
		if p == nil {
			p = &passOut{
				Satellite: nullStr(r.Satellite),
				Timestamp: nullI64(r.Timestamp),
				Name:      nullStr(r.PassName),
				RawData:   nullStr(r.RawData),
				Images:    make([]imgOut, 0, 8),
			}
			grouped[r.PassID] = p
		}

		rel := filepath.ToSlash(r.Path)
		passName := filepath.ToSlash(nullStr(r.PassName))
		if passName != "" && !strings.HasPrefix(rel, passName+"/") {
			rel = passName + "/" + rel
		}

		img := imgOut{
			ID:         r.ID,
			Path:       rel,
			Composite:  r.Composite,
			Sensor:     r.Sensor,
			MapOverlay: nullI64(r.MapOverlay),
			Corrected:  nullI64(r.Corrected),
			Filled:     nullI64(r.Filled),
			VPixels:    nullI64(r.VPixels),
			PassID:     r.PassID,
		}
		p.Images = append(p.Images, img)
	}

	// Flatten and sort by pass timestamp DESC
	out := make([]passOut, 0, len(grouped))
	for _, v := range grouped {
		out = append(out, *v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })

	b, _ := json.Marshal(out)
	return string(b), nil
}

func (api *GalleryAPI) Satellites() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := api.DB.Query(`
SELECT DISTINCT p.satellite
FROM images i
JOIN passes p ON i.passId = p.id
WHERE p.satellite IS NOT NULL
ORDER BY p.satellite DESC`)
		if err != nil {
			http.Error(w, "query error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s sql.NullString
			if err := rows.Scan(&s); err == nil && s.Valid {
				out = append(out, s.String)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func (api *GalleryAPI) Bands() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := api.DB.Query(`
SELECT DISTINCT p.downlink
FROM images i
JOIN passes p ON i.passId = p.id
WHERE p.downlink IS NOT NULL
ORDER BY p.downlink ASC`)
		if err != nil {
			http.Error(w, "query error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s sql.NullString
			if err := rows.Scan(&s); err == nil && s.Valid {
				out = append(out, s.String)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

func (api *GalleryAPI) CompositesList() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		sat := strings.TrimSpace(r.URL.Query().Get("satellite"))

		// Pull unique image composites (labels) from images
		var rows *sql.Rows
		var err error
		if sat != "" {
			rows, err = api.DB.Query(`
                SELECT DISTINCT i.composite
                FROM images i
                JOIN passes p ON i.passId = p.id
                WHERE p.satellite = ?`, sat)
		} else {
			rows, err = api.DB.Query(`SELECT DISTINCT composite FROM images`)
		}
		if err != nil {
			http.Error(w, "query error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		// raw set of labels present in images
		raw := map[string]string{} // lower -> original
		for rows.Next() {
			var c sql.NullString
			if err := rows.Scan(&c); err == nil && c.Valid {
				lbl := strings.TrimSpace(c.String)
				if lbl != "" {
					raw[strings.ToLower(lbl)] = lbl
				}
			}
		}

		// load configured entries (labels + enabled)
		entries, _ := api.loadCompositeEntries(ctx)

		// choose labels that are both enabled and present in images
		outSet := map[string]struct{}{}
		matchedAny := map[string]struct{}{}
		for _, e := range entries {
			if !e.Enabled {
				continue
			}
			lbl := strings.TrimSpace(e.Label)
			if lbl == "" {
				continue
			}
			ll := strings.ToLower(lbl)

			// exact or substring match vs the raw image composite labels
			found := false
			for k := range raw {
				if k == ll || strings.Contains(k, ll) {
					matchedAny[k] = struct{}{}
					found = true
				}
			}
			if found {
				outSet[lbl] = struct{}{}
			}
		}

		// if there are raw composites that didn't match any enabled label, include "Other"
		hasOther := false
		for k := range raw {
			if _, ok := matchedAny[k]; !ok {
				hasOther = true
				break
			}
		}

		// Build final []string (labels only)
		resp := make([]string, 0, len(outSet)+1)
		for lbl := range outSet {
			resp = append(resp, lbl)
		}
		sort.Slice(resp, func(i, j int) bool {
			return strings.ToLower(resp[i]) < strings.ToLower(resp[j])
		})
		if hasOther {
			resp = append(resp, "Other")
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// streams a single file from LiveOutputDir as a download.
// GET /api/export?path=<relative path to file inside live output>
func (g *GalleryAPI) ExportCADU() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("path")
		if q == "" {
			http.Error(w, "missing 'path' query parameter", http.StatusBadRequest)
			return
		}
		fullPath, err := sanitizeAndResolve(g.LiveOutputDir, q)
		if err != nil {
			http.Error(w, "invalid path: "+err.Error(), http.StatusBadRequest)
			return
		}
		stat, err := os.Stat(fullPath)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "file not found", http.StatusNotFound)
				return
			}
			http.Error(w, "stat error", http.StatusInternalServerError)
			return
		}
		if stat.IsDir() {
			http.Error(w, "requested path is a directory; use /api/zip", http.StatusBadRequest)
			return
		}

		// Set headers and stream
		filename := filepath.Base(fullPath)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
		w.Header().Set("Last-Modified", stat.ModTime().UTC().Format(http.TimeFormat))

		f, err := os.Open(fullPath)
		if err != nil {
			http.Error(w, "open error", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		// Best-effort Content-Length
		w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))

		if _, err := io.Copy(w, f); err != nil {
			// Client aborted or write error — don't send another header
			_ = err
			return
		}
	}
}

// streams a ZIP of a folder rooted inside LiveOutputDir.
// GET /api/zip?path=<relative folder path inside live output>
func (g *GalleryAPI) ZipPath() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("path")
		if q == "" {
			http.Error(w, "missing 'path' query parameter", http.StatusBadRequest)
			return
		}
		root, err := sanitizeAndResolve(g.LiveOutputDir, q)
		if err != nil {
			http.Error(w, "invalid path: "+err.Error(), http.StatusBadRequest)
			return
		}
		stat, err := os.Stat(root)
		if err != nil {
			if os.IsNotExist(err) {
				http.Error(w, "folder not found", http.StatusNotFound)
				return
			}
			http.Error(w, "stat error", http.StatusInternalServerError)
			return
		}
		if !stat.IsDir() {
			http.Error(w, "requested path is not a folder", http.StatusBadRequest)
			return
		}

		baseName := filepath.Base(root)
		if baseName == "." || baseName == string(filepath.Separator) {
			baseName = "export"
		}
		zipName := baseName + ".zip"

		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+zipName+`"`)

		zw := zip.NewWriter(w)
		defer zw.Close()

		// Walk the directory and add files into the ZIP with paths relative to the root
		err = filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			// figure archive path relative to root (use forward slashes inside zip)
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			zipPath := filepath.ToSlash(rel)

			// Include directory entries explicitly so empty dirs are preserved
			if d.IsDir() {
				if zipPath != "." {
					_, err := zw.Create(zipPath + "/")
					return err
				}
				return nil
			}

			// Regular file: copy contents
			fh, err := os.Stat(path)
			if err != nil {
				return err
			}
			hdr, err := zip.FileInfoHeader(fh)
			if err != nil {
				return err
			}
			hdr.Name = zipPath
			// Store as deflated (compressed)
			hdr.Method = zip.Deflate

			wr, err := zw.CreateHeader(hdr)
			if err != nil {
				return err
			}

			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(wr, f)
			return err
		})

		if err != nil {
			// errors mid-stream block header changes; end the response.
			_ = err
			return
		}
	}
}

func (api *GalleryAPI) UserAbout() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		fp := filepath.Join(api.UserContent, "about.txt")
		b, err := os.ReadFile(fp)
		if err != nil {
			http.Error(w, "about.txt not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(b)
	}
}

func (api *GalleryAPI) UserImages() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := os.ReadDir(api.UserContent)
		if err != nil {
			http.Error(w, "failed to read directory", http.StatusInternalServerError)
			return
		}
		var imgs []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			switch ext {
			case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
				imgs = append(imgs, name)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(imgs)
	}
}

// ---------- helpers ----------

func (api *GalleryAPI) loadCompositeEntries(ctx context.Context) ([]compEntry, error) {
	if api.LocalStore == nil {
		return nil, nil
	}

	cfg, _ := api.LocalStore.ListConfiguredComposites(ctx)
	rules, _ := api.LocalStore.ListRuleComposites(ctx)

	out := map[string]compEntry{}

	for _, c := range cfg {
		out[c.Key] = compEntry{
			Key:     c.Key,
			Label:   c.Name,
			Enabled: c.Enabled,
		}
	}

	for _, r := range rules {
		e := out[r.Key]
		e.Key = r.Key
		if e.Label == "" {
			e.Label = r.Name
		}
		e.Enabled = true
		out[r.Key] = e
	}

	res := make([]compEntry, 0, len(out))
	for _, v := range out {
		res = append(res, v)
	}
	return res, nil
}

func (api *GalleryAPI) disabledLabelSet(ctx context.Context) map[string]struct{} {
	m := map[string]struct{}{}
	entries, _ := api.loadCompositeEntries(ctx)
	for _, e := range entries {
		if !e.Enabled && strings.TrimSpace(e.Label) != "" {
			m[strings.ToLower(strings.TrimSpace(e.Label))] = struct{}{}
		}
	}
	return m
}
