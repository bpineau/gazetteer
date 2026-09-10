package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// gateNormalizer counts its calls and blocks until the ctx is done, so a test
// can hold every parallel slot and observe what happens to the queue.
type gateNormalizer struct {
	calls   atomic.Int32
	started chan struct{}
}

func (g *gateNormalizer) Normalize(ctx context.Context, addr string) (gazetteer.Listing, error) {
	g.calls.Add(1)
	select {
	case g.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return gazetteer.Listing{}, ctx.Err()
}

// countingNormalizer resolves instantly and records how many addresses it saw.
type countingNormalizer struct{ calls atomic.Int32 }

func (c *countingNormalizer) Normalize(_ context.Context, addr string) (gazetteer.Listing, error) {
	c.calls.Add(1)
	return gazetteer.Listing{Address: addr}, nil
}

func TestNormalizeCandidates_PreservesOrderAndDecorates(t *testing.T) {
	t.Parallel()
	n := &countingNormalizer{}
	addrs := []string{"a", "b", "c"}
	got, err := normalizeCandidates(context.Background(), n, addrs, func(l *gazetteer.Listing) {
		l.PropertyType = gazetteer.PropertyHouse
	})
	if err != nil {
		t.Fatalf("normalizeCandidates: %v", err)
	}
	for i, a := range addrs {
		if got[i].Address != a {
			t.Errorf("listings[%d] = %q, want %q (input order must survive the fan-out)", i, got[i].Address, a)
		}
		if got[i].PropertyType != gazetteer.PropertyHouse {
			t.Errorf("listings[%d] property type = %v, want the decorated one", i, got[i].PropertyType)
		}
	}
}

// A cancelled ctx must stop the geocoding fan-out instead of queueing (and
// sending) every remaining address: before the ctx arm on the semaphore
// acquisition, an abandoned compare still paid for every BAN round-trip.
func TestNormalizeCandidates_CancelledBeforeStart(t *testing.T) {
	t.Parallel()
	n := &gateNormalizer{started: make(chan struct{}, 1)}
	addrs := []string{"a", "b", "c", "d", "e"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := normalizeCandidates(ctx, n, addrs, func(*gazetteer.Listing) {})
	if got := n.calls.Load(); got != 0 {
		t.Errorf("Normalize calls = %d, want 0 on an already-cancelled ctx", got)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want a context.Canceled chain", err)
	}
	// The first address by index is the one reported, as for a resolve failure.
	if !strings.Contains(fmt.Sprint(err), `"a"`) {
		t.Errorf("err = %v, want it to name the first address", err)
	}
}

// Cancelling mid-flight stops the queue too: only the addresses that already
// hold a slot reach the geocoder.
func TestNormalizeCandidates_CancelledMidFlight(t *testing.T) {
	t.Parallel()
	n := &gateNormalizer{started: make(chan struct{}, 1)}
	addrs := make([]string, 3*maxParallelNormalize)
	for i := range addrs {
		addrs[i] = fmt.Sprintf("addr-%02d", i)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		listings []gazetteer.Listing
		err      error
	}
	done := make(chan outcome, 1)
	go func() {
		l, err := normalizeCandidates(ctx, n, addrs, func(*gazetteer.Listing) {})
		done <- outcome{l, err}
	}()

	<-n.started // the parallel slots are taken
	cancel()
	res := <-done

	// Nothing may start after the cancellation, so at most the addresses
	// holding a parallel slot were sent: the backlog must not be geocoded.
	if got := int(n.calls.Load()); got > maxParallelNormalize {
		t.Errorf("Normalize calls = %d, want at most %d (the backlog must not be geocoded)", got, maxParallelNormalize)
	}
	if got := int(n.calls.Load()); got < 1 {
		t.Errorf("Normalize calls = %d, want at least 1 (the test proves nothing otherwise)", got)
	}
	if !errors.Is(res.err, context.Canceled) {
		t.Errorf("err = %v, want a context.Canceled chain", res.err)
	}
	if res.listings != nil {
		t.Errorf("listings = %v, want nil alongside an error", res.listings)
	}
}
