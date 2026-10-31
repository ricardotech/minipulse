//go:build !windows

package collect

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// dockerSockets are the known socket locations on a Mac, in order:
// Docker Desktop / system, OrbStack, Colima.
func dockerSockets() []string {
	home, _ := os.UserHomeDir()
	return []string{
		"/var/run/docker.sock",
		filepath.Join(home, ".orbstack/run/docker.sock"),
		filepath.Join(home, ".colima/default/docker.sock"),
		filepath.Join(home, ".docker/run/docker.sock"),
	}
}

// NewDocker finds the first live Docker socket, or returns nil when no
// container runtime is running (containers are simply omitted then).
func NewDocker() *Docker {
	if sock := os.Getenv("DOCKER_HOST"); strings.HasPrefix(sock, "unix://") {
		return dockerAt(strings.TrimPrefix(sock, "unix://"))
	}
	for _, s := range dockerSockets() {
		if _, err := os.Stat(s); err == nil {
			return dockerAt(s)
		}
	}
	return nil
}

func dockerAt(socket string) *Docker {
	return &Docker{
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, "unix", socket)
				},
			},
		},
	}
}
