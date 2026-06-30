package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// handleEvents streams job updates to the client as Server-Sent Events. The
// front-end opens one EventSource and receives a JSON-encoded job snapshot on
// every status change or progress tick, driving the live progress bar without
// polling. A periodic comment line keeps proxies and the browser from closing
// an idle connection.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	updates, unsubscribe := s.svc.Jobs.Subscribe()
	defer unsubscribe()

	// Greet the client so it knows the stream is live.
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case job, open := <-updates:
			if !open {
				return
			}
			payload, err := json.Marshal(job)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: job\ndata: %s\n\n", payload)
			flusher.Flush()
		}
	}
}
