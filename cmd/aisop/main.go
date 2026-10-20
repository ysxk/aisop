package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ysxk/aisop/internal/app"
	"github.com/ysxk/aisop/internal/auth"
	"github.com/ysxk/aisop/internal/config"
	"github.com/ysxk/aisop/internal/database"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error(err.Error())
		os.Exit(1)
	}
}
func run(logger *slog.Logger) error {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if len(os.Args) > 2 || (command != "serve" && command != "migrate" && command != "bootstrap-admin") {
		return fmt.Errorf("usage: aisop [serve|migrate|bootstrap-admin]")
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	pool, err := database.Open(connectCtx, c.DatabaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer pool.Close()
	switch command {
	case "migrate":
		if err = database.Migrate(ctx, pool); err != nil {
			return fmt.Errorf("database migration failed; verify schema and database permissions")
		}
		logger.Info("database migrations complete")
		return nil
	case "bootstrap-admin":
		err = auth.New(pool, c.SessionTTL).Bootstrap(ctx, os.Getenv("ADMIN_USERNAME"), os.Getenv("ADMIN_PASSWORD"))
		if err != nil {
			return fmt.Errorf("admin initialization failed: check credentials, migrations, or existing administrator")
		}
		logger.Info("administrator initialized")
		return nil
	}
	a, err := app.New(c, pool, logger)
	if err != nil {
		return fmt.Errorf("media directory initialization failed")
	}
	defer a.Close()
	cleanupCtx, stopCleanup := context.WithCancel(ctx)
	cleanupDone := make(chan struct{})
	go func() { defer close(cleanupDone); a.RunCleanup(cleanupCtx, logger) }()
	defer func() { stopCleanup(); <-cleanupDone }()
	server := &http.Server{Addr: c.Addr, Handler: a.Handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Minute, WriteTimeout: 15 * time.Minute, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	failures := make(chan error, 1)
	go func() { logger.Info("HTTP server starting", "addr", c.Addr); failures <- server.ListenAndServe() }()
	select {
	case err := <-failures:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("HTTP server failed to start or stopped unexpectedly")
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return fmt.Errorf("HTTP graceful shutdown timed out")
		}
	}
	return nil
}
