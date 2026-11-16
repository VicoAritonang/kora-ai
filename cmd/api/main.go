// Command api serves the HTTP API and executes runs.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/koraai/koraai/internal/app"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/jobs"
	"github.com/koraai/koraai/internal/platform"
	httpapi "github.com/koraai/koraai/internal/transport/http"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		platform.NewLogger("info").Error("config", "err", err)
		os.Exit(1)
	}
	log := platform.NewLogger(cfg.App.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.Build(ctx, cfg, log, app.Options{})
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer a.Close()
	rc, err := a.InsertOnlyRiver()
	if err != nil {
		log.Error("river client", "err", err)
		os.Exit(1)
	}
	a.BuildPipeline(&jobs.Queue{Client: rc, Log: log})
	go a.Runner.ListenCancels(ctx)

	srv := &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.App.Port),
		Handler:           httpapi.Router(a.API, log),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: SSE streams stay open for a run's duration.
	}
	go func() {
		log.Info("api listening", "addr", srv.Addr, "env", cfg.App.Env, "flights", cfg.Suppliers.FlightProvider, "stays", cfg.Suppliers.StayProvider)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server", "err", err)
			stop()
		}
	}()
	<-ctx.Done()
	log.Info("shutting down")
	shut, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(shut)
	a.Runner.Shutdown(shut)
}
