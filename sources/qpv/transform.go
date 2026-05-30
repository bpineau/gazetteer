package qpv

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// rawName is the datadir filename for the upstream raw input — the ANCT
// "Liste des quartiers prioritaires de la politique de la ville 2024"
// CSV (one row per QPV, COG 2024 communal geography).
const rawName = "listeqp2024.raw.csv"

// rawURL is the ANCT national QPV 2024 list (format COG 2024), published
// on the data.gouv.fr dataset slug
// quartiers-prioritaires-de-la-politique-de-la-ville-qpv. data.gouv mints
// a dated static path per revision; bump this when the ANCT republishes
// the list (the slug page lists the current resource).
const rawURL = "https://static.data.gouv.fr/resources/quartiers-prioritaires-de-la-politique-de-la-ville-qpv/20260116-110350/listeqp2024-cog2024.csv"

// metaSource is the provenance string recorded in the rebuilt artifact —
// kept byte-identical to the committed embed so a refresh is a no-op diff.
const metaSource = "data.gouv.fr ANCT — Quartiers Prioritaires Politique de la Ville (QPV 2024)"

// metaNote documents the artifact semantics; byte-identical to the embed.
const metaNote = "Communes hosting at least one QPV. Effective 1 January 2024 (decree 2023-1314)."

// Upstream column headers in listeqp2024-cog2024.csv.
const (
	colCodeQP = "code_qp"   // QPV code, format "QNXXXYYZ"
	colLibQP  = "lib_qp"    // QPV name
	colInsee  = "insee_com" // hosting commune INSEE (a "; "-joined list when the QPV spans communes)
	colLibCom = "lib_com"   // hosting commune name (matching "; "-joined when multi-commune)
)

// transform rebuilds the processed qpv artifact from the upstream ANCT CSV.
// Each row is one QPV with its hosting commune(s). The transform groups
// QPVs under their insee_com key — a single INSEE for single-commune QPVs
// (zero-padded to 5 digits), or the verbatim "; "-joined list the ANCT
// publishes for QPVs straddling several communes. lib_com supplies the
// commune label (also "; "-joined when multi-commune). QPVs are sorted by
// code within each commune so the output is deterministic.
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
		return fmt.Errorf("qpv: read header: %w", err)
	}
	codeQP := indexOf(header, colCodeQP)
	libQP := indexOf(header, colLibQP)
	insee := indexOf(header, colInsee)
	libCom := indexOf(header, colLibCom)
	if codeQP < 0 || libQP < 0 || insee < 0 || libCom < 0 {
		return fmt.Errorf("qpv: header missing required columns: %v", header)
	}

	communes := map[string]Entry{}
	qpvCount := 0
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("qpv: read row: %w", err)
		}
		code := strings.TrimSpace(rec[codeQP])
		if code == "" {
			continue
		}
		key := communeKey(rec[insee])
		if key == "" {
			continue
		}
		e := communes[key]
		e.Label = strings.TrimSpace(rec[libCom])
		e.QPVs = append(e.QPVs, QPV{Code: code, Label: strings.TrimSpace(rec[libQP])})
		communes[key] = e
		qpvCount++
	}
	if len(communes) == 0 {
		return errors.New("qpv: transform produced no communes")
	}
	for k, e := range communes {
		sort.Slice(e.QPVs, func(i, j int) bool { return e.QPVs[i].Code < e.QPVs[j].Code })
		communes[k] = e
	}

	idx := Index{
		Meta: Meta{
			Source:           metaSource,
			RowCountCommunes: len(communes),
			RowCountQPV:      qpvCount,
			Note:             metaNote,
		},
		Communes: communes,
	}
	return json.NewEncoder(dst).Encode(idx)
}

// communeKey normalises the upstream insee_com field into the artifact key.
// A bare commune code is zero-padded to the canonical 5 digits (the ANCT
// CSV drops the leading zero for departments < 10, e.g. "1053"). A
// multi-commune QPV arrives as a "; "-joined list already zero-padded by
// the ANCT (e.g. "02571; 02691"); it is kept verbatim.
func communeKey(insee string) string {
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return ""
	}
	if strings.Contains(insee, ";") {
		return insee
	}
	for len(insee) < 5 {
		insee = "0" + insee
	}
	return insee
}

// validate gates publication: the rebuilt artifact must parse and be
// non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("qpv: validated artifact has no communes")
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
