package kvgaz_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/kvcache/kvgaz"
	"github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

func TestNew_SetThenGetRoundTrip(t *testing.T) {
	t.Parallel()
	gc := kvgaz.New(memcache.New())
	ctx := context.Background()

	if err := gc.Set(ctx, "k1", []byte("hello"), 0); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, hit, err := gc.Get(ctx, "k1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !hit {
		t.Fatal("Get: hit=false, want true")
	}
	if string(got) != "hello" {
		t.Errorf("Get: %q, want %q", got, "hello")
	}
}

func TestNew_GetMissingReturnsMissNotError(t *testing.T) {
	t.Parallel()
	gc := kvgaz.New(memcache.New())
	got, hit, err := gc.Get(context.Background(), "absent")
	if err != nil {
		t.Fatalf("Get: %v (want nil — miss is not an error)", err)
	}
	if hit {
		t.Errorf("Get: hit=true, want false")
	}
	if got != nil {
		t.Errorf("Get: value=%v, want nil", got)
	}
}

func TestNew_SetWithTTLExpiresOnRead(t *testing.T) {
	t.Parallel()
	mem := memcache.New()
	gc := kvgaz.New(mem)
	ctx := context.Background()

	if err := gc.Set(ctx, "k", []byte("v"), 1*time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Sanity: row is reachable via the wrapper.
	if _, hit, _ := gc.Get(ctx, "k"); !hit {
		t.Fatal("fresh row should hit")
	}

	// Tamper directly with the backing store to simulate "time
	// elapsed": rewrite the row with ExpiresAt in the past.
	past := time.Now().Add(-2 * time.Hour)
	if err := mem.Set(ctx, kvcache.Entry{
		Key:       "k",
		Value:     []byte("v"),
		ExpiresAt: &past,
	}); err != nil {
		t.Fatalf("backend Set: %v", err)
	}

	// The wrapper enforces wall-clock TTL — expired entries register
	// as misses (the row IS still in the backing store, but
	// gazetteer.Cache callers don't see ExpiresAt).
	_, hit, err := gc.Get(ctx, "k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if hit {
		t.Errorf("expired row should register as miss")
	}
}

func TestNew_BackendErrorPropagates(t *testing.T) {
	t.Parallel()
	errBoom := errors.New("boom")
	gc := kvgaz.New(&errCache{err: errBoom})
	_, hit, err := gc.Get(context.Background(), "k")
	if !errors.Is(err, errBoom) {
		t.Errorf("got err=%v, want errBoom", err)
	}
	if hit {
		t.Errorf("hit=true on backend error, want false")
	}
}

func TestNew_ErrNotFoundTranslatesToMiss(t *testing.T) {
	t.Parallel()
	gc := kvgaz.New(&errCache{err: kvcache.ErrNotFound})
	_, hit, err := gc.Get(context.Background(), "k")
	if err != nil {
		t.Errorf("got err=%v, want nil (ErrNotFound → miss)", err)
	}
	if hit {
		t.Errorf("hit=true on ErrNotFound, want false")
	}
}

// errCache returns err on every Get / passes Set through.
type errCache struct {
	err error
}

func (c *errCache) Get(_ context.Context, _ string) (kvcache.Entry, error) {
	return kvcache.Entry{}, c.err
}

func (c *errCache) Set(_ context.Context, _ kvcache.Entry) error {
	return nil
}

func (c *errCache) DeleteExpired(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}
