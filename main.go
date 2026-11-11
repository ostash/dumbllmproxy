// SPDX-FileCopyrightText: 2026 Viktor Ostashevskyi <ostash@ostash.kiev.ua>
// SPDX-License-Identifier: GPL-3.0-only

package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"dumbllmproxy/internal/config"
	"dumbllmproxy/internal/cost"
	"dumbllmproxy/internal/pricing"
	"dumbllmproxy/internal/proxy"
	"dumbllmproxy/internal/server"
	"dumbllmproxy/internal/store"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to YAML configuration file")
	flag.Parse()

	logger := log.New(os.Stderr, "", log.LstdFlags)
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatalf("load config: %v", err)
	}

	if dir := filepath.Dir(cfg.Proxy.Database); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			logger.Fatalf("create database directory: %v", err)
		}
	}
	st, err := store.Open(cfg.Proxy.Database)
	if err != nil {
		logger.Fatalf("open database: %v", err)
	}
	defer st.Close()

	registry := pricing.NewRegistry()
	if cfg.Pricing.DeepSeek != nil {
		ds, err := pricing.NewDeepSeek(*cfg.Pricing.DeepSeek)
		if err != nil {
			logger.Fatalf("load pricing config: %v", err)
		}
		if err := registry.Register("deepseek", ds); err != nil {
			logger.Fatalf("register pricing: %v", err)
		}
		for _, warning := range ds.Diagnostics() {
			logger.Printf("pricing warning: %s", warning)
		}
	}
	// Calculated costs are resolved on every read, so a changed pricing
	// configuration applies to previously captured requests after restart.
	st.SetCostResolver(cost.NewResolver(registry).Resolve)

	proxyHandler := proxy.New(cfg, st, logger)
	srv, err := server.New(cfg, st, proxyHandler)
	if err != nil {
		logger.Fatalf("create server: %v", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Proxy.Listen,
		Handler:           srv,
		ReadHeaderTimeout: 30 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	logger.Printf("dumbllmproxy listening on http://%s (database: %s)", cfg.Proxy.Listen, cfg.Proxy.Database)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Fatalf("serve: %v", err)
	}
}
