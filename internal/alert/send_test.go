package alert

import (
	"context"
	"net"
	"strings"
	"testing"
)

// The webhook URL can carry an ntfy topic that acts as a write
// secret; transport errors (*url.Error embeds the full URL in its
// string) get logged by callers and must not leak it.
func TestSendErrorDoesNotLeakWebhookURL(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close() // guaranteed connection-refused port

	err = Send(context.Background(), "http://"+addr+"/my-secret-topic", Notice{Title: "x", Detail: "y"})
	if err == nil {
		t.Fatal("expected send error to closed port")
	}
	// The host:port in a dial error is fine; the secret is the path.
	if strings.Contains(err.Error(), "my-secret-topic") {
		t.Errorf("error leaks webhook path (ntfy topic): %v", err)
	}
}
