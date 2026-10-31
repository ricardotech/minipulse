package collect

import (
	"context"

	"github.com/ricardotech/minipulse/internal/model"
)

// SystemServices lists Windows services. Implemented in the Windows
// collectors slice; stub for now so the platform compiles.
func SystemServices(ctx context.Context) ([]model.Service, error) {
	return nil, nil
}

// Listeners lists TCP ports in LISTEN state. Stub; see SystemServices.
func Listeners(ctx context.Context) ([]model.Listener, error) {
	return nil, nil
}
