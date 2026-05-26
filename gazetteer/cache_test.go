package gazetteer

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
)

func TestMemCache_GetMiss(t *testing.T) {
	c := NewMemCache(10)
	val, hit, err := c.Get(context.Background(), "missing")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Errorf("hit = true, want false on miss")
	}
	if val != nil {
		t.Errorf("val = %v, want nil on miss", val)
	}
}

func TestMemCache_SetThenGet(t *testing.T) {
	c := NewMemCache(10)
	if err := c.Set(context.Background(), "k", []byte("v"), time.Minute); err != nil {
		t.Fatalf("Set: %v", err)
	}
	val, hit, err := c.Get(context.Background(), "k")
	if err != nil || !hit {
		t.Fatalf("Get: err=%v hit=%v", err, hit)
	}
	if string(val) != "v" {
		t.Errorf("val = %q, want %q", val, "v")
	}
}

func TestMemCache_TTLExpiry(t *testing.T) {
	c := NewMemCache(10)
	_ = c.Set(context.Background(), "k", []byte("v"), 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	_, hit, _ := c.Get(context.Background(), "k")
	if hit {
		t.Errorf("hit = true after TTL expiry, want false")
	}
}

func TestMemCache_Eviction(t *testing.T) {
	c := NewMemCache(2)
	_ = c.Set(context.Background(), "a", []byte("1"), time.Minute)
	_ = c.Set(context.Background(), "b", []byte("2"), time.Minute)
	_ = c.Set(context.Background(), "c", []byte("3"), time.Minute) // evicts "a"
	_, hit, _ := c.Get(context.Background(), "a")
	if hit {
		t.Errorf("'a' should have been evicted")
	}
	_, hit, _ = c.Get(context.Background(), "c")
	if !hit {
		t.Errorf("'c' should be present")
	}
}

func TestMemCache_Concurrent(t *testing.T) {
	c := NewMemCache(100)
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			k := []byte{byte(i)}
			_ = c.Set(context.Background(), string(k), k, time.Minute)
			_, _, _ = c.Get(context.Background(), string(k))
		}(i)
	}
	wg.Wait()
	// Just verify no panic / data race; -race must be clean.
}

func TestNewKVMemCache_RoundTrips(t *testing.T) {
	c := NewKVMemCache()
	if c == nil {
		t.Fatal("NewKVMemCache returned nil")
	}
	ctx := context.Background()
	if _, err := c.Get(ctx, "absent"); err == nil {
		t.Errorf("Get on empty cache should error (ErrNotFound), got nil")
	}
	if err := c.Set(ctx, kvcache.Entry{Key: "k", Value: []byte("v")}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := c.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got.Value) != "v" {
		t.Errorf("Value=%q want v", got.Value)
	}
}
