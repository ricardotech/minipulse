//go:build !windows

package service

import (
	"context"
	"os/signal"
	"syscall"
)

// RunServe runs serveFn until SIGINT/SIGTERM. On Windows the same
// entry point also handles being launched by the Service Control
// Manager (see service_windows.go).
func RunServe(serveFn func(ctx context.Context) error) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	return serveFn(ctx)
}
