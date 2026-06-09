package nuisances

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// Raw input (datadir basename) and upstream URL. The region publishes the 500 m
// cumulative-nuisance grid on its Opendatasoft portal; the CSV export below
// keeps only the cell centre, the nuisance count and the point-noir flag.
const (
	rawName = "nuisances.raw.csv"
	rawURL  = "https://data.iledefrance.fr/api/explore/v2.1/catalog/datasets/" +
		"cumul-de-nuisances-environnementales-grille-regionale-au-pas-de-500m-dile-de-fra/" +
		"exports/csv?select=geo_point_2d,nb_nuis_po,pne"
)

// transform rebuilds the gzipped grid snapshot from the Opendatasoft CSV export.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	r := csv.NewReader(dataset.BOMReader(rc))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	r.LazyQuotes = true

	header, err := r.Read()
	if err != nil {
		return fmt.Errorf("nuisances: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	ciGeo, ok1 := col["geo_point_2d"]
	ciNuis, ok2 := col["nb_nuis_po"]
	ciPNE, ok3 := col["pne"]
	if !ok1 || !ok2 || !ok3 {
		return fmt.Errorf("nuisances: missing columns (have %v)", header)
	}

	var cells []cell
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("nuisances: read row: %w", err)
		}
		lat, lon, ok := parseLatLon(at(rec, ciGeo))
		if !ok {
			continue
		}
		// Drop a row with an unparseable count rather than defaulting it to 0,
		// which would mislabel an exposed cell as "calme" at high confidence.
		nuis, err := strconv.Atoi(strings.TrimSpace(at(rec, ciNuis)))
		if err != nil || nuis < 0 {
			continue
		}
		cells = append(cells, cell{Lat: lat, Lon: lon, Nuis: nuis, PNE: at(rec, ciPNE) == "1"})
	}
	if len(cells) == 0 {
		return errors.New("nuisances: transform produced no cells")
	}
	// Deterministic order for byte-stable output.
	sort.Slice(cells, func(i, j int) bool {
		if cells[i].Lat != cells[j].Lat {
			return cells[i].Lat < cells[j].Lat
		}
		return cells[i].Lon < cells[j].Lon
	})

	return dataset.WriteGzJSON(dst, processed{Cells: cells})
}

// parseLatLon parses an Opendatasoft geo_point_2d field ("48.6084, 3.0169" =
// "lat, lon") into (lat, lon).
func parseLatLon(s string) (lat, lon float64, ok bool) {
	parts := strings.SplitN(strings.TrimSpace(s), ",", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	lat, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	lon, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	if err1 != nil || err2 != nil || (lat == 0 && lon == 0) {
		return 0, 0, false
	}
	return lat, lon, true
}

// at returns the i-th field of rec, or "" when out of range.
func at(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

// validate gates a freshly-built artifact: it must gunzip, parse, and carry a
// plausible number of grid cells.
func validate(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("nuisances: validate gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	var p processed
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		return fmt.Errorf("nuisances: validate decode: %w", err)
	}
	if len(p.Cells) < 40000 {
		return fmt.Errorf("nuisances: only %d cells, want ≥ 40000", len(p.Cells))
	}
	return nil
}
