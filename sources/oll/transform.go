package oll

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// Raw input (datadir basename) and upstream URL. The Paris-region observatory
// publishes one ZIP archive per agglomeration perimeter; L7502 is "agglomération
// parisienne hors Paris" (the petite/grande couronne). The archive bundles the
// observed-rent table (Base_OP_<year>_L7502.csv) and the commune→zone map
// (L7502Zonage<year>.csv), both ISO-8859-1, ";"-separated.
const (
	rawL7502Name = "oll_l7502.raw.zip"
	rawL7502URL  = "https://www.observatoires-des-loyers.org/datagouv/2024/Base_OP_2024_L7502.zip"

	aggloCode = "L7502"
	aggloYear = 2024

	// aggloDisplayName is the human-readable perimeter label. The CSV's
	// "agglomeration" column carries the bare code (L7502) for this dataset, so
	// we pin a friendly name here.
	aggloDisplayName = "Agglomération parisienne hors Paris"
)

// transform rebuilds oll_idf.json from the L7502 archive: the commune→zone map
// joined to the observed median rents per (zone, rooms) bucket.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawL7502Name)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	buf, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("oll: read archive: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf), int64(len(buf)))
	if err != nil {
		return fmt.Errorf("oll: open zip: %w", err)
	}

	zonesCSV, err := readZipMember(zr, "zonage")
	if err != nil {
		return fmt.Errorf("oll: zonage member: %w", err)
	}
	rentsCSV, err := readZipMember(zr, "base_op")
	if err != nil {
		return fmt.Errorf("oll: rents member: %w", err)
	}

	zones, err := parseZonage(zonesCSV)
	if err != nil {
		return fmt.Errorf("oll: parse zonage: %w", err)
	}
	rents, err := parseRents(rentsCSV)
	if err != nil {
		return fmt.Errorf("oll: parse rents: %w", err)
	}
	if len(zones) == 0 || len(rents) == 0 {
		return fmt.Errorf("oll: empty transform (zones=%d rents=%d)", len(zones), len(rents))
	}

	out := processed{Agglos: []aggloData{{
		Code: aggloCode, Name: aggloDisplayName, Year: aggloYear, Zones: zones, Rents: rents,
	}}}
	return json.NewEncoder(dst).Encode(out)
}

// readZipMember returns the decoded (ISO-8859-1 → UTF-8) contents of the unique
// .csv member whose base name contains nameSubstr (case-insensitive). It skips
// macOS resource forks (__MACOSX/, ._*) and errors if zero or more than one
// genuine member matches, so a future archive layout change fails loudly rather
// than silently picking the wrong file.
func readZipMember(zr *zip.Reader, nameSubstr string) (string, error) {
	var match *zip.File
	for _, f := range zr.File {
		base := path.Base(f.Name)
		if strings.HasPrefix(f.Name, "__MACOSX/") || strings.HasPrefix(base, "._") {
			continue
		}
		ln := strings.ToLower(base)
		if !strings.HasSuffix(ln, ".csv") || !strings.Contains(ln, nameSubstr) {
			continue
		}
		if match != nil {
			return "", fmt.Errorf("ambiguous: multiple .csv members match %q (%s, %s)", nameSubstr, match.Name, f.Name)
		}
		match = f
	}
	if match == nil {
		return "", fmt.Errorf("no .csv member matching %q", nameSubstr)
	}
	rc, err := match.Open()
	if err != nil {
		return "", err
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return decodeLatin1(b), nil
}

// parseZonage reads the commune→zone map (cols Commune;Lib_com;Iris;Zone;Lib_zone),
// keeping one row per commune (the IRIS column is unused at this granularity).
func parseZonage(text string) ([]zoneRow, error) {
	recs, err := readCSV(text)
	if err != nil {
		return nil, err
	}
	col := headerIndex(recs[0])
	ci, czone, clib := hcol(col, "commune"), hcol(col, "zone"), hcol(col, "lib_zone")
	if ci < 0 || czone < 0 {
		return nil, fmt.Errorf("zonage missing Commune/Zone columns")
	}
	seen := map[string]bool{}
	var out []zoneRow
	for _, r := range recs[1:] {
		insee := strings.TrimSpace(field(r, ci))
		zone := normalizeZone(field(r, czone))
		if insee == "" || zone == "" || seen[insee] {
			continue
		}
		seen[insee] = true
		out = append(out, zoneRow{INSEE: insee, Zone: zone, Label: strings.TrimSpace(field(r, clib))})
	}
	return out, nil
}

// parseRents reads the observed-rent table and keeps the headline cells: one per
// (zone, rooms) for appartements, aggregated over époque and ancienneté.
func parseRents(text string) ([]rentRow, error) {
	recs, err := readCSV(text)
	if err != nil {
		return nil, err
	}
	c := headerIndex(recs[0])
	need := []string{"zone_calcul", "type_habitat", "nombre_pieces_local", "nombre_pieces_homogene",
		"epoque_construction_local", "epoque_construction_homogene",
		"anciennete_locataire_local", "anciennete_locataire_homogene",
		"loyer_median", "loyer_1_quartile", "loyer_3_quartile", "surface_moyenne", "nombre_observations"}
	idx := map[string]int{}
	for _, n := range need {
		i := hcol(c, n)
		if i < 0 {
			return nil, fmt.Errorf("rents missing column %q", n)
		}
		idx[n] = i
	}

	var out []rentRow
	for _, r := range recs[1:] {
		// Headline cell: appartement, a rooms bucket, every other dimension
		// aggregated (blank), with a published median.
		if field(r, idx["type_habitat"]) != "Appartement" ||
			field(r, idx["nombre_pieces_local"]) != "" ||
			field(r, idx["epoque_construction_local"]) != "" || field(r, idx["epoque_construction_homogene"]) != "" ||
			field(r, idx["anciennete_locataire_local"]) != "" || field(r, idx["anciennete_locataire_homogene"]) != "" {
			continue
		}
		// pieces bucket: a blank label is the zone-level all-sizes aggregate
		// (pieces 0), used when the listing has no room count; otherwise
		// "Appart NP" → N.
		ph := field(r, idx["nombre_pieces_homogene"])
		var pieces int
		var openEnded bool
		if ph != "" {
			p, oe, ok := parsePiecesHomogene(ph)
			if !ok {
				continue
			}
			pieces, openEnded = p, oe
		}
		zone := zoneFromCalcul(field(r, idx["zone_calcul"]))
		median, ok := parseFrenchFloat(field(r, idx["loyer_median"]))
		if zone == "" || !ok {
			continue
		}
		q1, _ := parseFrenchFloat(field(r, idx["loyer_1_quartile"]))
		q3, _ := parseFrenchFloat(field(r, idx["loyer_3_quartile"]))
		surf, _ := parseFrenchFloat(field(r, idx["surface_moyenne"]))
		n, _ := strconv.Atoi(strings.TrimSpace(field(r, idx["nombre_observations"])))
		out = append(out, rentRow{
			Zone: zone, Pieces: pieces, OpenEnded: openEnded,
			MedianEURPerM2: median, Q1EURPerM2: q1, Q3EURPerM2: q3, SurfaceM2: surf, N: n,
		})
	}
	return out, nil
}

// readCSV parses a ";"-separated CSV (BOM-tolerant, ragged rows allowed).
func readCSV(text string) ([][]string, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(recs) < 2 {
		return nil, errors.New("csv has no data rows")
	}
	return recs, nil
}

// headerIndex maps lower-cased, trimmed column names to their index.
func headerIndex(header []string) map[string]int {
	m := make(map[string]int, len(header))
	for i, h := range header {
		m[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return m
}

// hcol returns the index of a column by (lower-cased) name, or -1 when absent.
func hcol(m map[string]int, name string) int {
	if i, ok := m[name]; ok {
		return i
	}
	return -1
}

// field returns the i-th field of r, or "" when out of range.
func field(r []string, i int) string {
	if i < 0 || i >= len(r) {
		return ""
	}
	return strings.TrimSpace(r[i])
}

// decodeLatin1 converts ISO-8859-1 bytes to a UTF-8 string (each byte is its
// own code point). The upstream is cp1252-ish, which differs only in 0x80–0x9F;
// none of those bytes appear in the fields the transform retains (zone codes,
// "Appart NP", numeric €/m², and the ASCII "Zone N" labels), so plain Latin-1 is
// exact here. Revisit (charmap.Windows1252) if a future agglo keeps accented
// free-text columns.
func decodeLatin1(b []byte) string {
	rs := make([]rune, len(b))
	for i, c := range b {
		rs[i] = rune(c)
	}
	return string(rs)
}

// normalizeZone strips a leading zero from a zone number ("05" → "5"), leaving
// non-numeric labels untouched.
func normalizeZone(s string) string {
	s = strings.TrimSpace(s)
	if n, err := strconv.Atoi(s); err == nil {
		return strconv.Itoa(n)
	}
	return s
}

// zoneFromCalcul extracts the zone number from a Zone_calcul code
// ("L7502.4.05" → "5"). Returns "" when the shape is unexpected.
func zoneFromCalcul(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ".")
	return normalizeZone(parts[len(parts)-1])
}

// parsePiecesHomogene maps a "nombre_pieces_homogene" label to (pieces,
// openEnded). "Appart 1P".."Appart 3P" → 1..3; "Appart 4P+" → 4 open-ended.
func parsePiecesHomogene(s string) (int, bool, bool) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "Appart ") {
		return 0, false, false
	}
	tok := strings.TrimSpace(strings.TrimPrefix(s, "Appart "))
	open := strings.HasSuffix(tok, "+")
	tok = strings.TrimSuffix(strings.TrimSuffix(tok, "+"), "P")
	n, err := strconv.Atoi(strings.TrimSpace(tok))
	if err != nil || n < 1 {
		return 0, false, false
	}
	return n, open, true
}

// parseFrenchFloat parses a French-formatted decimal ("16,4", "1 234,5"). ok is
// false for an empty/unparseable cell.
func parseFrenchFloat(s string) (float64, bool) {
	s = strings.Map(func(r rune) rune {
		switch r {
		case ' ', ' ', ' ':
			return -1
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// validate gates a freshly-built artifact: it must parse and carry at least one
// agglo with zones and rents.
func validate(r io.Reader) error {
	var p processed
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return fmt.Errorf("oll: validate: %w", err)
	}
	if len(p.Agglos) == 0 {
		return errors.New("oll: validated artifact has no agglos")
	}
	for _, a := range p.Agglos {
		if len(a.Zones) == 0 || len(a.Rents) == 0 {
			return fmt.Errorf("oll: agglo %q has no zones/rents", a.Code)
		}
	}
	return nil
}
