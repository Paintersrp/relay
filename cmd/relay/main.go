package main

import (
	"log/slog"
	"net/http"
	"os"

	"relay/internal/server"
	"relay/internal/store"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dbPath := "data/relay.sqlite"
	if p := os.Getenv("RELAY_DB_PATH"); p != "" {
		dbPath = p
	}

	s, err := store.Open(dbPath, log)
	if err != nil {
		log.Error("open store", "error", err)
		os.Exit(1)
	}
	defer s.Close()

	srv := server.New(s, log)

	port := "8080"
	if p := os.Getenv("PORT"); p != "" {
		port = p
	}

	log.Info("Relay server starting", "port", port)
	if err := http.ListenAndServe(":"+port, srv.Handler()); err != nil {
		log.Error("server error", "error", err)
		os.Exit(1)
	}
}
