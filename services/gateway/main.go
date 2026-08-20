package main

import (
	"log"
	"net/http"

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

	log.Printf("gateway listening on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}
