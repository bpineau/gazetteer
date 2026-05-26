package gazetteer

import (
	"context"
	"errors"
)

// Normalizer canonicalises a free-text address into a Listing populated
// with normalized fields (Address, City, Zip, INSEE, Lat, Lon). Concrete
// implementations are installed on a Builder via WithNormalizer and
// reached from a built Client via Client.Normalize.
type Normalizer interface {
	Normalize(ctx context.Context, addr string) (Listing, error)
}

// ErrNormalizerNotConfigured is returned by Client.Normalize when no
// concrete Normalizer was installed via Builder.WithNormalizer.
var ErrNormalizerNotConfigured = errors.New("gazetteer: normalizer not configured (call Builder.WithNormalizer)")

// Normalize delegates to the Client's configured Normalizer. When none
// is configured, returns (Listing{}, ErrNormalizerNotConfigured).
func (c *Client) Normalize(ctx context.Context, addr string) (Listing, error) {
	if c == nil || c.normalizer == nil {
		return Listing{}, ErrNormalizerNotConfigured
	}
	return c.normalizer.Normalize(ctx, addr)
}
