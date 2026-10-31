//go:build !darwin && !windows

package collect

import (
	"context"
	"fmt"

	"github.com/ricardotech/minipulse/internal/model"
)

// SystemServices has no implementation on this platform yet; an
// explicit error beats an empty list that looks like data.
func SystemServices(ctx context.Context) ([]model.Service, error) {
	return nil, fmt.Errorf("service listing is not supported on this platform yet")
}

// Listeners has no implementation on this platform yet.
func Listeners(ctx context.Context) ([]model.Listener, error) {
	return nil, fmt.Errorf("listener listing is not supported on this platform yet")
}
