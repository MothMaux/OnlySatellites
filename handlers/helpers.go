package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type apiErr struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type apiOK[T any] struct {
	OK   bool `json:"ok"`
	Data T    `json:"data"`
}

func (h *APIHandler) parseDateTime(dateStr, timeStr string) int64 {
	parts := strings.Split(dateStr, "-")
	if len(parts) != 3 {
		return 0
	}
	year, _ := strconv.Atoi(parts[0])
	month, _ := strconv.Atoi(parts[1])
	day, _ := strconv.Atoi(parts[2])

	tp := strings.Split(timeStr, ":")
	if len(tp) != 2 {
		return 0
	}
	hour, _ := strconv.Atoi(tp[0])
	minute, _ := strconv.Atoi(tp[1])

	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, time.UTC).Unix()
}

func (h *APIHandler) parseTimeString(timeStr string) int {
	tp := strings.Split(timeStr, ":")
	if len(tp) != 2 {
		return 0
	}
	hour, _ := strconv.Atoi(tp[0])
	minute, _ := strconv.Atoi(tp[1])
	seconds := hour*3600 + minute*60
	return seconds
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func parseID(vars map[string]string, key string) (int64, error) {
	raw := vars[key]
	if raw == "" {
		return 0, errors.New("missing id")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("invalid id")
	}
	return id, nil
}

func badRequest(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, apiErr{OK: false, Error: msg})
}

func notFound(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusNotFound, apiErr{OK: false, Error: msg})
}

func serverErr(w http.ResponseWriter, err error) {
	writeJSON(w, http.StatusInternalServerError, apiErr{OK: false, Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// joins root + rel and ensures the result stays within root
func safeJoin(root, rel string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	// normalize rel (handle / and \ from DB)
	rel = filepath.FromSlash(strings.ReplaceAll(rel, "\\", "/"))
	// strip any leading separators to avoid treating as absolute
	rel = strings.TrimLeft(rel, `/\`)
	full := filepath.Join(rootAbs, rel)
	full = filepath.Clean(full)

	// Ensure full is within root scope
	if relPath, err := filepath.Rel(rootAbs, full); err != nil || strings.HasPrefix(relPath, "..") {
		return "", errors.New("path escapes root")
	}
	return full, nil
}

func sanitizeAndResolve(base, reqPath string) (string, error) {
	if base == "" {
		return "", errors.New("base directory not configured")
	}
	// Decode-style safety: reject NUL and normalize slashes
	if strings.ContainsRune(reqPath, '\x00') {
		return "", errors.New("invalid characters in path")
	}
	// Force platform-native separators and clean
	reqPath = filepath.Clean(reqPath)
	// Remove any leading separators to keep it relative
	reqPath = strings.TrimLeft(reqPath, string(filepath.Separator))

	full := filepath.Join(base, reqPath)

	// Resolve symlinks to defend against symlink escapes
	baseResolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	fullResolved, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}

	// Must be inside base (or equal to base)
	baseWithSep := baseResolved + string(filepath.Separator)
	if !(strings.HasPrefix(fullResolved, baseWithSep) || fullResolved == baseResolved) {
		return "", errors.New("path escapes live_output boundary")
	}
	return fullResolved, nil
}

func nullStr(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}
func nullI64(ni sql.NullInt64) int64 {
	if ni.Valid {
		return ni.Int64
	}
	return 0
}

// int->string
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var a [6]byte
	n := len(a)
	for i > 0 {
		n--
		a[n] = byte('0' + i%10)
		i /= 10
	}
	return string(a[n:])
}

func jsonToInt(v any) (int, error) {
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, nil
		}
		i, err := strconv.Atoi(s)
		if err != nil {
			return 0, err
		}
		return i, nil
	case nil:
		return 0, nil
	default:
		return 0, fmt.Errorf("invalid number type %T", v)
	}
}

func parseInt64Default(s string, def int64) int64 {
	if strings.TrimSpace(s) == "" {
		return def
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return def
	}
	return v
}
