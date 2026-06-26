//go:build debug

package metrics

import (
	"log"
	"net/http"
	_ "net/http/pprof" // registers handlers on http.DefaultServeMux
)

const pprofAddr = "localhost:6060"

func StartDebugServer() {
	log.Printf("pprof debug server starting at http://%s/debug/pprof/", pprofAddr)
	go func() {
		if err := http.ListenAndServe(pprofAddr, nil); err != nil {
			log.Printf("pprof server error: %v", err)
		}
	}()
}
