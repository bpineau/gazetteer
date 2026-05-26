package gazetteer

import (
	"context"
	"errors"
	"sync"
)

// Normalizer canonicalises a free-text address into a Listing populated
// with normalized fields (Address, City, Zip, INSEE, Lat, Lon). The
// default implementation in this phase returns ErrNormalizerNotConfigured;
// Phase 2 wires the BAN-backed implementation.
type Normalizer interface {
	Normalize(ctx context.Context, addr string) (Listing, error)
}

// ErrNormalizerNotConfigured is returned by NormalizeAddress when no
// concrete Normalizer has been installed via SetDefaultNormalizer.
var ErrNormalizerNotConfigured = errors.New("gazetteer: default normalizer not configured (Phase 2 wires the BAN backend)")

var (
	normMu  sync.RWMutex
	normDef Normalizer = notConfiguredNormalizer{}
)

type notConfiguredNormalizer struct{}

func (notConfiguredNormalizer) Normalize(_ context.Context, _ string) (Listing, error) {
	return Listing{}, ErrNormalizerNotConfigured
}

// SetDefaultNormalizer installs n as the process-wide default. Returns
// the previously installed normalizer so callers (and tests) can restore
// it. Pass nil to revert to the "not configured" sentinel state.
func SetDefaultNormalizer(n Normalizer) Normalizer {
	normMu.Lock()
	defer normMu.Unlock()
	prev := normDef
	if n == nil {
		normDef = notConfiguredNormalizer{}
	} else {
		normDef = n
	}
	return prev
}

// NormalizeAddress is the top-level facade. It delegates to the currently
// installed default Normalizer.
func NormalizeAddress(ctx context.Context, addr string) (Listing, error) {
	normMu.RLock()
	n := normDef
	normMu.RUnlock()
	return n.Normalize(ctx, addr)
}
