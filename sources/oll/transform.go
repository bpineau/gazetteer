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
	"unicode/utf8"

	"github.com/bpineau/gazetteer/dataset"
)

// aggloSpec is one OLL observatory perimeter the Source ingests. Each publishes
// a per-agglo ZIP at the observatory site bundling the observed-rent table
// (Base_OP_<year>_<code>.csv) and the commune→zone map (<code>Zonage<year>.csv),
// ";"-separated, in a per-agglo-varying encoding (UTF-8 or ISO-8859-1) and with
// per-agglo-varying column names — handled by decodeText and firstCol.
type aggloSpec struct {
	code string
	name string
	year int
}

func (s aggloSpec) rawName() string {
	return "oll_" + strings.ToLower(s.code) + ".raw.zip"
}

func (s aggloSpec) url() string {
	return fmt.Sprintf("https://www.observatoires-des-loyers.org/datagouv/%d/Base_OP_%d_%s.zip", s.year, s.year, s.code)
}

// aggloSpecs is the curated set of OLL agglomerations ingested into the embedded
// snapshot. Each was verified to use the join convention this transform relies
// on (the zonage Zone matches the last segment of the rents' Zone_calcul code);
// agglomerations with an incompatible multi-level zone layout (e.g. Paris
// intra-muros L7501) are deliberately excluded — Paris rents are served by
// encadrement. Extend this list (and re-run `refresh --go-embed-update`) to
// cover more perimeters.
var aggloSpecs = []aggloSpec{
	{"L7502", "Agglomération parisienne hors Paris", 2024},
	{"L6900", "Agglomération de Lyon", 2024},
	{"L5900", "Agglomération de Lille", 2024},
	{"L3100", "Agglomération de Toulouse", 2025},
	{"L3300", "Agglomération de Bordeaux", 2024},
	{"L4400", "Agglomération de Nantes", 2024},
	{"L6700", "Eurométropole de Strasbourg", 2024},
	{"L3400", "Agglomération de Montpellier", 2025},
	{"L3800", "Agglomération de Grenoble", 2024},
	{"L3500", "Agglomération de Rennes", 2024},
	{"L0600", "Département des Alpes-Maritimes", 2024},
	{"L6300", "Agglomération de Clermont-Ferrand", 2024},
	{"L5400", "Agglomération de Nancy", 2024},
	{"L3700", "Agglomération de Tours", 2024},
	{"L1700", "Agglomération de La Rochelle", 2024},
	{"L2500", "Agglomération de Besançon", 2024},
	{"L6400", "Pays Basque et Sud Landes", 2024},
	{"L9740", "Île de La Réunion", 2024},
}

// transform rebuilds the embedded snapshot: for each configured agglomeration it
// joins the commune→zone map to the observed median rents per (zone, rooms)
// bucket.
//
// Each agglo is an INDEPENDENT upstream archive, and the per-agglo CSV layouts
// are heterogeneous (column names, encodings, a few malformed headers). So a
// single agglo that is absent or unparseable is skipped rather than failing the
// whole rebuild — one bad archive must not sink the national snapshot. The
// build still fails loudly if it ends up with no agglos at all (a systematic
// breakage). Per-agglo correctness is covered by the offline golden test on
// parseAgglo.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	var out processed
	for _, spec := range aggloSpecs {
		rc, err := raw.Open(spec.rawName())
		if err != nil {
			continue // raw not present for this agglo
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			continue
		}
		a, err := parseAgglo(spec, buf)
		if err != nil || a == nil {
			continue // unparseable / incompatible layout — skip this agglo
		}
		out.Agglos = append(out.Agglos, *a)
	}
	if len(out.Agglos) == 0 {
		return errors.New("oll: transform produced no agglos")
	}
	return json.NewEncoder(dst).Encode(out)
}

// parseAgglo builds one aggloData from its ZIP archive bytes. It prunes the rent
// cells to those whose zone exists in the commune→zone map, so an un-joinable
// cell is never emitted; an agglo left with no joinable rents (an incompatible
// zone layout) returns (nil, nil) and is skipped.
func parseAgglo(spec aggloSpec, zipBytes []byte) (*aggloData, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}
	zonesCSV, err := readZipMember(zr, "zonage")
	if err != nil {
		return nil, fmt.Errorf("zonage member: %w", err)
	}
	rentsCSV, err := readZipMember(zr, "base_op")
	if err != nil {
		return nil, fmt.Errorf("rents member: %w", err)
	}
	zones, err := parseZonage(zonesCSV)
	if err != nil {
		return nil, fmt.Errorf("parse zonage: %w", err)
	}
	rents, err := parseRents(rentsCSV)
	if err != nil {
		return nil, fmt.Errorf("parse rents: %w", err)
	}

	zoneSet := make(map[string]bool, len(zones))
	for _, z := range zones {
		zoneSet[z.Zone] = true
	}
	pruned := make([]rentRow, 0, len(rents))
	for _, r := range rents {
		if zoneSet[r.Zone] {
			pruned = append(pruned, r)
		}
	}
	if len(zones) == 0 || len(pruned) == 0 {
		return nil, nil // incompatible / empty — skip this agglo
	}
	return &aggloData{Code: spec.code, Name: spec.name, Year: spec.year, Zones: zones, Rents: pruned}, nil
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
	return decodeText(b), nil
}

// parseZonage reads the commune→zone map (cols Commune;Lib_com;Iris;Zone;Lib_zone),
// keeping one row per commune (the IRIS column is unused at this granularity).
func parseZonage(text string) ([]zoneRow, error) {
	recs, err := readCSV(text)
	if err != nil {
		return nil, err
	}
	col := headerIndex(recs[0])
	// The INSEE column is named differently across agglos: "Commune", "INSEE",
	// "CODE_INSEE", "code_commune".
	ci := firstCol(col, "commune", "insee", "code_insee", "code_commune")
	czone, clib := hcol(col, "zone"), hcol(col, "lib_zone")
	if ci < 0 || czone < 0 {
		return nil, fmt.Errorf("zonage missing INSEE/Zone columns (have %v)", recs[0])
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

// firstCol returns the index of the first of names present in the header, or -1.
func firstCol(m map[string]int, names ...string) int {
	for _, n := range names {
		if i := hcol(m, n); i >= 0 {
			return i
		}
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

// decodeText decodes a CSV member to a UTF-8 string. The per-agglo archives are
// not uniformly encoded: some are UTF-8 (occasionally BOM-prefixed), others
// ISO-8859-1. It strips a UTF-8 BOM, then keeps valid UTF-8 as-is and otherwise
// falls back to ISO-8859-1 (each byte its own code point). The fields the
// transform retains are ASCII, so this only matters for header detection and
// free-text labels.
func decodeText(b []byte) string {
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	if utf8.Valid(b) {
		return string(b)
	}
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
