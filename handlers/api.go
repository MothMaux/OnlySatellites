package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"time"

	"OnlySats/com/shared"
)

type APIHandler struct {
	DB *shared.Database
}

func NewAPIHandler(db *shared.Database) *APIHandler {
	return &APIHandler{DB: db}
}

type GalleryImage struct {
	ID          int     `json:"id"`
	Path        string  `json:"path"`
	Composite   string  `json:"composite"`
	Sensor      string  `json:"sensor"`
	MapOverlay  int     `json:"mapOverlay"`
	Corrected   int     `json:"corrected"`
	Filled      int     `json:"filled"`
	VPixels     *int    `json:"vPixels"`
	PassID      int     `json:"passId"`
	Timestamp   int64   `json:"timestamp"`
	Satellite   string  `json:"satellite"`
	Name        string  `json:"name"`
	RawDataPath *string `json:"rawDataPath"`
}

type ImageResponse struct {
	Images []GalleryImage `json:"images"`
	Total  int            `json:"total"`
	Page   int            `json:"page"`
	Limit  int            `json:"limit"`
}

type QueryFilters struct {
	MapOverlay    bool
	CorrectedOnly bool
	FilledOnly    bool

	Satellite string
	Band      string

	StartDate string
	EndDate   string
	StartTime string
	EndTime   string

	CompositeKeys []string

	Page      int
	Limit     int
	SortBy    string
	SortOrder string

	LimitType string
}

// HTTP

func (h *APIHandler) GetImages(w http.ResponseWriter, r *http.Request) {
	f := h.parseQueryFilters(r)

	whereSQL, args := h.buildWhere(f)

	var (
		images []GalleryImage
		total  int
		err    error
	)

	if f.LimitType == "passes" {
		images, total, err = h.queryByPasses(whereSQL, args, f)
	} else {
		images, total, err = h.queryByImages(whereSQL, args, f)
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("Database error: %v", err), http.StatusInternalServerError)
		return
	}

	resp := ImageResponse{
		Images: images,
		Total:  total,
		Page:   f.Page,
		Limit:  f.Limit,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// Filters & WHERE

func (h *APIHandler) parseQueryFilters(r *http.Request) QueryFilters {
	q := r.URL.Query()

	mapOverlay := false
	if v := strings.ToLower(strings.TrimSpace(q.Get("mapsOnly"))); v == "1" || v == "true" {
		mapOverlay = true
	}
	correctedOnly := false
	if v := strings.ToLower(strings.TrimSpace(q.Get("correctedOnly"))); v == "1" || v == "true" {
		correctedOnly = true
	}
	filledOnly := false
	if v := strings.ToLower(strings.TrimSpace(q.Get("filledOnly"))); v == "1" || v == "true" {
		filledOnly = true
	}

	// composite filters (multi)
	compKeys := q["composite"]

	// base
	f := QueryFilters{
		MapOverlay:    mapOverlay,
		CorrectedOnly: correctedOnly,
		FilledOnly:    filledOnly,
		Satellite:     q.Get("satellite"),
		Band:          q.Get("band"),
		StartDate:     q.Get("startDate"),
		EndDate:       q.Get("endDate"),
		StartTime:     q.Get("startTime"),
		EndTime:       q.Get("endTime"),

		Page:      1,
		Limit:     50,
		SortBy:    "timestamp",
		SortOrder: "DESC",
		LimitType: strings.ToLower(strings.TrimSpace(q.Get("limitType"))),
	}

	// pagination
	if v := strings.TrimSpace(q.Get("page")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			f.Page = n
		}
	}
	if v := strings.TrimSpace(q.Get("limit")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			f.Limit = n
		}
	}
	if v := strings.TrimSpace(q.Get("sortBy")); v != "" {
		switch strings.ToLower(v) {
		case "vpixels", "images.vpixels":
			f.SortBy = "vPixels"
		default:
			f.SortBy = "timestamp"
		}
	}
	if v := strings.TrimSpace(q.Get("sortOrder")); v != "" {
		if strings.ToUpper(v) == "ASC" {
			f.SortOrder = "ASC"
		} else {
			f.SortOrder = "DESC"
		}
	}
	if f.LimitType != "passes" {
		f.LimitType = "images"
	}

	// composites
	for _, k := range compKeys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		f.CompositeKeys = append(f.CompositeKeys, k)
	}

	return f
}

func (h *APIHandler) buildWhere(f QueryFilters) (string, []any) {
	var conditions []string
	var args []any

	// image-level filters
	if f.MapOverlay {
		conditions = append(conditions, "images.mapOverlay = 1")
	}
	if f.CorrectedOnly {
		conditions = append(conditions, "images.corrected = 1")
	}
	if f.FilledOnly {
		conditions = append(conditions, "images.filled = 1")
	}

	// composite filters — exact label match only (including "Other" as a normal label)
	if len(f.CompositeKeys) > 0 {
		// Normalize to lowercase and dedupe the requested labels
		selSet := make(map[string]struct{}, len(f.CompositeKeys))
		for _, s := range f.CompositeKeys {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			selSet[strings.ToLower(s)] = struct{}{}
		}

		if len(selSet) > 0 {
			// WHERE LOWER(images.composite) IN (?, ?, ...)
			placeholders := make([]string, 0, len(selSet))
			for range selSet {
				placeholders = append(placeholders, "?")
			}
			conditions = append(conditions, "LOWER(images.composite) IN ("+strings.Join(placeholders, ",")+")")
			for s := range selSet {
				args = append(args, s)
			}
		}
	}

	// pass-level filters
	if s := strings.TrimSpace(f.Satellite); s != "" {
		conditions = append(conditions, "passes.satellite = ?")
		args = append(args, s)
	}
	if b := strings.TrimSpace(f.Band); b != "" {
		conditions = append(conditions, "passes.downlink = ?")
		args = append(args, b)
	}

	// date range
	if f.StartDate != "" {
		start := h.parseDateTime(f.StartDate, "00:00")
		conditions = append(conditions, "passes.timestamp >= ?")
		args = append(args, start)
	}
	if f.EndDate != "" {
		end := h.parseDateTime(f.EndDate, "23:59")
		conditions = append(conditions, "passes.timestamp <= ?")
		args = append(args, end)
	}

	// time-of-day window (seconds modulo 86400)
	todExpr := "(passes.timestamp % 86400)"

	if f.StartTime != "" && f.EndTime != "" {
		startSeconds := h.parseTimeString(f.StartTime)
		endSeconds := h.parseTimeString(f.EndTime)

		if startSeconds <= endSeconds {
			conditions = append(conditions, todExpr+" >= ? AND "+todExpr+" <= ?")
			args = append(args, startSeconds, endSeconds)
		} else {
			// Wrap midnight
			conditions = append(conditions, "("+todExpr+" >= ? OR "+todExpr+" <= ?)")
			args = append(args, startSeconds, endSeconds)
		}
	} else if f.StartTime != "" {
		startSeconds := h.parseTimeString(f.StartTime)
		conditions = append(conditions, todExpr+" >= ?")
		args = append(args, startSeconds)
	} else if f.EndTime != "" {
		endSeconds := h.parseTimeString(f.EndTime)
		conditions = append(conditions, todExpr+" <= ?")
		args = append(args, endSeconds)
	}

	if len(conditions) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(conditions, " AND "), args
}

// Queries

func (h *APIHandler) queryByImages(whereSQL string, args []any, f QueryFilters) ([]GalleryImage, int, error) {
	sortCol := "passes.timestamp"
	if f.SortBy == "vPixels" {
		sortCol = "images.vPixels"
	}
	sortDir := f.SortOrder

	limit := clamp(f.Limit, 1, 500)
	offset := 0
	if f.Page > 1 {
		offset = (f.Page - 1) * limit
	}

	// Count
	countSQL := `
		SELECT COUNT(*)
		FROM images
		JOIN passes ON images.passId = passes.id
	` + " " + whereSQL
	var total int
	if err := h.DB.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Data
	selectSQL := `
		SELECT
			images.id, images.path, images.composite, images.sensor,
			images.mapOverlay, images.corrected, images.filled,
			images.vPixels, images.passId,
			passes.timestamp, COALESCE(passes.satellite,'Unknown'), passes.name, passes.rawDataPath
		FROM images
		JOIN passes ON images.passId = passes.id
	` + " " + whereSQL + `
		ORDER BY ` + sortCol + " " + sortDir + `
		LIMIT ? OFFSET ?
	`

	argsWithPaging := append(append([]any{}, args...), limit, offset)
	rows, err := h.DB.Query(selectSQL, argsWithPaging...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]GalleryImage, 0, limit)
	for rows.Next() {
		var gi GalleryImage
		if err := rows.Scan(
			&gi.ID, &gi.Path, &gi.Composite, &gi.Sensor,
			&gi.MapOverlay, &gi.Corrected, &gi.Filled,
			&gi.VPixels, &gi.PassID,
			&gi.Timestamp, &gi.Satellite, &gi.Name, &gi.RawDataPath,
		); err != nil {
			return nil, 0, err
		}
		gi.Path = strings.ReplaceAll(gi.Path, `\`, `/`)
		out = append(out, gi)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	return out, total, nil
}

// Pass-limited: pick pass set from *filtered images*, then return only those filtered images.
func (h *APIHandler) queryByPasses(whereSQL string, args []any, f QueryFilters) ([]GalleryImage, int, error) {
	limit := clamp(f.Limit, 1, 200)
	offset := 0
	if f.Page > 1 {
		offset = (f.Page - 1) * limit
	}

	// rewrite WHERE for CTE aliases i/p
	whereForCTE := strings.ReplaceAll(whereSQL, "images.", "i.")
	whereForCTE = strings.ReplaceAll(whereForCTE, "passes.", "p.")

	countSQL := `
    WITH filtered AS (
        SELECT i.passId
        FROM images i
        JOIN passes p ON i.passId = p.id
        ` + " " + whereForCTE + `
    )
    SELECT COUNT(*) FROM (SELECT DISTINCT passId FROM filtered);
`
	var total int
	if err := h.DB.QueryRow(countSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	var sql string
	if f.SortBy == "vPixels" {
		sql = `
			WITH filtered AS (
				SELECT
					i.*,
					p.timestamp    AS p_timestamp,
					p.satellite    AS p_satellite,
					p.name         AS p_name,
					p.rawDataPath  AS p_rawDataPath
				FROM images i
				JOIN passes p ON i.passId = p.id
				` + " " + whereForCTE + `
			),
			pass_metrics AS (
				SELECT passId, MAX(vPixels) AS maxVPixels
				FROM filtered
				GROUP BY passId
			),
			selected_passes AS (
				SELECT pm.passId AS id
				FROM pass_metrics pm
				JOIN passes p ON p.id = pm.passId
				ORDER BY pm.maxVPixels ` + f.SortOrder + `, p.timestamp DESC
				LIMIT ? OFFSET ?
			)
			SELECT
				f.id, f.path, f.composite, f.sensor,
				f.mapOverlay, f.corrected, f.filled,
				f.vPixels, f.passId,
				f.p_timestamp, COALESCE(f.p_satellite,'Unknown'), f.p_name, f.p_rawDataPath
			FROM filtered f
			JOIN selected_passes sp ON f.passId = sp.id
			ORDER BY f.p_timestamp DESC, f.id ASC
		`
	} else {
		sql = `
			WITH filtered AS (
				SELECT
					i.*,
					p.timestamp    AS p_timestamp,
					p.satellite    AS p_satellite,
					p.name         AS p_name,
					p.rawDataPath  AS p_rawDataPath
				FROM images i
				JOIN passes p ON i.passId = p.id
				` + " " + whereForCTE + `
			),
			selected_passes AS (
				SELECT passId AS id, MAX(p_timestamp) AS max_ts
				FROM filtered
				GROUP BY passId
				ORDER BY max_ts ` + f.SortOrder + `
				LIMIT ? OFFSET ?
			)
			SELECT
				f.id, f.path, f.composite, f.sensor,
				f.mapOverlay, f.corrected, f.filled,
				f.vPixels, f.passId,
				f.p_timestamp, COALESCE(f.p_satellite,'Unknown'), f.p_name, f.p_rawDataPath
			FROM filtered f
			JOIN selected_passes sp ON f.passId = sp.id
			ORDER BY f.p_timestamp ` + f.SortOrder + `, f.id ASC
		`
	}

	argsFinal := append(append([]any{}, args...), limit, offset)

	rows, err := h.DB.Query(sql, argsFinal...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []GalleryImage
	for rows.Next() {
		var gi GalleryImage
		if err := rows.Scan(
			&gi.ID, &gi.Path, &gi.Composite, &gi.Sensor,
			&gi.MapOverlay, &gi.Corrected, &gi.Filled,
			&gi.VPixels, &gi.PassID,
			&gi.Timestamp, &gi.Satellite, &gi.Name, &gi.RawDataPath,
		); err != nil {
			return nil, 0, err
		}
		gi.Path = strings.ReplaceAll(gi.Path, `\`, `/`)
		out = append(out, gi)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

type ShareImageMeta struct {
	ID        int
	Path      string
	Satellite string
	Timestamp int64
	Composite string
	Sensor    string
}

func (h *APIHandler) queryShareMetaByID(id int) (*ShareImageMeta, error) {
	const q = `
SELECT
  images.id,
  REPLACE(images.path, '\', '/') AS path_norm,
  COALESCE(passes.satellite,'Unknown') AS satellite,
  passes.timestamp,
  images.composite,
  images.sensor
FROM images
JOIN passes ON images.passId = passes.id
WHERE images.id = ?
LIMIT 1;
`
	var m ShareImageMeta
	if err := h.DB.QueryRow(q, id).Scan(&m.ID, &m.Path, &m.Satellite, &m.Timestamp, &m.Composite, &m.Sensor); err != nil {
		return nil, err
	}
	return &m, nil
}

func (h *APIHandler) ShareImageByID(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/api/share/images/")
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		http.NotFound(w, r)
		return
	}

	id, err := strconv.Atoi(rel)
	if err != nil || id <= 0 {
		http.NotFound(w, r)
		return
	}

	meta, err := h.queryShareMetaByID(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "Database error", http.StatusInternalServerError)
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if xf := r.Header.Get("X-Forwarded-Proto"); xf == "https" || xf == "http" {
		scheme = xf
	}
	host := r.Host
	if xh := r.Header.Get("X-Forwarded-Host"); xh != "" {
		host = xh
	}

	// html content
	shareURL := fmt.Sprintf("%s://%s%s", scheme, host, r.URL.Path)

	imageURL := fmt.Sprintf("%s://%s/images/%s", scheme, host, meta.Path)

	title := meta.Satellite
	tsUTC := time.Unix(meta.Timestamp, 0).UTC().Format("2006-01-02 15:04:05 UTC")
	desc := fmt.Sprintf("%s • %s \n%s", meta.Composite, meta.Sensor, tsUTC)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	fmt.Fprint(w, "<!doctype html><html><head>")
	fmt.Fprint(w, `<meta charset="utf-8">`)
	fmt.Fprintf(w, `<title>%s</title>`, html.EscapeString(title))

	fmt.Fprint(w, `<meta property="og:type" content="website">`)
	fmt.Fprintf(w, `<meta property="og:title" content="%s">`, html.EscapeString(title))
	fmt.Fprintf(w, `<meta property="og:description" content="%s">`, html.EscapeString(desc))
	fmt.Fprintf(w, `<meta property="og:url" content="%s">`, html.EscapeString(shareURL))
	fmt.Fprintf(w, `<meta property="og:image" content="%s">`, html.EscapeString(imageURL))

	fmt.Fprint(w, `<meta name="twitter:card" content="summary_large_image">`)
	fmt.Fprintf(w, `<meta name="twitter:title" content="%s">`, html.EscapeString(title))
	fmt.Fprintf(w, `<meta name="twitter:description" content="%s">`, html.EscapeString(desc))
	fmt.Fprintf(w, `<meta name="twitter:image" content="%s">`, html.EscapeString(imageURL))

	fmt.Fprint(w, `</head><body style="margin:0;font-family:system-ui,sans-serif;">`)
	fmt.Fprint(w, `<div style="padding:12px 16px;">`)
	fmt.Fprintf(w, `<h1 style="margin:0 0 6px 0;font-size:18px;">%s</h1>`, html.EscapeString(title))
	fmt.Fprintf(w, `<div style="opacity:.75;font-size:13px;margin-bottom:10px;">%s</div>`, html.EscapeString(desc))
	fmt.Fprintf(w, `<img src="%s" alt="%s" style="max-width:100%%;height:auto;display:block;">`, html.EscapeString(imageURL), html.EscapeString(title))
	fmt.Fprint(w, `</div></body></html>`)
}
