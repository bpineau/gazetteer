package gazetteer

import (
	"errors"
	"fmt"
	"testing"
)

func TestSentinelErrors_Identity(t *testing.T) {
	sentinels := []error{
		ErrInsufficientInputs,
		ErrUnsupportedPropertyType,
		ErrAntiBot,
		ErrUpstreamUnavailable,
		ErrUpstreamSchemaChanged,
		ErrUpstreamPermanent,
		ErrSourceCircuitTripped,
	}
	for _, s := range sentinels {
		if s == nil {
			t.Errorf("sentinel is nil")
		}
		if s.Error() == "" {
			t.Errorf("sentinel %v has empty Error()", s)
		}
	}
}

func TestSentinelErrors_Wrap(t *testing.T) {
	// errors.Is must work through wrapping (standard Go convention).
	wrapped := fmt.Errorf("dvf: %w: missing INSEE", ErrInsufficientInputs)
	if !errors.Is(wrapped, ErrInsufficientInputs) {
		t.Errorf("errors.Is failed to unwrap ErrInsufficientInputs")
	}

	wrapped2 := fmt.Errorf("bienici: %w", ErrAntiBot)
	if !errors.Is(wrapped2, ErrAntiBot) {
		t.Errorf("errors.Is failed to unwrap ErrAntiBot")
	}
	if errors.Is(wrapped2, ErrInsufficientInputs) {
		t.Errorf("errors.Is mistakenly matched unrelated sentinel")
	}
}

func TestNewCircuitTrippedError(t *testing.T) {
	const sourceName = "dvf"
	e := NewCircuitTrippedError(sourceName)
	if e == nil {
		t.Fatal("NewCircuitTrippedError returned nil")
	}
	want := "dvf: upstream circuit tripped, skipping for the rest of this run"
	if got := e.Error(); got != want {
		t.Errorf("Error()\n got: %q\nwant: %q", got, want)
	}
	if !errors.Is(e, ErrSourceCircuitTripped) {
		t.Errorf("errors.Is(e, ErrSourceCircuitTripped) must match")
	}
	if !errors.Is(e, e) {
		t.Errorf("errors.Is(e, e) must match (pointer identity)")
	}
	// Different singletons must not collide.
	other := NewCircuitTrippedError("bienici")
	if errors.Is(e, other) {
		t.Errorf("errors.Is across different per-source singletons must not match")
	}
}
