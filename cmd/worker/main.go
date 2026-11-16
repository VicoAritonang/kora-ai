// Command worker runs background jobs (River). Subcommands:
//
//	worker                      run the job loop
//	worker import-norms FILE…   load curated norm files into cost_norms
//	worker refresh-index        refresh the Destination Cost Index once
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/koraai/koraai/internal/app"
	"github.com/koraai/koraai/internal/config"
	"github.com/koraai/koraai/internal/platform"
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
	zero := time.Duration(0) // the index refresh needs no simulated supplier latency
	a, err := app.Build(ctx, cfg, log, app.Options{SupplierLatency: &zero})
	if err != nil {
		log.Error("startup failed", "err", err)
		os.Exit(1)
	}
	defer a.Close()

	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "import-norms":
			for _, f := range os.Args[2:] {
				n, err := a.Norms.ImportFile(ctx, f)
				if err != nil {
					log.Error("import norms", "file", f, "err", err)
					os.Exit(1)
				}
				log.Info("norms imported", "file", f, "rows", n)
			}
			return
		case "refresh-index":
			n, err := a.Index.Refresh(ctx, cfg.App.Origins, 12)
			if err != nil {
				log.Error("refresh index", "err", err)
				os.Exit(1)
			}
			log.Info("index refreshed", "rows", n)
			return
		}
	}

	rc, err := a.WorkerRiver()
	if err != nil {
		log.Error("river", "err", err)
		os.Exit(1)
	}
	if err := rc.Start(ctx); err != nil {
		log.Error("river start", "err", err)
		os.Exit(1)
	}
	log.Info("worker started")
	<-ctx.Done()
	shut, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = rc.Stop(shut)
}
