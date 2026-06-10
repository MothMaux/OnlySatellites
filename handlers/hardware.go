// handlers/hardware.go
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"runtime"
	"time"

	com "OnlySats/com"
	"OnlySats/com/metrics"
	"OnlySats/config"

	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/process"
)

type HardwareHandler struct {
	Store   *sql.DB
	Timeout time.Duration
}

// report system/app uptime and this process' resource usage.
type InfoHandler struct {
	AppStart time.Time

	// cached
	proc *process.Process
}

// construct and primes the process handle.
func NewInfoHandler(appStart int) *InfoHandler {
	h := &InfoHandler{AppStart: time.Unix(int64(appStart), 0)}
	_ = h.initProc()
	return h
}

func (h *InfoHandler) initProc() error {
	if h.proc != nil {
		return nil
	}
	p, err := process.NewProcess(int32(os.Getpid()))
	if err == nil {
		h.proc = p
	}
	return err
}

type infoResponse struct {
	SystemUptimeSec uint64        `json:"system_uptime_sec"`
	AppUptimeSec    float64       `json:"app_uptime_sec"`
	AppCPUPercent   float64       `json:"app_cpu_percent"`
	AppMem          appMemPayload `json:"app_mem"`
}

type appMemPayload struct {
	RSSBytes        uint64  `json:"rss_bytes"`
	MemoryPercent   float32 `json:"memory_percent"`
	GoHeapAlloc     uint64  `json:"go_heap_alloc_bytes"`
	GoHeapSys       uint64  `json:"go_heap_sys_bytes"`
	GoStackInUse    uint64  `json:"go_stack_inuse_bytes"`
	GoGoroutines    int     `json:"go_goroutines"`
	GoLastGCUnixSec uint64  `json:"go_last_gc_unix_sec"`
}

func (h *InfoHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	up, _ := host.Uptime()
	appUptime := time.Since(h.AppStart).Seconds()

	_ = h.initProc()

	// Process CPU & memory
	var cpuPct float64
	var rss uint64
	var memPct float32

	if h.proc != nil {
		if v, err := h.proc.CPUPercentWithContext(r.Context()); err == nil {
			cpuPct = v
		}
		if mi, err := h.proc.MemoryInfoWithContext(r.Context()); err == nil && mi != nil {
			rss = mi.RSS
		}
		if mp, err := h.proc.MemoryPercentWithContext(r.Context()); err == nil {
			memPct = mp
		}
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	resp := infoResponse{
		SystemUptimeSec: up,
		AppUptimeSec:    appUptime,
		AppCPUPercent:   cpuPct,
		AppMem: appMemPayload{
			RSSBytes:        rss,
			MemoryPercent:   memPct,
			GoHeapAlloc:     ms.HeapAlloc,
			GoHeapSys:       ms.HeapSys,
			GoStackInUse:    ms.StackInuse,
			GoGoroutines:    runtime.NumGoroutine(),
			GoLastGCUnixSec: uint64(ms.LastGC / 1e9),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *HardwareHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	mode := "native"
	if h.Store != nil {
		if v, err := com.GetSetting(h.Store, r.Context(), "hwmonitor"); err == nil && v != "" {
			mode = v
		}
	}

	switch mode {
	case "off":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hardware monitoring is disabled on this station"))
		return

	case "hwinfo":
		// Simple forwarder to local HWiNFO HTTP server
		client := &http.Client{Timeout: 3 * time.Second}
		req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, "http://localhost:55555/", nil)
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "failed to query HWiNFO server: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		for k, vv := range resp.Header {
			// Pass through content-type at least; avoid hop-by-hop headers
			if k == "Content-Type" {
				for _, v := range vv {
					w.Header().Add(k, v)
				}
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		return

	default:
		to := h.Timeout
		if to <= 0 {
			to = 2 * time.Second
		}
		ctx, cancel := context.WithTimeout(r.Context(), to)
		defer cancel()

		snap, err := metrics.CollectNative(ctx, config.GetString("paths.live_output"))
		if err != nil {
			http.Error(w, "failed to collect hardware metrics: "+err.Error(), http.StatusInternalServerError)
			return
		}
		arr, err := metrics.ToHWInfoFormat(ctx, snap)
		if err != nil {
			http.Error(w, "failed to format metrics: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(arr)

	}
}
