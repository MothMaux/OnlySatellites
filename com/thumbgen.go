package com

import (
	"OnlySats/config"
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/h2non/bimg"
)

var processedImages int64
var skippedImages int64
var failedImages int64

func RunThumbGen(db *sql.DB) error {
	// reset counters for each run
	atomic.StoreInt64(&processedImages, 0)
	atomic.StoreInt64(&skippedImages, 0)
	atomic.StoreInt64(&failedImages, 0)

	baseOutputDir := config.GetString("paths.live_output")
	thumbOutputDir := config.GetString("paths.thumbnails")

	workers := config.GetInt("thumbgen.max_workers")
	if workers <= 0 {
		workers = 2
	}
	jobBuffer := config.GetInt("thumbgen.batch_size")
	if jobBuffer <= 0 {
		jobBuffer = 500
	}
	width := config.GetInt("thumbgen.thumbnail_width")
	if width <= 0 {
		width = 200
	}
	quality := min(max(config.GetInt("thumbgen.quality"), 10), 100)

	logLevel := config.GetString("server.logging_level")
	logFile := filepath.Join(config.GetString("paths.logs") + "thumbgen.log")

	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		return fmt.Errorf("failed to create log dir: %w", err)
	}
	lf, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer lf.Close()
	bufWriter := bufio.NewWriterSize(lf, 1<<20)
	logger := log.New(bufWriter, "", log.LstdFlags)

	start := time.Now()

	// info only
	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM images WHERE needsThumb = 1").Scan(&total); err != nil {
		return fmt.Errorf("failed to count images: %w", err)
	}
	logger.Printf("Found %d images to process (workers=%d, width=%d, quality=%d, out=%s)",
		total, workers, width, quality, thumbOutputDir)

	// worker pool + successes collector
	type imageJob struct {
		id   int64
		path string
	}

	jobs := make(chan imageJob, jobBuffer)
	successes := make(chan int64, jobBuffer) // IDs to mark needsThumb=0
	var wg sync.WaitGroup

	// Workers
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				made, err := processImage(job.path, baseOutputDir, thumbOutputDir, width, quality)
				if err != nil {
					atomic.AddInt64(&failedImages, 1)
					if logLevel == "detailed" {
						logger.Printf("[FAIL] %s: %v", job.path, err)
					}
					continue
				}
				if made {
					atomic.AddInt64(&processedImages, 1)
					if logLevel == "detailed" {
						logger.Printf("[OK] %s (created)", job.path)
					}
				} else {
					atomic.AddInt64(&skippedImages, 1)
					if logLevel == "detailed" {
						logger.Printf("[SKIP] %s (exists)", job.path)
					}
				}
				// success: mark as completed later in one batch
				successes <- job.id
			}
		}()
	}

	// Collector goroutine drains successes while workers run (prevents deadlock)
	doneIDs := make([]int64, 0, jobBuffer)
	var collectWg sync.WaitGroup
	collectWg.Add(1)
	go func() {
		defer collectWg.Done()
		for id := range successes {
			doneIDs = append(doneIDs, id)
		}
	}()

	// queue jobs from DB
	rows, err := db.Query("SELECT id, path FROM images WHERE needsThumb = 1")
	if err != nil {
		return fmt.Errorf("failed to query images: %w", err)
	}
	sent := 0
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err == nil {
			jobs <- imageJob{id: id, path: p}
			sent++
			if logLevel != "detailed" && sent%5000 == 0 {
				logger.Printf("Queued %d images...", sent)
			}
		}
	}
	_ = rows.Close()
	close(jobs)      // stop workers when queue drains
	wg.Wait()        // wait for all workers to finish
	close(successes) // signal collector to finish
	collectWg.Wait()

	// batch UPDATE needsThumb=0 for all successes
	if len(doneIDs) > 0 {
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin update txn: %w", err)
		}
		stmt, err := tx.Prepare("UPDATE images SET needsThumb = 0 WHERE id = ?")
		if err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("prepare update: %w", err)
		}
		for _, id := range doneIDs {
			if _, err := stmt.Exec(id); err != nil {
				_ = stmt.Close()
				_ = tx.Rollback()
				return fmt.Errorf("update needsThumb=0 id=%d: %w", id, err)
			}
		}
		_ = stmt.Close()
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit update: %w", err)
		}
		logger.Printf("Marked needsThumb=0 for %d images", len(doneIDs))
	}

	// flush file logs before printing summary
	_ = bufWriter.Flush()

	elapsed := time.Since(start).Truncate(time.Millisecond)
	fmt.Printf("Thumbnail generation completed in %s: %d processed, %d skipped, %d failed\n",
		elapsed, processedImages, skippedImages, failedImages)
	logger.Printf("Completed in %s: %d processed, %d skipped, %d failed",
		elapsed, processedImages, skippedImages, failedImages)

	return nil
}

// webp helper
func toWebP(rel string) string {
	rel = strings.ReplaceAll(rel, "\\", "/")
	ext := strings.ToLower(filepath.Ext(rel))
	if ext == "" {
		return rel + ".webp"
	}
	return strings.TrimSuffix(rel, ext) + ".webp"
}

func processImage(relPath, baseOutputDir, thumbOutputDir string, width, quality int) (bool, error) {
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	relPath = filepath.Clean(relPath)

	src := filepath.Join(baseOutputDir, relPath)

	var dst string
	if strings.TrimSpace(thumbOutputDir) == "" {
		// side-by-side: <live>/<dir>/thumbnails/<name>.webp
		srcDir := filepath.Dir(src)
		dst = filepath.Join(srcDir, "thumbnails", filepath.Base(toWebP(relPath)))
	} else {
		// central mirror: <thumbRoot>/<rel>.webp
		dst = filepath.Join(thumbOutputDir, toWebP(relPath))
	}

	// If thumbnail already exists, treat as success
	if _, err := os.Stat(dst); err == nil {
		return false, nil // not made, but OK
	}

	// does source exist
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return false, fmt.Errorf("source image does not exist: %s", src)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, fmt.Errorf("failed to create thumb directory: %w", err)
	}

	data, err := bimg.Read(src)
	if err != nil {
		return false, fmt.Errorf("failed to read image %s: %w", src, err)
	}

	size, err := bimg.NewImage(data).Size()
	if err != nil {
		return false, fmt.Errorf("failed to get size for %s: %w", src, err)
	}

	newH := int((float64(width) * float64(size.Height)) / float64(size.Width))
	if newH <= 0 {
		newH = 1
	}

	out, err := bimg.NewImage(data).Process(bimg.Options{
		Width:   width,
		Height:  newH,
		Force:   true,
		Quality: quality,
		Type:    bimg.WEBP,
	})
	if err != nil {
		return false, fmt.Errorf("processing failed for %s: %w", src, err)
	}

	if err := bimg.Write(dst, out); err != nil {
		return false, fmt.Errorf("failed to write thumbnail %s: %w", dst, err)
	}
	return true, nil // made a new thumbnail
}
