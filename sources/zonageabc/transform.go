package zonageabc

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/dataset"
)

// rawCSVName is the datadir filename for the upstream raw input.
const rawCSVName = "zonage_abc.raw.csv"

// rawCSVURL is the "Liste ensemble des communes - Zonage ABC" CSV published
// on data.gouv.fr (dataset slug liste-des-communes-selon-le-zonage-abc).
// data.gouv mints a dated static path per revision; bump this when a new
// arrêté is published (the slug page lists the current resource).
const rawCSVURL = "https://static.data.gouv.fr/resources/liste-des-communes-selon-le-zonage-abc/20250910-150516/liste-des-communes-zonage-abc-5-septembre-2025.csv"

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "data.gouv.fr/datasets/liste-des-communes-selon-le-zonage-abc"

// effectiveDateRE extracts a "5 septembre 2025"-style French date from the
// zone column header ("Zonage en vigueur depuis le 5 septembre 2025").
var effectiveDateRE = regexp.MustCompile(`(\d{1,2})\s+(\p{L}+)\s+(\d{4})`)

// transform rebuilds the processed zonage_abc artifact from the upstream
// CSV. Columns: CODGEO;DEP;LIBGEO;Zonage en vigueur depuis le <date>;…. The
// zone column is matched by its "Zonage" header prefix (its full label
// embeds the revision date and changes each arrêté), and the effective date
// is parsed out of that same header.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawCSVName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(bomReader(rc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("zonageabc: read header: %w", err)
	}
	codgeo, zoneCol := -1, -1
	for i, h := range header {
		switch h := strings.TrimSpace(h); {
		case strings.EqualFold(h, "CODGEO"):
			codgeo = i
		case strings.HasPrefix(strings.ToLower(h), "zonage"):
			zoneCol = i
		}
	}
	if codgeo < 0 || zoneCol < 0 {
		return fmt.Errorf("zonageabc: header missing CODGEO/Zonage columns: %v", header)
	}

	idx := Index{
		Meta: Meta{
			Source:        metaSource,
			DownloadedAt:  time.Now().UTC().Format("2006-01-02"),
			EffectiveDate: parseEffectiveDate(header[zoneCol]),
			Note:          "Zonage A/Abis/B1/B2/C per " + strings.TrimSpace(header[zoneCol]) + ".",
		},
		Communes: map[string]Zone{},
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("zonageabc: read row: %w", err)
		}
		insee := strings.TrimSpace(rec[codgeo])
		zone := strings.TrimSpace(rec[zoneCol])
		if insee == "" || zone == "" {
			continue
		}
		idx.Communes[insee] = Zone(zone)
	}
	idx.Meta.RowCountCommunes = len(idx.Communes)
	if len(idx.Communes) == 0 {
		return errors.New("zonageabc: transform produced no communes")
	}

	enc := json.NewEncoder(dst)
	return enc.Encode(idx)
}

// validate gates publication: the rebuilt artifact must parse and be
// non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("zonageabc: validated artifact has no communes")
	}
	return nil
}

// parseEffectiveDate turns "...depuis le 5 septembre 2025" into "2025-09-05".
// Returns "" when the header carries no recognisable date.
func parseEffectiveDate(header string) string {
	m := effectiveDateRE.FindStringSubmatch(header)
	if m == nil {
		return ""
	}
	month, ok := frenchMonths[strings.ToLower(m[2])]
	if !ok {
		return ""
	}
	day := m[1]
	if len(day) == 1 {
		day = "0" + day
	}
	return fmt.Sprintf("%s-%02d-%s", m[3], month, day)
}

var frenchMonths = map[string]int{
	"janvier": 1, "février": 2, "fevrier": 2, "mars": 3, "avril": 4,
	"mai": 5, "juin": 6, "juillet": 7, "août": 8, "aout": 8,
	"septembre": 9, "octobre": 10, "novembre": 11, "décembre": 12, "decembre": 12,
}

// bomReader strips a leading UTF-8 BOM if present.
func bomReader(r io.Reader) io.Reader {
	br := bufio.NewReader(r)
	if b, err := br.Peek(3); err == nil && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	return br
}
