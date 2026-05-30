package dataset

import (
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// validateProcessed is the generic well-formedness check Refresh applies to
// a freshly produced processed artifact when the Set supplies no Validate
// of its own. It is intentionally shallow — a structural smoke test that
// catches an empty or truncated transform output, not a schema check. A
// Source that wants its real parser to gate publication sets Set.Validate.
//
// The check is chosen from the filename's extension(s):
//
//	*.gz      → gzip-decodable and the decoded stream is non-empty
//	*.json    → a single valid JSON value spanning the whole stream
//	*.csv     → at least one record parses
//	otherwise → the stream is non-empty
func validateProcessed(name string, r io.Reader) error {
	switch {
	case strings.HasSuffix(name, ".gz"):
		zr, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		defer func() { _ = zr.Close() }()
		inner := strings.TrimSuffix(name, ".gz")
		return validateProcessed(inner, zr)
	case strings.HasSuffix(name, ".json"):
		dec := json.NewDecoder(r)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return fmt.Errorf("json: %w", err)
		}
		if len(v) == 0 {
			return errors.New("json: empty document")
		}
		return nil
	case strings.HasSuffix(name, ".csv"):
		cr := csv.NewReader(r)
		cr.FieldsPerRecord = -1
		if _, err := cr.Read(); err != nil {
			return fmt.Errorf("csv: %w", err)
		}
		return nil
	default:
		n, err := io.Copy(io.Discard, r)
		if err != nil {
			return err
		}
		if n == 0 {
			return errors.New("empty output")
		}
		return nil
	}
}
