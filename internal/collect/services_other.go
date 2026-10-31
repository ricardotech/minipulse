//go:build !darwin && !windows

package collect

import (
	"context"

	"github.com/ricardotech/minipulse/internal/model"
)

// SystemServices has no implementation on this platform yet.
func SystemServices(ctx context.Context) ([]model.Service, error) {
	return nil, nil
}

// Listeners has no implementation on this platform yet.
func Listeners(ctx context.Context) ([]model.Listener, error) {
	return nil, nil
}
