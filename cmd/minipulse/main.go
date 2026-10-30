// MiniPulse — a tiny, always-on dashboard for a Mac mini (or any
// Apple Silicon Mac) used as a home server: live power draw in watts,
// energy cost, RAM/CPU, a deep "where is my disk going" view, Docker
// containers, services and alerts. One binary, no sudo, no cloud.
//
// Usage:
//
//	minipulse [serve] [--config PATH] [--addr HOST:PORT]
//	minipulse service install|uninstall|status
//	minipulse scan            run one disk scan now and print the top paths
//	minipulse init-config     write an example config file
//	minipulse token           print a fresh random API token
//	minipulse version
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/ricardotech/minipulse/internal/api"
	"github.com/ricardotech/minipulse/internal/collect"
	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/service"
	"github.com/ricardotech/minipulse/internal/store"
)

// Set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "minipulse:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return cmdServe(args)
	case "service":
		return cmdService(args)
	case "scan":
		return cmdScan(args)
	case "init-config":
		path := config.DefaultPath()
		if err := config.WriteExample(path); err != nil {
			return err
		}
		fmt.Println("wrote", path)
		return nil
	case "token":
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return err
		}
		fmt.Println(hex.EncodeToString(b))
		fmt.Fprintln(os.Stderr, "put this in your config as \"token\", then open the dashboard once with ?token=…")
		return nil
	case "version", "--version", "-version":
		fmt.Println("minipulse", version)
		return nil
	default:
		return fmt.Errorf("unknown command %q (serve|service|scan|init-config|token|version)", cmd)
	}
}

func cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "path to config.json")
	addr := fs.String("addr", "", "override listen address, e.g. 127.0.0.1:8848")
	insecure := fs.Bool("insecure-no-auth", false, "allow non-loopback bind without a token (not recommended)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if *addr != "" {
		cfg.Addr = *addr
	}
	if !cfg.IsLoopback() && cfg.Token == "" && !*insecure {
		return fmt.Errorf("refusing to listen on %s without an API token: "+
			"run `minipulse token`, set \"token\" in %s, or bind 127.0.0.1 and use Tailscale Serve", cfg.Addr, *cfgPath)
	}

	// An always-on monitoring agent should stay small: cap the Go heap
	// (disk scans may burst above it briefly, then hand memory back).
	debug.SetMemoryLimit(192 << 20)
	debug.SetGCPercent(50)

	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	coll := collect.New(cfg, st)
	go coll.Run(ctx)

	srv := &http.Server{Addr: cfg.Addr, Handler: api.New(cfg, st, coll).Handler()}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	slog.Info("MiniPulse listening", "addr", "http://"+cfg.Addr, "version", version, "data", cfg.DataDir)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func cmdService(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: minipulse service install|uninstall|status")
	}
	switch args[0] {
	case "install":
		self, err := os.Executable()
		if err != nil {
			return err
		}
		if err := service.Install(self, config.DefaultPath()); err != nil {
			return err
		}
		fmt.Println("installed:", service.PlistPath())
		fmt.Println("MiniPulse will start at login and restart on crash.")
		return nil
	case "uninstall":
		if err := service.Uninstall(); err != nil {
			return err
		}
		fmt.Println("uninstalled")
		return nil
	case "status":
		out, err := service.Status()
		if err != nil {
			return err
		}
		if out == "" {
			fmt.Println("not loaded (run: minipulse service install)")
		} else {
			fmt.Print(out)
		}
		return nil
	default:
		return fmt.Errorf("usage: minipulse service install|uninstall|status")
	}
}

func cmdScan(args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	cfgPath := fs.String("config", config.DefaultPath(), "path to config.json")
	topN := fs.Int("top", 25, "how many entries to print per root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	roots := cfg.DiskScanRoots
	if fs.NArg() > 0 {
		roots = fs.Args()
	}
	st, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	for _, root := range roots {
		res := collect.ScanDir(root, cfg.DiskMinEntryBytes)
		if _, err := st.SaveDiskScan(res.Scan, res.Entries); err != nil {
			return err
		}
		fmt.Printf("\n%s — %s in %d files (%.1fs), biggest first:\n",
			root, humanBytes(res.Scan.TotalBytes), res.Scan.FileCount,
			float64(res.Scan.DurationMs)/1000)
		// Top-level children, descending — the classic `du` answer.
		printed := 0
		for _, e := range res.Entries {
			if e.Parent != root {
				continue
			}
			fmt.Printf("%12s  %s\n", humanBytes(e.Size), e.Path)
			if printed++; printed >= *topN {
				break
			}
		}
	}
	return nil
}

func humanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(u), 0
	for x := n / u; x >= u; x /= u {
		div *= u
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
