// Package kvcachetest is a conformance suite for kvcache.Cache
// implementations. Backends call Suite(t, factory) and get coverage of
// the behavioural contract documented on kvcache.Cache:
//
//   - Get on a missing key returns ErrNotFound (errors.Is-compatible).
//   - Set then Get round-trips Key, Value, FetchedAt and ExpiresAt.
//   - Set with the same key replaces the prior row.
//   - Get returns expired rows (stale-while-revalidate is the contract).
//   - DeleteExpired removes expired rows, leaves fresh + permanent rows
//     untouched, and returns an accurate count.
//   - Concurrent Get/Set/DeleteExpired do not race (run with -race).
//
// The factory MUST return a fresh, empty Cache for each invocation.
package kvcachetest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
)

// Factory returns a fresh, empty Cache. The suite calls it once per
// sub-test so backends do not need to worry about cross-test isolation.
type Factory func(t *testing.T) kvcache.Cache

// Suite runs the full conformance suite against the given factory.
func Suite(t *testing.T, newCache Factory) {
	t.Helper()
	t.Run("GetMissingReturnsErrNotFound", func(t *testing.T) {
		testGetMissing(t, newCache(t))
	})
	t.Run("SetGetRoundTrip", func(t *testing.T) {
		testSetGetRoundTrip(t, newCache(t))
	})
	t.Run("SetReplaces", func(t *testing.T) {
		testSetReplaces(t, newCache(t))
	})
	t.Run("GetReturnsExpiredRow", func(t *testing.T) {
		testGetReturnsExpiredRow(t, newCache(t))
	})
	t.Run("SetFillsFetchedAtWhenZero", func(t *testing.T) {
		testSetFillsFetchedAt(t, newCache(t))
	})
	t.Run("DeleteExpiredCountsAndKeepsFresh", func(t *testing.T) {
		testDeleteExpired(t, newCache(t))
	})
	t.Run("ParallelAccess", func(t *testing.T) {
		testParallelAccess(t, newCache(t))
	})
	t.Run("HelperSetWithTTL", func(t *testing.T) {
		testHelperSetWithTTL(t, newCache(t))
	})
}

func testGetMissing(t *testing.T, c kvcache.Cache) {
	t.Helper()
	_, err := c.Get(context.Background(), "missing")
	if !errors.Is(err, kvcache.ErrNotFound) {
		t.Fatalf("Get(missing): want ErrNotFound, got %v", err)
	}
}

func testSetGetRoundTrip(t *testing.T, c kvcache.Cache) {
	t.Helper()
	ctx := context.Background()
	exp := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	fetched := time.Date(2026, 5, 5, 10, 11, 12, 0, time.UTC)
	in := kvcache.Entry{
		Key:       "k",
		Value:     []byte(`{"v":1}`),
		FetchedAt: fetched,
		ExpiresAt: &exp,
	}
	if err := c.Set(ctx, in); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Key != in.Key {
		t.Errorf("Key=%q want %q", got.Key, in.Key)
	}
	if string(got.Value) != string(in.Value) {
		t.Errorf("Value=%q want %q", got.Value, in.Value)
	}
	if !got.FetchedAt.Equal(fetched) {
		t.Errorf("FetchedAt=%v want %v", got.FetchedAt, fetched)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(exp) {
		t.Errorf("ExpiresAt=%v want %v", got.ExpiresAt, exp)
	}
}

func testSetReplaces(t *testing.T, c kvcache.Cache) {
	t.Helper()
	ctx := context.Background()
	if err := c.Set(ctx, kvcache.Entry{Key: "k", Value: []byte("v1")}); err != nil {
		t.Fatalf("Set 1: %v", err)
	}
	if err := c.Set(ctx, kvcache.Entry{Key: "k", Value: []byte("v2")}); err != nil {
		t.Fatalf("Set 2: %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Value) != "v2" {
		t.Errorf("Value=%q want v2 (upsert)", got.Value)
	}
}

func testGetReturnsExpiredRow(t *testing.T, c kvcache.Cache) {
	t.Helper()
	ctx := context.Background()
	past := time.Now().Add(-time.Hour)
	if err := c.Set(ctx, kvcache.Entry{
		Key:       "stale",
		Value:     []byte("payload"),
		ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "stale")
	if err != nil {
		t.Fatalf("Get expired: want row returned, got err %v", err)
	}
	if string(got.Value) != "payload" {
		t.Errorf("Value=%q want payload", got.Value)
	}
	if got.ExpiresAt == nil || !got.ExpiresAt.Equal(past) {
		t.Errorf("ExpiresAt round-trip lost: got %v want %v", got.ExpiresAt, past)
	}
}

func testSetFillsFetchedAt(t *testing.T, c kvcache.Cache) {
	t.Helper()
	ctx := context.Background()
	before := time.Now().Add(-time.Second)
	if err := c.Set(ctx, kvcache.Entry{Key: "k", Value: []byte("v")}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.FetchedAt.IsZero() {
		t.Fatalf("FetchedAt is zero — backend must fill it in")
	}
	if got.FetchedAt.Before(before) {
		t.Errorf("FetchedAt=%v predates Set call (before=%v)", got.FetchedAt, before)
	}
}

func testDeleteExpired(t *testing.T, c kvcache.Cache) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	if err := c.Set(ctx, kvcache.Entry{Key: "old", Value: []byte("1"), ExpiresAt: &past}); err != nil {
		t.Fatalf("Set old: %v", err)
	}
	if err := c.Set(ctx, kvcache.Entry{Key: "fresh", Value: []byte("1"), ExpiresAt: &future}); err != nil {
		t.Fatalf("Set fresh: %v", err)
	}
	if err := c.Set(ctx, kvcache.Entry{Key: "perm", Value: []byte("1")}); err != nil {
		t.Fatalf("Set perm: %v", err)
	}

	n, err := c.DeleteExpired(ctx, now)
	if err != nil {
		t.Fatalf("DeleteExpired: %v", err)
	}
	if n != 1 {
		t.Errorf("DeleteExpired count=%d want 1", n)
	}
	if _, err := c.Get(ctx, "old"); !errors.Is(err, kvcache.ErrNotFound) {
		t.Errorf("'old' should be gone, got %v", err)
	}
	if _, err := c.Get(ctx, "fresh"); err != nil {
		t.Errorf("'fresh' should remain, got %v", err)
	}
	if _, err := c.Get(ctx, "perm"); err != nil {
		t.Errorf("'perm' should remain (nil ExpiresAt is forever), got %v", err)
	}
}

func testParallelAccess(t *testing.T, c kvcache.Cache) {
	t.Helper()
	ctx := context.Background()
	const writers = 8
	const ops = 50

	var wg sync.WaitGroup
	wg.Add(writers)
	for w := range writers {
		go func(id int) {
			defer wg.Done()
			for i := range ops {
				key := keyFor(id, i)
				_ = c.Set(ctx, kvcache.Entry{Key: key, Value: []byte("v")})
				_, _ = c.Get(ctx, key)
			}
		}(w)
	}

	// Concurrent reader hammering keys that may or may not exist yet.
	wg.Go(func() {
		for i := range ops * writers {
			_, _ = c.Get(ctx, keyFor(i%writers, i%ops))
		}
	})

	// Concurrent DeleteExpired call — must not panic / race.
	wg.Go(func() {
		_, _ = c.DeleteExpired(ctx, time.Now().Add(-time.Hour))
	})

	wg.Wait()
}

func testHelperSetWithTTL(t *testing.T, c kvcache.Cache) {
	t.Helper()
	ctx := context.Background()
	if err := kvcache.Set(ctx, c, "tkey", []byte("payload"), kvcache.WithTTL(time.Hour)); err != nil {
		t.Fatalf("kvcache.Set: %v", err)
	}
	got, err := c.Get(ctx, "tkey")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ExpiresAt == nil {
		t.Fatalf("kvcache.Set with WithTTL must populate ExpiresAt")
	}
	if !got.ExpiresAt.After(time.Now()) {
		t.Errorf("ExpiresAt=%v should be in the future", got.ExpiresAt)
	}
}

func keyFor(writer, i int) string {
	return "p:" + itoa(writer) + ":" + itoa(i)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if negative {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
