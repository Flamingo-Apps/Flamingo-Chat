package main

import (
	"log"
	"net/http"
	"time"

	"github.com/Flamingo-Apps/Flamingo-Chat/pkg/config"
)

func main() {
	port := config.String("HTTP_PORT", "8080")

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// TODO: WebSocket upgrade endpoint, JWT validation, and gRPC routing to
	// identity/matching/chat/presence/moderation.

	// A bare http.ListenAndServe has no read timeout, so a client that sends
	// headers slowly (by accident or on purpose) can tie up a connection
	// indefinitely. ReadHeaderTimeout only bounds the time to read the
	// request headers, so it won't cut off a long-lived WebSocket connection
	// once it's upgraded - that path gets its own timeout handling when it's
	// built.
	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	log.Printf("gateway listening on :%s", port)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
