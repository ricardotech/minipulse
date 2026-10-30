// Package alert evaluates threshold rules against samples and
// delivers notifications to an ntfy topic or generic webhook.
// It is intentionally tiny: four rules, a per-rule cooldown, and
// recovery notices when a rule clears.
package alert

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/model"
)

// Notice is one alert (or recovery) emitted by the evaluator.
type Notice struct {
	Rule     string
	Title    string
	Detail   string
	Recovery bool
}

type ruleState struct {
	firing   bool
	lastSent time.Time
}

// Evaluator holds per-rule state. Not safe for concurrent use; the
// collector calls Evaluate from its single sampling goroutine.
type Evaluator struct {
	cfg   *config.Config
	state map[string]*ruleState
}

func New(cfg *config.Config) *Evaluator {
	return &Evaluator{cfg: cfg, state: map[string]*ruleState{}}
}

// Evaluate checks one sample and returns notices to send right now
// (respecting cooldowns). now is injected for testing.
func (e *Evaluator) Evaluate(sm model.Sample, now time.Time) []Notice {
	type check struct {
		rule   string
		breach bool
		title  string
		detail string
	}
	var checks []check
	c := e.cfg
	if c.AlertMemPct > 0 {
		checks = append(checks, check{"memory", sm.MemPct >= c.AlertMemPct,
			"Memory pressure high",
			fmt.Sprintf("RAM at %.0f%% (threshold %.0f%%)", sm.MemPct, c.AlertMemPct)})
	}
	if c.AlertCPUTempC > 0 && sm.Power.Available {
		checks = append(checks, check{"cpu_temp", sm.Power.CPUTempC >= c.AlertCPUTempC,
			"CPU temperature high",
			fmt.Sprintf("CPU at %.0f°C (threshold %.0f°C)", sm.Power.CPUTempC, c.AlertCPUTempC)})
	}
	if c.AlertWatts > 0 && sm.Power.Available {
		checks = append(checks, check{"power", sm.Power.SysW >= c.AlertWatts,
			"Power draw high",
			fmt.Sprintf("Drawing %.1f W (threshold %.0f W)", sm.Power.SysW, c.AlertWatts)})
	}
	if c.AlertDiskPct > 0 {
		for _, v := range sm.Volumes {
			checks = append(checks, check{"disk:" + v.Mount, v.UsedPct >= c.AlertDiskPct,
				"Disk almost full: " + v.Mount,
				fmt.Sprintf("%s at %.0f%% (threshold %.0f%%)", v.Mount, v.UsedPct, c.AlertDiskPct)})
		}
	}

	cooldown := time.Duration(c.AlertCooldownMinutes) * time.Minute
	var out []Notice
	for _, ch := range checks {
		st := e.state[ch.rule]
		if st == nil {
			st = &ruleState{}
			e.state[ch.rule] = st
		}
		switch {
		case ch.breach && !st.firing:
			st.firing, st.lastSent = true, now
			out = append(out, Notice{Rule: ch.rule, Title: ch.title, Detail: ch.detail})
		case ch.breach && st.firing && now.Sub(st.lastSent) >= cooldown:
			st.lastSent = now
			out = append(out, Notice{Rule: ch.rule, Title: ch.title + " (still)", Detail: ch.detail})
		case !ch.breach && st.firing:
			st.firing = false
			out = append(out, Notice{Rule: ch.rule, Title: "Recovered: " + ch.title, Detail: ch.detail, Recovery: true})
		}
	}
	return out
}

// Send delivers a notice. ntfy servers take a plain-text POST with a
// Title header; generic webhooks get the same body, which most
// receivers (ntfy, Apprise, plain HTTP sinks) accept as-is.
func Send(ctx context.Context, webhookURL string, n Notice) error {
	if webhookURL == "" {
		return nil
	}
	body := n.Detail
	if body == "" {
		body = n.Title
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Title", "MiniPulse: "+n.Title)
	req.Header.Set("Tags", map[bool]string{true: "white_check_mark", false: "warning"}[n.Recovery])
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		// Unwrap *url.Error: its string embeds the full webhook URL,
		// and an ntfy topic in the path is a write secret that would
		// otherwise land in the service log.
		var ue *url.Error
		if errors.As(err, &ue) {
			return fmt.Errorf("webhook send: %w", ue.Err)
		}
		return fmt.Errorf("webhook send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		// Don't put the URL in the error: an ntfy topic in the path
		// acts as a write secret and errors end up in logs.
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		return fmt.Errorf("webhook returned %s", resp.Status)
	}
	return nil
}
