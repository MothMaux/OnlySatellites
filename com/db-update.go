package com

import (
	"OnlySats/config"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Pass struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Satellite   string `json:"satellite"`
	Timestamp   int64  `json:"timestamp"`
	RawDataPath string `json:"rawDataPath"`
	Downlink    string `json:"downlink"`
	NeedsRescan uint8  `json:"needsRescan,omitempty"`
}

type Image struct {
	ID         int    `json:"id"`
	Path       string `json:"path"`
	Composite  string `json:"composite"`
	Sensor     string `json:"sensor"`
	MapOverlay uint8  `json:"mapOverlay"`
	Corrected  uint8  `json:"corrected"`
	Filled     uint8  `json:"filled"`
	VPixels    *int   `json:"vPixels"`
	PassID     int    `json:"passId"`
	// NeedsThumb uint8 `json:"needsThumb,omitempty"`
}

type Dataset struct {
	Satellite string  `json:"satellite"`
	Timestamp float64 `json:"timestamp"`
}

type updCtx struct {
	cfg           *config.AppConfig
	passCfg       *config.PassConfig
	db            *sql.DB
	liveOutputDir string
}

type existingPassData struct {
	id          int64
	needsRescan uint8
}

// load PassConfig from prefs SQLite

func loadPassConfigFromPrefs(ctx context.Context, prefsDBPath string) (*config.PassConfig, error) {
	if strings.TrimSpace(prefsDBPath) == "" {
		return nil, errors.New("prefs db path empty")
	}
	if _, err := os.Stat(prefsDBPath); err != nil {
		return nil, fmt.Errorf("prefs db not found: %w", err)
	}

	pdb, err := sql.Open("sqlite3", prefsDBPath)
	if err != nil {
		return nil, fmt.Errorf("open prefs db: %w", err)
	}
	defer pdb.Close()

	if _, err := pdb.Exec(`PRAGMA foreign_keys=ON;`); err != nil {
		return nil, fmt.Errorf("prefs pragma: %w", err)
	}

	out := &config.PassConfig{
		Composites: map[string]string{},
		PassTypes:  map[string]config.PassTypeConfig{},
		Passes:     config.PassesConfig{FolderIncludes: map[string]string{}},
	}

	// composites
	{
		rows, err := pdb.QueryContext(ctx, `SELECT key, label FROM composites`)
		if err != nil {
			return nil, fmt.Errorf("query composites: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				return nil, err
			}
			out.Composites[k] = v
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// Detect if pass_types has rawdata_file
	hasRawData := false
	{
		row := pdb.QueryRowContext(ctx, `SELECT 1 FROM pragma_table_info('pass_types') WHERE name='rawdata_file'`)
		var dummy int
		if err := row.Scan(&dummy); err == nil {
			hasRawData = true
		}
	}

	// pass_types
	type passRow struct {
		id          int64
		code        string
		datasetFile sql.NullString
		rawDataFile sql.NullString
		downlink    sql.NullString
	}
	var passRows []passRow
	{
		q := `SELECT id, code, dataset_file, downlink FROM pass_types`
		if hasRawData {
			q = `SELECT id, code, dataset_file, rawdata_file, downlink FROM pass_types`
		}
		rows, err := pdb.QueryContext(ctx, q)
		if err != nil {
			return nil, fmt.Errorf("query pass_types: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var r passRow
			if hasRawData {
				if err := rows.Scan(&r.id, &r.code, &r.datasetFile, &r.rawDataFile, &r.downlink); err != nil {
					return nil, err
				}
			} else {
				if err := rows.Scan(&r.id, &r.code, &r.datasetFile, &r.downlink); err != nil {
					return nil, err
				}
			}
			passRows = append(passRows, r)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// image_dir_rules per pass_type
	for _, pr := range passRows {
		pt := config.PassTypeConfig{
			DatasetFile: strings.TrimSpace(pr.datasetFile.String),
			Downlink:    strings.TrimSpace(pr.downlink.String),
			ImageDirs:   map[string]config.ImageDirConfig{},
		}
		// If config.PassTypeConfig has RawDataFile, populate it:
		pt.RawDataFile = strings.TrimSpace(pr.rawDataFile.String) // empty when column missing

		hasComposite := false
		{
			row := pdb.QueryRowContext(ctx, `SELECT 1 FROM pragma_table_info('image_dir_rules') WHERE name='composite'`)
			var dummy int
			if row.Scan(&dummy) == nil {
				hasComposite = true
			}
		}

		// image_dir_rules per pass_type
		rows, err := pdb.QueryContext(ctx, func() string {
			if hasComposite {
				return `
            SELECT dir_name, sensor, is_filled, v_pix, is_corrected, composite
            FROM image_dir_rules
            WHERE pass_type_id = ?`
			}
			return `
        SELECT dir_name, sensor, is_filled, v_pix, is_corrected
        FROM image_dir_rules
        WHERE pass_type_id = ?`
		}(), pr.id)
		if err != nil {
			return nil, fmt.Errorf("query image_dir_rules(%s): %w", pr.code, err)
		}
		for rows.Next() {
			var dir, sensor string
			var isFilled, vPix, isCorrected int
			var composite sql.NullString

			if hasComposite {
				if err := rows.Scan(&dir, &sensor, &isFilled, &vPix, &isCorrected, &composite); err != nil {
					_ = rows.Close()
					return nil, err
				}
			} else {
				if err := rows.Scan(&dir, &sensor, &isFilled, &vPix, &isCorrected); err != nil {
					_ = rows.Close()
					return nil, err
				}
			}

			pt.ImageDirs[dir] = config.ImageDirConfig{
				IsFilled:    isFilled != 0,
				VPix:        vPix,
				Sensor:      sensor,
				IsCorrected: isCorrected != 0,
				Composite:   strings.TrimSpace(composite.String), // empty when column missing
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()

		out.PassTypes[pr.code] = pt
	}

	// folder_includes
	{
		rows, err := pdb.QueryContext(ctx, `
			SELECT f.prefix, p.code
			FROM folder_includes f
			JOIN pass_types p ON p.id = f.pass_type_id`)
		if err != nil {
			return nil, fmt.Errorf("query folder_includes: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var prefix, code string
			if err := rows.Scan(&prefix, &code); err != nil {
				return nil, err
			}
			out.Passes.FolderIncludes[prefix] = code
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	// If nothing is configured, treat as an error
	if len(out.Composites) == 0 && len(out.PassTypes) == 0 && len(out.Passes.FolderIncludes) == 0 {
		return nil, errors.New("prefs db contains no pass config")
	}

	return out, nil
}

// utils

func isImageFile(name string) bool {
	matched, _ := regexp.MatchString(`(?i)\.(jpg|jpeg|png|gif|webp)$`, name)
	return matched
}

func getImageDimensions(imagePath string) *int {
	f, err := os.Open(imagePath)
	if err != nil {
		return nil
	}
	defer f.Close()
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return nil
	}
	return &cfg.Height
}

func boolToInt(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func extractTimestampFromFolder(folderName string) *int64 {
	folderName = filepath.Base(folderName)

	folderName = strings.ReplaceAll(folderName, "\\", "_")
	folderName = strings.ReplaceAll(folderName, "/", "_")

	re := regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})_(\d{2})-(\d{2})`)
	m := re.FindStringSubmatch(folderName)
	if len(m) != 6 {
		return nil
	}
	tstr := fmt.Sprintf("%s-%s-%sT%s:%s:00Z", m[1], m[2], m[3], m[4], m[5])
	t, err := time.Parse(time.RFC3339, tstr)
	if err != nil {
		return nil
	}
	ts := t.Unix()
	return &ts
}

func (c *updCtx) getAllExistingPasses() (map[string]existingPassData, error) {
	passes := make(map[string]existingPassData)
	rows, err := c.db.Query(`SELECT id, name, COALESCE(needsRescan, 1) FROM passes`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		var needsRescan uint8
		if err := rows.Scan(&id, &name, &needsRescan); err != nil {
			return nil, err
		}
		passes[name] = existingPassData{id: id, needsRescan: needsRescan}
	}
	return passes, rows.Err()
}

// DB helpers

func (c *updCtx) initializeDatabase() error {
	_, err := c.db.Exec(`
		CREATE TABLE IF NOT EXISTS passes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT UNIQUE,
			satellite TEXT,
			timestamp INTEGER,
			rawDataPath TEXT,
			downlink TEXT,
			needsRescan INTEGER DEFAULT 1
		);
		CREATE TABLE IF NOT EXISTS images (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			path TEXT,
			composite TEXT,
			sensor TEXT,
			mapOverlay INTEGER,
			corrected INTEGER,
			filled INTEGER,
			vPixels INTEGER,
			passId INTEGER,
			needsThumb INTEGER DEFAULT 1,
			FOREIGN KEY (passId) REFERENCES passes(id)
		);
	`)
	if err != nil {
		return err
	}
	// Backward-compat migrations
	if err := c.ensureColumnExists("passes", "needsRescan", "INTEGER DEFAULT 1"); err != nil {
		return err
	}
	if err := c.ensureColumnExists("images", "needsThumb", "INTEGER DEFAULT 1"); err != nil {
		return err
	}
	return nil
}

func (c *updCtx) ensureColumnExists(table, column, colDef string) error {
	rows, err := c.db.Query(`PRAGMA table_info(` + table + `);`)
	if err != nil {
		return err
	}
	defer rows.Close()

	has := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, column) {
			has = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !has {
		_, err := c.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + column + ` ` + colDef + `;`)
		return err
	}
	return nil
}

func (c *updCtx) clearTables() error {
	_, err := c.db.Exec("DELETE FROM images; DELETE FROM passes;")
	if err != nil {
		return err
	}

	_, err = c.db.Exec("DELETE FROM sqlite_sequence WHERE name IN ('images', 'passes');")
	return err
}

// Rescan helpers

func latestModTimeOfTree(root string) (time.Time, error) {
	var latest time.Time
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		mt := info.ModTime()
		if mt.After(latest) {
			latest = mt
		}
		return nil
	})
	return latest, err
}

func needsRescanFromMTime(latest time.Time, now time.Time) uint8 {
	if latest.IsZero() {
		return 1
	}
	if now.Sub(latest) > 30*time.Minute {
		return 0
	}
	return 1
}

// main logic

// Returns: images, parsed dataset, datasetAbsPath (for reading only), downlink, rawDataRelPath (from config)
func (c *updCtx) processPassType(passFolder string, passType config.PassTypeConfig) ([]Image, *Dataset, string, string, string, error) {
	// DATASET: used for reading satellite/timestamp only; not stored in DB
	var dataset Dataset
	datasetAbsPath := ""
	if strings.TrimSpace(passType.DatasetFile) != "" {
		datasetAbsPath = filepath.Join(c.liveOutputDir, passFolder, passType.DatasetFile)
		if data, err := os.ReadFile(datasetAbsPath); err == nil {
			_ = json.Unmarshal(data, &dataset)
		}
	}

	var images []Image

	// Precompute composite keys, longest-first
	compKeys := make([]string, 0, len(c.passCfg.Composites))
	for k := range c.passCfg.Composites {
		compKeys = append(compKeys, k)
	}
	sort.Slice(compKeys, func(i, j int) bool { return len(compKeys[i]) > len(compKeys[j]) })

	for subDir, overrides := range passType.ImageDirs {
		basePath := filepath.Join(c.liveOutputDir, passFolder)

		var scanPaths []string
		if strings.Contains(subDir, "*") {
			matches, err := filepath.Glob(filepath.Join(basePath, subDir))
			if err != nil || len(matches) == 0 {
				continue
			}
			scanPaths = matches
		} else {
			scanPaths = []string{filepath.Join(basePath, subDir)}
		}

		for _, scanPath := range scanPaths {
			entries, err := os.ReadDir(scanPath)
			if err != nil {
				continue
			}

			overrideComp := strings.TrimSpace(overrides.Composite)

			for _, e := range entries {
				if !e.IsDir() && isImageFile(e.Name()) {
					vPixels := overrides.VPix
					if vPixels == 0 {
						if v := getImageDimensions(filepath.Join(scanPath, e.Name())); v != nil {
							vPixels = *v
						}
					}

					corrected := overrides.IsCorrected
					if !corrected && strings.Contains(e.Name(), "_corrected") {
						corrected = true
					}

					// determine composite by key match
					rawComp := strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
					chosen := "Other"
					lcRaw := strings.ToLower(rawComp)
					for _, k := range compKeys {
						if strings.Contains(lcRaw, strings.ToLower(k)) {
							chosen = c.passCfg.Composites[k]
							break
						}
					}
					if overrideComp != "" {
						chosen = overrideComp
					}

					relPath, _ := filepath.Rel(filepath.Join(c.liveOutputDir, passFolder), filepath.Join(scanPath, e.Name()))
					fullRel := filepath.ToSlash(filepath.Clean(filepath.Join(passFolder, relPath)))

					images = append(images, Image{
						Path:       fullRel,
						Composite:  chosen,
						Sensor:     overrides.Sensor,
						Corrected:  boolToInt(corrected),
						Filled:     boolToInt(overrides.IsFilled),
						MapOverlay: boolToInt(strings.Contains(strings.ToLower(e.Name()), "map")),
						VPixels:    &vPixels,
					})
				}
			}
		}
	}
	return images, &dataset, datasetAbsPath, passType.Downlink, passType.RawDataFile, nil
}

func (c *updCtx) processPassOptimized(passFolder string, images []Image, dataset *Dataset, downlink, rawDataRelPath string, existingPassID int64, code string) error {
	satellite := "Unknown"
	var timestamp *int64

	if dataset != nil {
		if dataset.Satellite != "" {
			satellite = dataset.Satellite
		} else {
			satellite = code
		}
		if dataset.Timestamp > 0 {
			ts := int64(dataset.Timestamp)
			timestamp = &ts
		}
	}
	// If timestamp is nil, in the future, or less than 1 (incorrectly transmitted/decoded ts) get from folder name
	if timestamp == nil || *timestamp > 1735756467000 || *timestamp < 1 {
		timestamp = extractTimestampFromFolder(passFolder)
	}

	rd := "NOT_CONFIGURED"
	if rawDataRelPath != "" {
		rd = rawDataRelPath
	}
	dl := "NOT_CONFIGURED"
	if downlink != "" {
		dl = downlink
	}

	// Only calculate needsRescan if update is needed
	fullPath := filepath.Join(c.liveOutputDir, passFolder)
	lmt, _ := latestModTimeOfTree(fullPath)
	rescanFlag := needsRescanFromMTime(lmt, time.Now())

	var passID int64
	if existingPassID > 0 {
		// Update existing
		passID = existingPassID
		_, ierr := c.db.Exec(`
			UPDATE passes
			SET satellite = ?, timestamp = ?, rawDataPath = ?, downlink = ?, needsRescan = ?
			WHERE id = ?`,
			satellite, timestamp, rd, dl, rescanFlag, passID)
		if ierr != nil {
			return ierr
		}
	} else {
		// Insert new
		res, ierr := c.db.Exec(`
			INSERT INTO passes (name, satellite, timestamp, rawDataPath, downlink, needsRescan)
			VALUES (?, ?, ?, ?, ?, ?)`,
			passFolder, satellite, timestamp, rd, dl, rescanFlag)
		if ierr != nil {
			return ierr
		}
		if passID, ierr = res.LastInsertId(); ierr != nil {
			return ierr
		}
	}

	// Batch image inserts more efficiently
	if len(images) == 0 {
		return nil
	}

	// Only query existing images NOW (not earlier)
	existing := make(map[string]struct{})
	{
		rows, qerr := c.db.Query(`SELECT path FROM images WHERE passId = ?`, passID)
		if qerr == nil {
			defer rows.Close()
			for rows.Next() {
				var p string
				if err := rows.Scan(&p); err == nil {
					existing[p] = struct{}{}
				}
			}
		}
	}

	// Filter out images that already exist
	newImages := make([]Image, 0, len(images))
	for _, img := range images {
		if _, seen := existing[img.Path]; !seen {
			newImages = append(newImages, img)
		}
	}

	if len(newImages) == 0 {
		return nil
	}

	// Batch insert with transaction
	tx, txErr := c.db.Begin()
	if txErr != nil {
		return txErr
	}
	defer tx.Rollback()

	stmt, prepErr := tx.Prepare(`
		INSERT OR IGNORE INTO images
			(path, composite, sensor, mapOverlay, corrected, filled, vPixels, passId, needsThumb)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)
	`)
	if prepErr != nil {
		return prepErr
	}
	defer stmt.Close()

	for _, img := range newImages {
		if _, ierr := stmt.Exec(
			img.Path, img.Composite, img.Sensor, img.MapOverlay,
			img.Corrected, img.Filled, img.VPixels, passID,
		); ierr != nil {
			return ierr
		}
	}

	return tx.Commit()
}

// Only updates only metadata fields (composite, sensor, etc.) without deleting/re-adding images
func (c *updCtx) updateMetadata(existingPasses map[string]existingPassData) error {
	updated := 0
	errors := 0

	fmt.Println("Starting metadata-only update...")

	for passName, passData := range existingPasses {
		// Find the pass type for this pass
		var matchedTypeName string
		for pattern, typeName := range c.passCfg.Passes.FolderIncludes {
			p := strings.TrimSpace(pattern)
			if p == "" {
				continue
			}

			// Simple substring match (most common case)
			if !strings.ContainsAny(p, "*/") {
				if strings.Contains(strings.ToLower(passName), strings.ToLower(p)) {
					matchedTypeName = typeName
					break
				}
			} else {
				// For glob patterns, check if the pass name matches
				matched, _ := filepath.Match(p, passName)
				if matched {
					matchedTypeName = typeName
					break
				}
			}
		}

		if matchedTypeName == "" {
			continue
		}

		passType, exists := c.passCfg.PassTypes[matchedTypeName]
		if !exists {
			continue
		}

		// Get all images for this pass
		rows, err := c.db.Query(`SELECT id, path FROM images WHERE passId = ?`, passData.id)
		if err != nil {
			fmt.Printf("Error querying images for pass %s: %v\n", passName, err)
			errors++
			continue
		}

		type imageRecord struct {
			id   int64
			path string
		}
		var images []imageRecord

		for rows.Next() {
			var img imageRecord
			if err := rows.Scan(&img.id, &img.path); err != nil {
				continue
			}
			images = append(images, img)
		}
		rows.Close()

		if len(images) == 0 {
			continue
		}

		// Update each image's metadata based on the config
		for _, img := range images {
			// Determine which directory this image is from
			relPath := img.path
			parts := strings.Split(filepath.ToSlash(relPath), "/")
			if len(parts) < 2 {
				continue
			}

			// The directory name is typically the second-to-last component
			// e.g., "pass_folder/RGB/image.jpg" -> "RGB"
			dirName := parts[len(parts)-2]

			// Find matching image dir config
			dirConfig, exists := passType.ImageDirs[dirName]
			if !exists {
				// Try matching with case-insensitive comparison
				for configDir, cfg := range passType.ImageDirs {
					if strings.EqualFold(configDir, dirName) {
						dirConfig = cfg
						exists = true
						break
					}
				}
			}

			if !exists {
				continue
			}

			// Update the metadata fields
			_, err := c.db.Exec(`
				UPDATE images
				SET composite = ?, sensor = ?, corrected = ?, filled = ?
				WHERE id = ?`,
				dirConfig.Composite,
				dirConfig.Sensor,
				boolToInt(dirConfig.IsCorrected),
				boolToInt(dirConfig.IsFilled),
				img.id)

			if err != nil {
				fmt.Printf("Error updating image %d: %v\n", img.id, err)
				errors++
			} else {
				updated++
			}
		}
	}

	fmt.Printf("Metadata update complete. Updated %d images (%d errors)\n", updated, errors)
	return nil
}

func (c *updCtx) processPasses(mode int8) error {
	if c.cfg == nil {
		return fmt.Errorf("processPasses: AppConfig is nil")
	}
	if c.passCfg == nil {
		return fmt.Errorf("processPasses: PassConfig is nil")
	}
	if c.db == nil {
		return fmt.Errorf("processPasses: db is nil")
	}
	if strings.TrimSpace(c.liveOutputDir) == "" {
		return fmt.Errorf("processPasses: liveOutputDir is empty")
	}

	// Load all existing pass data once (keyed by passes.name)
	existingPasses, err := c.getAllExistingPasses()
	if err != nil {
		return fmt.Errorf("load existing passes: %w", err)
	}

	if mode == 2 {
		return c.updateMetadata(existingPasses)
	}

	// support two modes:
	//  1- Simple pattern (no '/' and no '*'): case-insensitive substring match on top-level folders
	//  2- Advanced pattern (has '/' or '*'): expand via Glob under live_output_dir
	type cand struct {
		relFolder string // relative to live_output_dir
		typeName  string
	}
	candidates := make(map[string]cand)

	// Collect top-level dirs for simple substring matching only once
	topEntries, _ := os.ReadDir(c.liveOutputDir)
	topLevelDirs := make([]string, 0, len(topEntries))
	for _, d := range topEntries {
		if d.IsDir() {
			topLevelDirs = append(topLevelDirs, d.Name())
		}
	}

	for pattern, typeName := range c.passCfg.Passes.FolderIncludes {
		p := strings.TrimSpace(pattern)
		if p == "" {
			continue
		}

		if strings.ContainsAny(p, "*/") {
			// expand glob rooted at live_output_dir
			absGlob := filepath.Join(c.liveOutputDir, p)
			matches, _ := filepath.Glob(absGlob)
			for _, m := range matches {
				fi, err := os.Stat(m)
				if err != nil || !fi.IsDir() {
					continue
				}
				rel, err := filepath.Rel(c.liveOutputDir, m)
				if err != nil || strings.HasPrefix(rel, "..") {
					continue
				}
				rel = filepath.ToSlash(rel)
				if _, exists := candidates[rel]; !exists {
					candidates[rel] = cand{relFolder: rel, typeName: typeName}
				}
			}
		} else {
			// case-insensitive substring match on top-level folders
			lp := strings.ToLower(p)
			for _, name := range topLevelDirs {
				if strings.Contains(strings.ToLower(name), lp) {
					rel := filepath.ToSlash(name)
					if _, exists := candidates[rel]; !exists {
						candidates[rel] = cand{relFolder: rel, typeName: typeName}
					}
				}
			}
		}
	}

	added := 0
	skipped := 0

	// Process each candidate pass folder once
	for _, cnd := range candidates {
		passRel := cnd.relFolder
		matchedTypeName := cnd.typeName
		if matchedTypeName == "" {
			continue
		}

		if existing, found := existingPasses[passRel]; found && existing.needsRescan == 0 {
			fmt.Println("Skipping possible pass: ", passRel)
			skipped++
			continue
		}

		passType := c.passCfg.PassTypes[matchedTypeName]
		images, dataset, _, downlink, rawDataRelPath, err := c.processPassType(passRel, passType)
		if err != nil {
			fmt.Printf("Error processing %s: %v\n", passRel, err)
			continue
		}

		// Reuse existing pass ID when possible
		passID := int64(0)
		if existing, found := existingPasses[passRel]; found {
			passID = existing.id
		}

		if err := c.processPassOptimized(passRel, images, dataset, downlink, rawDataRelPath, passID, matchedTypeName); err != nil {
			fmt.Printf("Error inserting pass %s: %v\n", passRel, err)
			continue
		}
		added++
	}

	if mode == 0 {
		fmt.Printf("Database population complete. Passes processed: %d\n", added)
	} else {
		fmt.Printf("Database updated. Processed %d passes (skipped %d)\n", added, skipped)
	}
	return nil
}

// entrypoint
func RunDBUpdate(cfg *config.AppConfig, passCfg *config.PassConfig, repopulate bool) error {
	if cfg == nil {
		return fmt.Errorf("RunDBUpdate: cfg is nil")
	}
	if strings.TrimSpace(cfg.Paths.DataDir) == "" {
		return fmt.Errorf("RunDBUpdate: database.path missing")
	}
	if strings.TrimSpace(cfg.Paths.LiveOutputDir) == "" {
		return fmt.Errorf("RunDBUpdate: paths.live_output_dir missing")
	}

	ctx := context.Background()
	prefsDBPath := filepath.Join(strings.TrimSpace(cfg.Paths.DataDir), "local_data.db")
	if loaded, err := loadPassConfigFromPrefs(ctx, prefsDBPath); err == nil {
		passCfg = loaded
		fmt.Println("PassConfig loaded")
	} else {
		fmt.Println("PassConfig could not be loaded: ", err)
	}
	if passCfg == nil {
		return fmt.Errorf("RunDBUpdate: no pass config available")
	}

	db, err := sql.Open("sqlite3", filepath.Join(cfg.Paths.DataDir, "image_metadata.db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	uctx := &updCtx{
		cfg:           cfg,
		passCfg:       passCfg,
		db:            db,
		liveOutputDir: cfg.Paths.LiveOutputDir,
	}

	if err := uctx.initializeDatabase(); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	if repopulate {
		if err := uctx.clearTables(); err != nil {
			return fmt.Errorf("clear tables: %w", err)
		}
		return uctx.processPasses(0)
	}
	return uctx.processPasses(1)
}

func RunDBMetadataUpdate(cfg *config.AppConfig, passCfg *config.PassConfig) error {
	if cfg == nil {
		return fmt.Errorf("RunDBMetadataUpdate: cfg is nil")
	}
	if strings.TrimSpace(cfg.Paths.DataDir) == "" {
		return fmt.Errorf("RunDBMetadataUpdate: database.path missing")
	}
	if strings.TrimSpace(cfg.Paths.LiveOutputDir) == "" {
		return fmt.Errorf("RunDBMetadataUpdate: paths.live_output_dir missing")
	}

	ctx := context.Background()
	prefsDBPath := filepath.Join(strings.TrimSpace(cfg.Paths.DataDir), "local_data.db")
	if loaded, err := loadPassConfigFromPrefs(ctx, prefsDBPath); err == nil {
		passCfg = loaded
		fmt.Println("PassConfig loaded")
	} else {
		fmt.Println("PassConfig could not be loaded: ", err)
	}
	if passCfg == nil {
		return fmt.Errorf("RunDBMetadataUpdate: no pass config available")
	}

	db, err := sql.Open("sqlite3", filepath.Join(cfg.Paths.DataDir, "image_metadata.db"))
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	uctx := &updCtx{
		cfg:           cfg,
		passCfg:       passCfg,
		db:            db,
		liveOutputDir: cfg.Paths.LiveOutputDir,
	}

	if err := uctx.initializeDatabase(); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}

	return uctx.processPasses(2)
}
