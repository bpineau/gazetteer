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
