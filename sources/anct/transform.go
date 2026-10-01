package anct

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// Raw input filenames in the datadir. Each is one upstream programme list,
// kept on disk for troubleshooting and reprocessing.
const (
	rawACVName = "anct_acv.raw.csv"
	rawPVDName = "anct_pvd.raw.csv"
	rawORTName = "anct_ort.raw.csv"
)

// Raw input URLs.
//
//   - ACV / PVD are the ANCT national "communes bénéficiaires" CSV lists
//     (datasets programme-action-coeur-de-ville / programme-petites-villes-de-demain).
//   - ORT is the national "Liste des communes couvertes par des opérations
//     de revitalisation de territoire" published by the Ministère de la
//     Cohésion des territoires (a Grist document).
//
// ACV and PVD are addressed by their stable data.gouv.fr resource ids, which
// redirect to the current dated upload, so a new list uploaded over the same
// resource is picked up as is. ORT is the CSV export of the Grist document's
// raw BDD table, every row of it: an export taken through a view section
// (viewSection=N) inherits whatever filter or linking that page carries in
// the document, and one such page silently narrowed the export to a single
// département. The lists grow over time as new conventions are signed.
const (
	rawACVURL = "https://www.data.gouv.fr/fr/datasets/r/8b6f422b-cbdf-459a-9a16-d6be4b92d91a"
	rawPVDURL = "https://www.data.gouv.fr/fr/datasets/r/1fa831ec-d912-4277-8b95-a8b998bf951e"
	rawORTURL = "https://grist.numerique.gouv.fr/o/dgaln/api/docs/j4i9oKD3jzFtgEUuM9sXnL/download/csv?tableId=BDD"
)

// Publication floors checked by validate. Each list only grows as
// conventions are signed (ORT also loses the conventions that end), and
// stood at 245 ACV, about 1 640 PVD and about 2 200 signed ORT communes in
// 2026. A rebuild far below these is a truncated or filtered upstream
// export, not a policy change, and must not replace the embedded data.
const (
	minACV = 200
	minPVD = 1200
	minORT = 1500
)

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "data.gouv.fr ANCT — Action Coeur de Ville + Petites Villes de Demain + ORT"

// metaNote documents the row semantics in the rebuilt artifact.
const metaNote = "One row per commune participating in at least one programme."

// Upstream column headers.
const (
	// ACV and PVD share the commune-code / label columns; both carry a
	// signature date (ACV as DD-MM-YYYY, PVD as YYYY-MM-DD).
	colINSEE = "insee_com"
	colLib   = "lib_com"
	colDate  = "date_signature"

	// ORT columns. The list covers candidate and signed conventions; only
	// rows whose "Signée ?" is "Signée" are flagged.
	colORTINSEE  = "Code commune"
	colORTSigned = "Signée ?"
	colORTDate   = "Si signée, date de signature"
)

// ortSignedValue is the "Signée ?" cell value that marks a signed ORT
// convention (the column also carries non-signed candidate rows).
const ortSignedValue = "Signée"

// transform rebuilds the processed ANCT artifact by merging the three
// upstream lists into one per-commune index, keyed by INSEE. A commune is
// recorded once it participates in at least one programme; its label comes
// from whichever of ACV / PVD listed it (both agree on overlap). The result
// is written as plain (non-gzipped) JSON.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	idx := Index{
		Meta:     Meta{Source: metaSource, Note: metaNote},
		Communes: map[string]Entry{},
	}

	acvCount, err := mergeACV(raw, &idx)
	if err != nil {
		return err
	}
	pvdCount, err := mergePVD(raw, &idx)
	if err != nil {
		return err
	}
	ortCount, err := mergeORT(raw, &idx)
	if err != nil {
		return err
	}

	if len(idx.Communes) == 0 {
		return errors.New("anct: transform produced no communes")
	}
	idx.Meta.RowCountCommunes = len(idx.Communes)
	idx.Meta.RowCountACV = acvCount
	idx.Meta.RowCountPVD = pvdCount
	idx.Meta.RowCountORT = ortCount

	return json.NewEncoder(dst).Encode(idx)
}

// mergeACV folds the Action Cœur de Ville list into idx, returning the
// number of ACV communes. Its signature date is DD-MM-YYYY.
func mergeACV(raw dataset.RawSet, idx *Index) (int, error) {
	rows, err := readCSV(raw, rawACVName, ',', []string{colINSEE, colLib, colDate})
	if err != nil {
		return 0, fmt.Errorf("anct: acv: %w", err)
	}
	n := 0
	for _, r := range rows {
		insee := strings.TrimSpace(r[colINSEE])
		if insee == "" {
			continue
		}
		e := idx.Communes[insee]
		e.ACV = true
		e.ACVSignedAt = dmyToISO(r[colDate])
		if lib := strings.TrimSpace(r[colLib]); lib != "" {
			e.Label = lib
		}
		idx.Communes[insee] = e
		n++
	}
	return n, nil
}

// mergePVD folds the Petites Villes de Demain list into idx, returning the
// number of PVD communes. Its signature date was ISO (YYYY-MM-DD) until the
// 2026 list switched to DD/MM/YYYY; dmyToISO takes both.
func mergePVD(raw dataset.RawSet, idx *Index) (int, error) {
	rows, err := readCSV(raw, rawPVDName, ',', []string{colINSEE, colLib, colDate})
	if err != nil {
		return 0, fmt.Errorf("anct: pvd: %w", err)
	}
	n := 0
	for _, r := range rows {
		insee := strings.TrimSpace(r[colINSEE])
		if insee == "" {
			continue
		}
		e := idx.Communes[insee]
		e.PVD = true
		e.PVDSignedAt = dmyToISO(r[colDate])
		if e.Label == "" {
			if lib := strings.TrimSpace(r[colLib]); lib != "" {
				e.Label = lib
			}
		}
		idx.Communes[insee] = e
		n++
	}
	return n, nil
}

// mergeORT folds the signed ORT conventions into idx, returning the number
// of signed-ORT communes. Only rows whose "Signée ?" is "Signée" count; the
// ORT list carries no usable label (its "Commune" column embeds the
// department prefix), so labels are left to ACV / PVD. A convention whose
// status reads "Terminée" has ended and does not count. Date is ISO.
func mergeORT(raw dataset.RawSet, idx *Index) (int, error) {
	rows, err := readCSV(raw, rawORTName, ',', []string{colORTINSEE, colORTSigned, colORTDate})
	if err != nil {
		return 0, fmt.Errorf("anct: ort: %w", err)
	}
	n := 0
	for _, r := range rows {
		insee := strings.TrimSpace(r[colORTINSEE])
		if insee == "" {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(r[colORTSigned]), ortSignedValue) {
			continue
		}
		e := idx.Communes[insee]
		e.ORT = true
		e.ORTSignedAt = dmyToISO(r[colORTDate])
		idx.Communes[insee] = e
		n++
	}
	return n, nil
}

// readCSV opens name from raw, parses it with the given delimiter, and
// returns each data row as a column-name→value map restricted to want. It
// errors when any requested column is absent from the header.
func readCSV(raw dataset.RawSet, name string, comma rune, want []string) ([]map[string]string, error) {
	rc, err := raw.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(rc))
	cr.Comma = comma
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	for _, w := range want {
		if _, ok := col[w]; !ok {
			return nil, fmt.Errorf("header missing column %q: %v", w, header)
		}
	}

	var out []map[string]string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		m := make(map[string]string, len(want))
		for _, w := range want {
			if i := col[w]; i < len(rec) {
				m[w] = rec[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}

// dmyToISO converts a day-first "DD-MM-YYYY" or "DD/MM/YYYY" date to
// "YYYY-MM-DD". Inputs already in ISO form (or unrecognised) are returned
// trimmed/unchanged. The ACV list uses day-first French dates with dashes,
// the PVD list since 2026 with slashes.
func dmyToISO(s string) string {
	s = strings.TrimSpace(s)
	p := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '/' })
	if len(p) != 3 {
		return s
	}
	if len(p[0]) == 2 && len(p[1]) == 2 && len(p[2]) == 4 { // DD-MM-YYYY, DD/MM/YYYY
		return p[2] + "-" + p[1] + "-" + p[0]
	}
	return s // already YYYY-MM-DD (or unknown)
}

// validate gates publication: the rebuilt artifact must parse and each
// programme list must reach its floor (minACV, minPVD, minORT), so a
// truncated or filtered upstream export fails the refresh instead of
// silently dropping communes from the embedded data.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("anct: validated artifact has no communes")
	}
	m := idx.Meta
	if m.RowCountACV < minACV || m.RowCountPVD < minPVD || m.RowCountORT < minORT {
		return fmt.Errorf("anct: a programme list is below its floor (ACV %d, min %d; PVD %d, min %d; ORT %d, min %d): truncated upstream export?",
			m.RowCountACV, minACV, m.RowCountPVD, minPVD, m.RowCountORT, minORT)
	}
	return nil
}
