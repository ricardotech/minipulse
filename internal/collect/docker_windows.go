package collect

import (
	"context"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

// Docker Desktop exposes the Engine API over named pipes: the Linux
// engine (the common case) and the Windows-containers engine.
func dockerPipes() []string {
	return []string{
		`\\.\pipe\dockerDesktopLinuxEngine`,
		`\\.\pipe\docker_engine`,
	}
}

// NewDocker finds the first reachable Docker named pipe, or returns
// nil when no container runtime is running (containers are simply
// omitted then). Honors DOCKER_HOST in npipe:// form.
func NewDocker() *Docker {
	if host := os.Getenv("DOCKER_HOST"); strings.HasPrefix(host, "npipe://") {
		if d := dockerPipeAt(strings.TrimPrefix(host, "npipe://")); d != nil {
			return d
		}
		return nil
	}
	for _, p := range dockerPipes() {
		if d := dockerPipeAt(p); d != nil {
			return d
		}
	}
	return nil
}

// dockerPipeAt probes the pipe once so a missing runtime is
// detected at discovery time (like os.Stat on the unix socket),
// then returns a client that dials it per request.
func dockerPipeAt(path string) *Docker {
	timeout := 2 * time.Second
	conn, err := winio.DialPipe(path, &timeout)
	if err != nil {
		return nil
	}
	conn.Close()
	return &Docker{
		client: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return winio.DialPipeContext(ctx, path)
				},
			},
		},
	}
}
