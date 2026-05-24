package com

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func buildSatdumpEndpoint(addr string, port int) string {
	addr = strings.TrimSpace(addr)
	if port <= 0 {
		return fmt.Sprintf("http://%s/api", addr)
	}
	return fmt.Sprintf("http://%s:%d/api", addr, port)
}

func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

func httpGetJSON(ctx context.Context, url string) (any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var v any
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

func satdumpPoller(ctx context.Context, out chan<- satdumpLogEntry, instance, endpoint string, every time.Duration) {
	log.Printf("[satdump] %s polling %s every %v\n", instance, endpoint, every)
	baseEvery := every
	slowEvery := every * 10
	t := time.NewTicker(baseEvery)
	defer t.Stop()
	var (
		inError   bool
		downSince time.Time
	)

	for {
		select {
		case <-ctx.Done():
			return

		case <-t.C:
			err := fetchAndEnqueueSatdump(ctx, out, instance, endpoint)
			if err != nil {
				if !inError {
					inError = true
					downSince = time.Now()

					log.Printf("[satdump] %s: %s went offline at %s, slowing polling from %v to %v\n",
						instance,
						endpoint,
						downSince.Format(time.RFC3339),
						baseEvery,
						slowEvery,
					)
					t.Stop()
					t = time.NewTicker(slowEvery)
				}
				continue
			}
			if inError {
				recoveredAt := time.Now()
				log.Printf("[satdump] %s: %s back online; was down from %s to %s (duration %s)\n",
					instance,
					endpoint,
					downSince.Format(time.RFC3339),
					recoveredAt.Format(time.RFC3339),
					recoveredAt.Sub(downSince),
				)
				inError = false
				t.Stop()
				t = time.NewTicker(baseEvery)
			}
		}
	}
}

func startSatdumpLogger(ctx context.Context, db *sql.DB, flushEvery time.Duration, maxBatch int) chan<- satdumpLogEntry {
	ch := make(chan satdumpLogEntry, maxBatch*4)

	go func() {
		defer func() {
			for {
				select {
				case <-ch:
				default:
					return
				}
			}
		}()

		ticker := time.NewTicker(flushEvery)
		defer ticker.Stop()

		buf := make([]satdumpLogEntry, 0, maxBatch)

		flush := func() {
			if len(buf) == 0 {
				return
			}
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				log.Printf("[satdump logger] begin tx: %v", err)
				buf = buf[:0]
				return
			}
			stmt, err := tx.PrepareContext(ctx, `INSERT INTO satdump_readings (ts, instance, data) VALUES (?, ?, ?)`)
			if err != nil {
				log.Printf("[satdump logger] prepare: %v", err)
				_ = tx.Rollback()
				buf = buf[:0]
				return
			}
			for _, e := range buf {
				if _, err := stmt.ExecContext(ctx, e.ts, e.instance, string(e.data)); err != nil {
					log.Printf("[satdump logger] exec: %v", err)
				}
			}
			_ = stmt.Close()
			if err := tx.Commit(); err != nil {
				log.Printf("[satdump logger] commit: %v", err)
			}
			buf = buf[:0]
		}

		for {
			select {
			case <-ctx.Done():
				flush()
				return
			case e := <-ch:
				buf = append(buf, e)
				if len(buf) >= maxBatch {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()
	return ch
}

/**
func RunScheduledTasks(appCfg *config.AppConfig) {
	db := must(shared.OpenAnalDB(appCfg.Paths.DataDir))
	defer db.Close()
	_ = shared.InitSchema(db)

	lds := must(OpenLocalData(appCfg))
	defer lds.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logCh := startSatdumpLogger(ctx, db, 30*time.Second, 32)

	every := time.Second

	rows, err := lds.ListSatdumpLoggingEnabled(ctx)
	if err != nil {
		log.Printf("[scheduler] failed to list satdump instances: %v", err)
		return
	}
	for _, s := range rows {
		if s.Address == "" {
			s.Address = shared.GetHostIPv4()
		}
		endpoint := buildSatdumpEndpoint(s.Address, s.Port)
		go satdumpPoller(ctx, logCh, s.Name, endpoint, every)
	}
	<-ctx.Done()
} */
