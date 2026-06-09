package ips_ecoles

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/frnorm"
	"github.com/bpineau/gazetteer/helpers/stats"
)

// rawCSVName is the datadir filename for the upstream raw input.
const rawCSVName = "ips_ecoles.raw.csv"

// rawCSVURL is the DEPP "Indices de position sociale dans les écoles" CSV
// published on data.gouv.fr (stable resource id, redirected to the dated
// static path). The file carries three rentrées (2022-2023, 2023-2024,
// 2024-2025) stacked; the transform keeps only dataYearLabel.
const rawCSVURL = "https://www.data.gouv.fr/fr/datasets/r/896c2e97-6a64-4521-bcab-b5b0d3cf7065"

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "DEPP — Indices de position sociale (IPS) des écoles, rentrée 2024-2025"

// dataYearLabel selects the rentrée to retain from the multi-year CSV and is
// echoed into Meta. Bump it (and metaSource) when a newer vintage ships.
const dataYearLabel = "2024-2025"

// metaNote documents the aggregation in the artifact.
const metaNote = "Per-commune median IPS over the écoles primaires open at rentrée 2024-2025. " +
	"UNWEIGHTED median (upstream CSV does not publish per-school effectifs). " +
	"Paris/Lyon/Marseille publish per-arrondissement rows — do NOT fold to parent commune; " +
	"this is the only commune-level source in the gazetteer that yields fine-grained PLM granularity."

// Upstream column headers (semicolon-separated, UTF-8 BOM).
const (
	colYear  = "Rentrée scolaire"
	colINSEE = "Code INSEE de la commune"
	colIPS   = "IPS"
)

// transform rebuilds the per-commune IPS artifact from the upstream DEPP
// CSV. For each row of the selected rentrée whose IPS is numeric (rows with
// a non-significant "NS" IPS are skipped), the per-school IPS is bucketed by
// the commune's 5-digit INSEE. Each commune's entry then carries the
// UNWEIGHTED median, min and max over its schools and the school count. The
// result is written as GZIPPED JSON (the embedded artifact is .json.gz).
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawCSVName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(rc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("ips_ecoles: read header: %w", err)
	}
	yearCol, inseeCol, ipsCol := -1, -1, -1
	for i, h := range header {
		switch strings.TrimSpace(h) {
		case colYear:
			yearCol = i
		case colINSEE:
			inseeCol = i
		case colIPS:
			ipsCol = i
		}
	}
	if yearCol < 0 || inseeCol < 0 || ipsCol < 0 {
		return fmt.Errorf("ips_ecoles: header missing %q/%q/%q columns: %v",
			colYear, colINSEE, colIPS, header)
	}

	// values accumulates the per-school IPS list keyed by commune INSEE.
	values := map[string][]float64{}
	schools := 0
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("ips_ecoles: read row: %w", err)
		}
		if ipsCol >= len(rec) || inseeCol >= len(rec) || yearCol >= len(rec) {
			continue
		}
		if strings.TrimSpace(rec[yearCol]) != dataYearLabel {
			continue
		}
		insee := strings.TrimSpace(rec[inseeCol])
		if insee == "" {
			continue
		}
		ips, ok := frnorm.ParseFRFloat(rec[ipsCol])
		if !ok {
			// Non-significant ("NS") or blank IPS: school is dropped.
			continue
		}
		values[insee] = append(values[insee], ips)
		schools++
	}
	if len(values) == 0 {
		return fmt.Errorf("ips_ecoles: transform produced no communes (no %q rows?)", dataYearLabel)
	}

	idx := Index{
		Meta: Meta{
			Source:           metaSource,
			DataYearLabel:    dataYearLabel,
			RowCountCommunes: len(values),
			RowCountSchools:  schools,
			Note:             metaNote,
		},
		Communes: make(map[string]Entry, len(values)),
	}
	for insee, xs := range values {
		idx.Communes[insee] = Entry{
			IPSMedian:   stats.Median(xs),
			IPSMin:      minOf(xs),
			IPSMax:      maxOf(xs),
			SchoolCount: len(xs),
		}
	}

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("ips_ecoles: encode json: %w", err)
	}
	return nil
}

// validate gates publication: the rebuilt gzipped artifact must gunzip,
// parse, and be non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("ips_ecoles: validated artifact has no communes")
	}
	return nil
}

func minOf(xs []float64) float64 {
	m := xs[0]
	for _, v := range xs[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func maxOf(xs []float64) float64 {
	m := xs[0]
	for _, v := range xs[1:] {
		if v > m {
			m = v
		}
	}
	return m
}
