package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	dataDir := envOr("CLASH_ACL_DATA_DIR", "data")
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		log.Fatal("cannot create data directory")
	}
	configPath := filepath.Join(dataDir, "config.json")
	if _, err := readConfig(configPath); errors.Is(err, os.ErrNotExist) {
		if err := writeConfig(configPath, defaultConfig()); err != nil {
			log.Fatal("cannot initialize configuration")
		}
	} else if err != nil {
		log.Fatal("cannot read configuration: check config.json format and permissions")
	}
	server := &http.Server{
		Addr:              envOr("CLASH_ACL_LISTEN_ADDR", "127.0.0.1:8080"),
		Handler:           newHandler(configPath),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      5 * time.Minute,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Print("HTTP shutdown did not complete")
		}
	}()
	log.Printf("clash_ACL listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal("HTTP server failed")
	}
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
