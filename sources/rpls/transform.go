package rpls

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/frnorm"
)

// rawName is the datadir filename for the upstream raw input.
const rawName = "rpls.raw.csv"

// rawURL is the data.gouv.fr stable "latest" resource for the "Taux de
// logements sociaux dans les Communes" dataset (relayed from Caisse des
// Dépôts open-data). It 302-redirects to the current CSV export; data.gouv
// pins it to the latest published revision, so this URL survives vintage
// bumps (bump dataYear when the upstream vintage changes).
const rawURL = "https://www.data.gouv.fr/api/1/datasets/r/b0d30277-3a14-4673-a988-2fa6c11e030c"

// metaSource is the provenance string recorded in the rebuilt artifact. It
// mirrors the committed snapshot.
const metaSource = `data.gouv.fr "Taux de logements sociaux dans les Communes" (r/b0d30277, vintage 2024)`

// metaNote documents the field semantics in the rebuilt artifact.
const metaNote = "SRU rate (loi SRU article 55) — share of logements locatifs sociaux over résidences principales; computation excludes intermediate LLS. A commune reporting 0% commonly means it is below SRU obligation thresholds."

// dataYear is the SRU vintage of the upstream resource. The CSV does not
// carry it inline; keep it in sync with the resource above.
const dataYear = 2024

// Upstream column headers (semicolon-separated, UTF-8 BOM, CRLF).
const (
	colINSEE = "Code Commune"
	colLabel = "Nom Commune"
	colRate  = "Taux de logements sociaux (%)"
)

// transform rebuilds the processed rpls artifact from the upstream CSV. Each
// row carries one commune: INSEE (Code Commune), name (Nom Commune) and the
// SRU rate (Taux de logements sociaux (%)). Rows with a blank INSEE or a
// blank rate are skipped. Output is gzipped JSON, matching the committed
// .json.gz artifact.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(rc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("rpls: read header: %w", err)
	}
	insee, label, rate := indexOf(header, colINSEE), indexOf(header, colLabel), indexOf(header, colRate)
	if insee < 0 || rate < 0 {
		return fmt.Errorf("rpls: header missing %q/%q: %v", colINSEE, colRate, header)
	}

	idx := Index{
		Meta: Meta{
			Source:   metaSource,
			DataYear: dataYear,
			Note:     metaNote,
		},
		Communes: map[string]Entry{},
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("rpls: read row: %w", err)
		}
		code := strings.TrimSpace(rec[insee])
		if code == "" {
			continue
		}
		pct, ok := parseRate(rec[rate])
		if !ok {
			continue
		}
		e := Entry{RatePct: pct}
		if label >= 0 {
			e.Label = strings.TrimSpace(rec[label])
		}
		idx.Communes[code] = e
	}
	if len(idx.Communes) == 0 {
		return errors.New("rpls: transform produced no communes")
	}
	idx.Meta.RowCountCommunes = len(idx.Communes)

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("rpls: encode json: %w", err)
	}
	return nil
}

// validate gates publication: the rebuilt (gzipped) artifact must parse and
// be non-empty. It runs the real parser, so a corrupt gzip stream or schema
// drift fails loudly before the artifact is published.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("rpls: validated artifact has no communes")
	}
	return nil
}

// indexOf returns the index of the column whose trimmed header equals name,
// or -1.
func indexOf(header []string, name string) int {
	for i, h := range header {
		if strings.TrimSpace(h) == name {
			return i
		}
	}
	return -1
}

// parseRate parses a SRU rate ("7.0", "0,0") into a percentage rounded to one
// decimal. ok is false for an empty/unparseable cell.
func parseRate(s string) (float64, bool) {
	f, ok := frnorm.ParseFRFloat(s)
	if !ok {
		return 0, false
	}
	return math.Round(f*10) / 10, true
}
