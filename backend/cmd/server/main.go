package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"backend/internal/api"
	"backend/internal/db"
	"backend/internal/envfile"
	"backend/internal/storage"
)

func main() {
	if err := run(); err != nil {
		log.Printf("server: ERROR: %v", err)
		os.Exit(1)
	}
}

func run() error {
	// Go has no built-in .env support; without this, "cp .env.example .env"
	// has no effect and every variable below reads as unset.
	if err := envfile.Load(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL is not set")
	}

	openContext, cancelOpen := context.WithTimeout(context.Background(), 30*time.Second)
	database, err := db.Open(openContext, databaseURL)
	cancelOpen()
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer database.Close()

	basePath := strings.TrimSpace(os.Getenv("AUDIO_STORAGE_PATH"))
	if basePath == "" {
		basePath = "data/audio"
	}
	server := &http.Server{
		Addr:    ":" + port(),
		Handler: api.New(database, storage.New(basePath)).Handler(),
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", server.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	signalContext, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-signalContext.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		return nil
	}
}

func port() string {
	value := strings.TrimSpace(os.Getenv("PORT"))
	if value == "" {
		return "8080"
	}
	return value
}
