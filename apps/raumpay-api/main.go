package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if os.Getenv("RAUMPAY_MODE") != "sandbox" {
		slog.Error("set RAUMPAY_MODE=sandbox; this local prototype cannot run in production")
		os.Exit(1)
	}
	api, err := newAPI(os.Getenv("RAUMPAY_MERCHANT_KEY"), os.Getenv("RAUMPAY_SIMULATOR_KEY"))
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
	server := &http.Server{
		Addr: "127.0.0.1:8080", Handler: api.handler(),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second,
		MaxHeaderBytes: 8192,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdown); err != nil {
			slog.Error("shutdown failed")
		}
	}()
	slog.Warn("local sandbox: simulated payments only; all state and idempotency records are lost on restart", "address", server.Addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
