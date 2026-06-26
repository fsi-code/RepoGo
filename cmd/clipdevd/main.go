package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"

	"clipdev/internal/broker"
	"clipdev/internal/config"
	"clipdev/internal/dispatch"
	"clipdev/internal/hitl"
	"clipdev/internal/server"
	"clipdev/internal/watcher"
)

func main() {
	configPath := flag.String("config", "config.toml", "path to config.toml")
	workdir := flag.String("workdir", "", "override workdir (default: config value)")
	port := flag.Int("port", 0, "override HTTP port (default: config value)")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if *workdir != "" {
		cfg.Workdir = *workdir
	}
	if *port > 0 {
		cfg.Port = *port
	}

	log.Printf("clipdevd starting")
	log.Printf("  workdir  : %s", cfg.Workdir)
	log.Printf("  port     : %d", cfg.Port)
	log.Printf("  hitl     : write=%v patch=%v (timeout=%s)",
		containsStr(cfg.HITL.RequireApproval, "write"),
		containsStr(cfg.HITL.RequireApproval, "patch"),
		cfg.HITL.Timeout.Duration,
	)
	logClipboardDiag() // définie dans diag_unix.go / diag_windows.go

	b := broker.New()
	h := hitl.New(cfg)
	disp := dispatch.New(cfg, h, b)
	srv := server.New(cfg, b, h, disp)
	w := watcher.New(disp, b)

	sigs := append([]os.Signal{os.Interrupt}, extraSignals...)
	ctx, cancel := signal.NotifyContext(context.Background(), sigs...)
	defer cancel()

	go srv.Start()
	go w.Start(ctx)

	<-ctx.Done()
	log.Println("shutting down cleanly")
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
