package dataset

import (
	"bufio"
	"io"
)

// BOMReader returns a reader that transparently skips a leading UTF-8 byte
// order mark (EF BB BF) if present. French open-data CSV exports frequently
// carry one, which would otherwise corrupt the first column's header. It is
// a small convenience for Transform implementations.
func BOMReader(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	return br
}
