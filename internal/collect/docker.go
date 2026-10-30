package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ricardotech/minipulse/internal/model"
)

// Docker talks to the Docker Engine API over its unix socket. No SDK:
// the two endpoints used here are stable and tiny.
type Docker struct {
	socket string
	client *http.Client
}

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
		socket: socket,
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

type dockerContainer struct {
	ID     string   `json:"Id"`
	Names  []string `json:"Names"`
	Image  string   `json:"Image"`
	State  string   `json:"State"`
	Status string   `json:"Status"`
}

type dockerStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs  int    `json:"online_cpus"`
	} `json:"cpu_stats"`
	PreCPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
	} `json:"memory_stats"`
}

// Containers lists all containers (running first) with one-shot stats
// for the running ones.
func (d *Docker) Containers(ctx context.Context) ([]model.Container, error) {
	var raw []dockerContainer
	if err := d.get(ctx, "/v1.44/containers/json?all=1", &raw); err != nil {
		return nil, err
	}
	out := make([]model.Container, 0, len(raw))
	for _, c := range raw {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		mc := model.Container{ID: shortID(c.ID), Name: name, Image: c.Image, State: c.State, Status: c.Status}
		if c.State == "running" {
			var st dockerStats
			if err := d.get(ctx, "/v1.44/containers/"+c.ID+"/stats?stream=false", &st); err == nil {
				mc.MemUsed, mc.MemMax = st.MemoryStats.Usage, st.MemoryStats.Limit
				cpuDelta := float64(st.CPUStats.CPUUsage.TotalUsage - st.PreCPUStats.CPUUsage.TotalUsage)
				sysDelta := float64(st.CPUStats.SystemUsage - st.PreCPUStats.SystemUsage)
				if sysDelta > 0 && cpuDelta >= 0 {
					cpus := st.CPUStats.OnlineCPUs
					if cpus < 1 {
						cpus = 1
					}
					mc.CPUPct = cpuDelta / sysDelta * float64(cpus) * 100
				}
			}
		}
		out = append(out, mc)
	}
	return out, nil
}

// shortID truncates a container ID for display, tolerating short or
// malformed IDs from a non-Docker socket (never slice unguarded).
func shortID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (d *Docker) get(ctx context.Context, path string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker %s: %s", path, resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
