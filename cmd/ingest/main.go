package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/ennaton/nine-ingest/internal/auth"
	"github.com/ennaton/nine-ingest/internal/httpapi"
	"github.com/ennaton/nine-ingest/internal/kafka"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	addr := env("NINE_INGEST_ADDR", ":18082")
	brokers := strings.Split(env("NINE_KAFKA_BROKERS", "localhost:19092"), ",")

	producer, err := kafka.Dial(brokers)
	if err != nil {
		log.Error("cannot reach kafka", "brokers", brokers, "err", err)
		os.Exit(1)
	}
	defer producer.Close()

	keys := auth.FromEnv()
	srv := &http.Server{
		Addr:              addr,
		Handler:           httpapi.New(keys, producer, log).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("ingest listening", "addr", addr, "brokers", brokers)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server stopped", "err", err)
			os.Exit(1)
		}
	}()

	// Drain in flight requests before exiting: an event that got a 202 must
	// already be on the log, and one still being written must finish.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Error("shutdown", "err", err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
