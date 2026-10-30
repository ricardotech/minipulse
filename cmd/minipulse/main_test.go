package main

import (
	"testing"

	"github.com/ricardotech/minipulse/internal/config"
)

func TestValidateServeConfigOffLoopback(t *testing.T) {
	mk := func(addr, token string) *config.Config {
		cfg := config.Defaults()
		cfg.Addr, cfg.Token = addr, token
		return cfg
	}
	if err := validateServeConfig(mk("192.168.1.5:8848", ""), false, "c.json"); err == nil {
		t.Error("off-loopback without token must be refused")
	}
	if err := validateServeConfig(mk("192.168.1.5:8848", "1234"), false, "c.json"); err == nil {
		t.Error("off-loopback with a 4-char token must be refused (online brute force)")
	}
	if err := validateServeConfig(mk("192.168.1.5:8848", "0123456789abcdef"), false, "c.json"); err != nil {
		t.Errorf("off-loopback with strong token: %v", err)
	}
	if err := validateServeConfig(mk("192.168.1.5:8848", ""), true, "c.json"); err != nil {
		t.Errorf("explicit --insecure-no-auth must be honored: %v", err)
	}
	if err := validateServeConfig(mk("127.0.0.1:8848", ""), false, "c.json"); err != nil {
		t.Errorf("loopback without token is the default and must pass: %v", err)
	}
	if err := validateServeConfig(mk(":8848", ""), false, "c.json"); err == nil {
		t.Error("empty host binds all interfaces and must require a token")
	}
}
