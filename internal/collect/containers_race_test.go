package collect

import (
	"context"
	"sync"
	"testing"

	"github.com/ricardotech/minipulse/internal/config"
	"github.com/ricardotech/minipulse/internal/store"
)

// Run with -race: Containers is called from concurrent HTTP handlers
// and lazily (re)discovers the Docker client. Regression test for
// the unsynchronized c.docker reads/writes.
func TestContainersConcurrentSafe(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c := New(config.Defaults(), st)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				_, _ = c.Containers(context.Background())
			}
		}()
	}
	wg.Wait()
}

// TestSubscribeCap: leaked/hostile SSE clients must not pile up
// unbounded goroutines fds.
func TestSubscribeCap(t *testing.T) {
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	c := New(config.Defaults(), st)
	leaves := []func(){}
	for i := 0; i < maxSubscribers; i++ {
		_, leave, ok := c.Subscribe()
		if !ok {
			t.Fatalf("subscriber %d refused before cap", i)
		}
		leaves = append(leaves, leave)
	}
	if _, _, ok := c.Subscribe(); ok {
		t.Errorf("subscriber %d accepted beyond cap %d", maxSubscribers+1, maxSubscribers)
	}
	leaves[0]()
	if _, leave, ok := c.Subscribe(); !ok {
		t.Errorf("slot not freed after leave")
	} else {
		leave()
	}
	for _, l := range leaves[1:] {
		l()
	}
}
