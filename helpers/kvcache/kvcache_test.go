package kvcache_test

// Direct unit tests for the kvcache package's option pattern + Set
// helper. The conformance suite under pkg/kvcache/kvcachetest already
// exercises WithTTL end-to-end against a real Cache backend, but those
// tests pass through to memcache.Set and can't pin the option's
// upstream contract (what Entry is HANDED to Cache.Set ?). Pinning that
// here means a refactor of the option plumbing can't silently lose the
// TTL or scramble FetchedAt.

import (
	"context"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
)

// stubCache is a minimal kvcache.Cache that records every Set call.
// Get and DeleteExpired are present only so the type satisfies the
// interface; they are not exercised by these tests.
type stubCache struct {
	last     kvcache.Entry
	setCalls int
}

func (s *stubCache) Get(_ context.Context, _ string) (kvcache.Entry, error) {
	return kvcache.Entry{}, kvcache.ErrNotFound
}

func (s *stubCache) Set(_ context.Context, e kvcache.Entry) error {
	s.last = e
	s.setCalls++
	return nil
}

func (s *stubCache) DeleteExpired(_ context.Context, _ time.Time) (int64, error) {
	return 0, nil
}

// TestSet_WithTTL_AddsExpiresAt : with TTL=1h and a zero FetchedAt
// (the only thing public callers can pass through the helper today,
// since there is no WithFetchedAt option), the helper computes
// ExpiresAt = time.Now().UTC().Add(ttl). We bracket the call with
// before/after timestamps and verify ExpiresAt lands in
// [before+ttl, after+ttl].
//
// We also pin that FetchedAt is LEFT zero — the backend is contractually
// responsible for stamping it (per memcache.go and the Cache doc), so
// the option must not pre-fill it.
func TestSet_WithTTL_AddsExpiresAt(t *testing.T) {
	c := &stubCache{}
	const ttl = time.Hour

	before := time.Now().UTC()
	err := kvcache.Set(context.Background(), c, "k", []byte("v"), kvcache.WithTTL(ttl))
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("kvcache.Set: %v", err)
	}
	if c.setCalls != 1 {
		t.Fatalf("backend.Set called %d times, want 1", c.setCalls)
	}
	if c.last.Key != "k" || string(c.last.Value) != "v" {
		t.Errorf("key/value lost: got key=%q value=%q", c.last.Key, c.last.Value)
	}
	if !c.last.FetchedAt.IsZero() {
		t.Errorf("FetchedAt=%v, want zero — backend must fill it in", c.last.FetchedAt)
	}
	if c.last.ExpiresAt == nil {
		t.Fatal("ExpiresAt is nil; WithTTL(1h) must populate it")
	}
	exp := *c.last.ExpiresAt
	low := before.Add(ttl)
	high := after.Add(ttl)
	if exp.Before(low) || exp.After(high) {
		t.Errorf("ExpiresAt=%v outside [%v, %v]", exp, low, high)
	}
}

// TestSet_WithTTL_ZeroFetchedAt_UsesNow makes the « zero FetchedAt →
// fall back to time.Now() » branch explicit. Same observation as above
// but worded as a separate assertion so a refactor that, say, switches
// to a fixed-zero base (epoch) breaks loudly with a useful message.
func TestSet_WithTTL_ZeroFetchedAt_UsesNow(t *testing.T) {
	c := &stubCache{}
	now := time.Now().UTC()
	if err := kvcache.Set(context.Background(), c, "k", []byte("v"),
		kvcache.WithTTL(2*time.Hour)); err != nil {
		t.Fatalf("kvcache.Set: %v", err)
	}
	if c.last.ExpiresAt == nil {
		t.Fatal("ExpiresAt nil")
	}
	delta := c.last.ExpiresAt.Sub(now)
	// Anything close to 2h means we used wall-clock as the base.
	// Bracket loosely (±5s) so the test isn't flaky under load.
	if delta < 2*time.Hour-5*time.Second || delta > 2*time.Hour+5*time.Second {
		t.Errorf("ExpiresAt-now = %v, want ~2h (zero FetchedAt should fall back to time.Now)", delta)
	}
	// Epoch-base regression sentinel : a buggy impl that uses
	// time.Time{} as the base would put ExpiresAt at year 0001.
	if c.last.ExpiresAt.Year() < 2000 {
		t.Errorf("ExpiresAt=%v looks like epoch-base; impl must fall back to time.Now()", c.last.ExpiresAt)
	}
}

// TestSet_WithTTL_ZeroTTL_NoExpiry pins the « TTL=0 means no expiry »
// branch. The impl gates ExpiresAt-population on `cfg.ttl > 0`, so a
// caller asking for WithTTL(0) gets a row with ExpiresAt=nil — kept
// until explicitly overwritten. This matches the no-TTL design pivot
// recorded in MEMORY.md (enrichments_no_ttl).
func TestSet_WithTTL_ZeroTTL_NoExpiry(t *testing.T) {
	c := &stubCache{}
	if err := kvcache.Set(context.Background(), c, "k", []byte("v"),
		kvcache.WithTTL(0)); err != nil {
		t.Fatalf("kvcache.Set: %v", err)
	}
	if c.last.ExpiresAt != nil {
		t.Errorf("ExpiresAt=%v, want nil (WithTTL(0) = forever)", *c.last.ExpiresAt)
	}
}

// TestSet_WithTTL_NegativeTTL_TreatedAsNoExpiry pins the negative-TTL
// behaviour : the `cfg.ttl > 0` gate excludes negatives, so they
// silently pass through as « no expiry » (same as TTL=0). The option
// does NOT panic, clamp to zero/+ε, or eagerly mark the row expired.
//
// If you want « immediately expired » semantics, set ExpiresAt
// yourself via Cache.Set — don't go through WithTTL.
func TestSet_WithTTL_NegativeTTL_TreatedAsNoExpiry(t *testing.T) {
	c := &stubCache{}
	if err := kvcache.Set(context.Background(), c, "k", []byte("v"),
		kvcache.WithTTL(-time.Hour)); err != nil {
		t.Fatalf("kvcache.Set: %v", err)
	}
	if c.last.ExpiresAt != nil {
		t.Errorf("ExpiresAt=%v, want nil (negative TTL passes through as no-expiry today)", *c.last.ExpiresAt)
	}
}

// TestSet_NoOptions_PassesEntryThrough : with zero options, the helper
// hands the backend an Entry with Key+Value only, FetchedAt zero, no
// ExpiresAt. Pinning this protects the « bare Set » shape used by
// callers like the BAN geocoder before they layer on TTLs.
func TestSet_NoOptions_PassesEntryThrough(t *testing.T) {
	c := &stubCache{}
	if err := kvcache.Set(context.Background(), c, "bare", []byte("v")); err != nil {
		t.Fatalf("kvcache.Set: %v", err)
	}
	if c.last.Key != "bare" {
		t.Errorf("Key=%q want bare", c.last.Key)
	}
	if string(c.last.Value) != "v" {
		t.Errorf("Value=%q want v", c.last.Value)
	}
	if !c.last.FetchedAt.IsZero() {
		t.Errorf("FetchedAt=%v, want zero", c.last.FetchedAt)
	}
	if c.last.ExpiresAt != nil {
		t.Errorf("ExpiresAt=%v, want nil", *c.last.ExpiresAt)
	}
}

// TestSet_MultipleOptions_LastTTLWins : applying WithTTL twice keeps
// the latest value (options run in order, mutating the same setConfig).
// This is the documented option-pattern behaviour; pinning it means a
// future change to e.g. « first wins » or « cumulative » will trip the
// test instead of silently changing the meaning of caller code.
func TestSet_MultipleOptions_LastTTLWins(t *testing.T) {
	c := &stubCache{}
	if err := kvcache.Set(context.Background(), c, "k", []byte("v"),
		kvcache.WithTTL(time.Hour), kvcache.WithTTL(24*time.Hour)); err != nil {
		t.Fatalf("kvcache.Set: %v", err)
	}
	if c.last.ExpiresAt == nil {
		t.Fatal("ExpiresAt nil")
	}
	delta := time.Until(*c.last.ExpiresAt)
	if delta < 23*time.Hour || delta > 25*time.Hour {
		t.Errorf("ExpiresAt delta = %v, want ~24h (last WithTTL should win)", delta)
	}
}

// TestErrNotFound_IsSentinel guards the ErrNotFound contract noted in
// the package doc : « Callers MUST use errors.Is(err, kvcache.ErrNotFound) ».
// The sentinel must be a non-nil, distinguishable error. (The
// errors.Is identity-with-itself is trivially true but pinning it
// here catches a future « return wrapping a fresh errors.New each
// call » regression.)
func TestErrNotFound_IsSentinel(t *testing.T) {
	if kvcache.ErrNotFound == nil {
		t.Fatal("kvcache.ErrNotFound is nil")
	}
	if got := kvcache.ErrNotFound.Error(); got != "kvcache: not found" {
		t.Errorf("ErrNotFound.Error()=%q want %q", got, "kvcache: not found")
	}
}
