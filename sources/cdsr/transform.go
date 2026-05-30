package cdsr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// Raw input (datadir basename) and upstream URL. The region publishes the CDSR
// dataset on its Opendatasoft portal; the exports/json endpoint returns the full
// record set as a flat JSON array.
const (
	rawName = "cdsr.raw.json"
	rawURL  = "https://data.iledefrance.fr/api/explore/v2.1/catalog/datasets/cdsr-coproprietes-en-difficulte-soutenues-par-la-region-travaux-economie/exports/json"
)

// exportRow is the upstream record shape (a superset; only the fields below are
// kept). latitude/longitude arrive as strings.
type exportRow struct {
	Residence string `json:"residence_et_adresse"`
	Name      string `json:"nom_copropriete"`
	Address   string `json:"adresse_copropriete"`
	Commune   string `json:"commune"`
	Latitude  string `json:"latitude"`
	Longitude string `json:"longitude"`
	Lots      int    `json:"nombre_de_lots"`
	DateVote  string `json:"date_vote_label"`
}

// transform rebuilds cdsr.json from the Opendatasoft export: one compact Copro
// per labelled condominium, dropping every field the Source does not use.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	var in []exportRow
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&in); err != nil {
		return fmt.Errorf("cdsr: decode export: %w", err)
	}

	out := make([]Copro, 0, len(in))
	for i, r := range in {
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(r.Latitude), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(r.Longitude), 64)
		if err1 != nil || err2 != nil || (lat == 0 && lon == 0) {
			return fmt.Errorf("cdsr: row %d (%s): bad coordinates (lat=%q lon=%q)", i, r.Commune, r.Latitude, r.Longitude)
		}
		out = append(out, Copro{
			Name:      coproName(r),
			Address:   strings.TrimSpace(r.Address),
			Commune:   strings.TrimSpace(r.Commune),
			Lat:       lat,
			Lon:       lon,
			Lots:      r.Lots,
			LabelYear: yearOf(r.DateVote),
		})
	}
	if len(out) == 0 {
		return errors.New("cdsr: transform produced no rows")
	}
	return json.NewEncoder(dst).Encode(out)
}

// coproName picks a display name: the copro name when present, else the
// "residence_et_adresse" label, else the bare address.
func coproName(r exportRow) string {
	if n := strings.TrimSpace(r.Name); n != "" {
		return n
	}
	if res := strings.TrimSpace(r.Residence); res != "" {
		return res
	}
	return strings.TrimSpace(r.Address)
}

// yearOf extracts the 4-digit year from an ISO date ("2016-11-16" → 2016).
// Returns 0 when the input is empty or malformed.
func yearOf(isoDate string) int {
	s := strings.TrimSpace(isoDate)
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil {
		return 0
	}
	return y
}

// validate gates a freshly-built artifact: it must parse as a non-empty Copro
// array with usable coordinates on every row.
func validate(r io.Reader) error {
	var copros []Copro
	if err := json.NewDecoder(r).Decode(&copros); err != nil {
		return fmt.Errorf("cdsr: validate: %w", err)
	}
	if len(copros) == 0 {
		return errors.New("cdsr: validated artifact is empty")
	}
	for i, c := range copros {
		if c.Lat == 0 && c.Lon == 0 {
			return fmt.Errorf("cdsr: row %d (%s) has no coordinates", i, c.Commune)
		}
	}
	return nil
}
