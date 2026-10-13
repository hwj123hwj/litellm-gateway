package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"time"
)

func serveGateway(ctx context.Context, server *http.Server, serve func() error, timeout time.Duration) error {
	served := make(chan error, 1)
	go func() { served <- serve() }()
	select {
	case err := <-served:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		// listener 关闭会让 Serve 立即返回，主生命周期必须等 Shutdown 排空完成。
		shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		err := server.Shutdown(shutdownCtx)
		if err != nil {
			_ = server.Close()
		}
		<-served
		return err
	}
}

func boundedCleanup(ctx context.Context, cleanup func() error) error {
	done := make(chan error, 1)
	go func() { done <- cleanup() }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

type metricsCloser interface{ Close(context.Context) error }
type archiveCloser interface{ Close() }
type storeCloser interface{ Close() error }

func closeGatewayPersistence(ctx context.Context, collector metricsCloser, archiver archiveCloser, store storeCloser, logger *log.Logger) {
	if err := collector.Close(ctx); err != nil {
		logger.Printf("Metrics shutdown incomplete: %v", err)
	}
	if err := boundedCleanup(ctx, func() error { archiver.Close(); return nil }); err != nil {
		logger.Printf("Archive shutdown incomplete: %v", err)
	}
	if store != nil {
		if err := boundedCleanup(ctx, store.Close); err != nil {
			logger.Printf("SQLite shutdown incomplete: %v", err)
		}
	}
}
