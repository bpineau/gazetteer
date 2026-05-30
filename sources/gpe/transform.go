package gpe

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/dataset"
)

// rawName is the datadir filename for the upstream raw input.
const rawName = "gpe_stations.raw.json"

// rawURL is the Société du Grand Paris station-location dataset, exported as
// JSON (WGS84) from the Île-de-France Smart Services OpenDataSoft portal.
const rawURL = "https://data.smartidf.services/api/explore/v2.1/catalog/datasets/point-de-localisation-des-gares-du-grand-paris-express/exports/json?limit=-1"

const metaSource = "data.smartidf.services — point-de-localisation-des-gares-du-grand-paris-express (Société du Grand Paris)"

const metaNote = "Future Grand Paris Express stations (lines 14 ext / 15 / 16 / 17 / 18). " +
	"line = verbatim SGP label (a single line, or several at an interchange). No opening year: " +
	"the calendar shifts and one line label spans sections opening years apart."

// odsRecord is the subset of the OpenDataSoft export we consume.
type odsRecord struct {
	Code    string `json:"code"`
	Libelle string `json:"libelle"`
	Ligne   string `json:"ligne"`
	GeoPt   struct {
		Lon float64 `json:"lon"`
		Lat float64 `json:"lat"`
	} `json:"geo_point_2d"`
}

// transform rebuilds the processed GPE catalog from the upstream ODS export:
// it keeps every station with a code, a line and valid coordinates, and
// writes a code-sorted JSON catalog.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	body, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("gpe: read raw: %w", err)
	}
	var recs []odsRecord
	if err := json.Unmarshal(body, &recs); err != nil {
		return fmt.Errorf("gpe: parse raw json: %w", err)
	}

	idx := Index{
		Meta: Meta{
			Source:       metaSource,
			Note:         metaNote,
			DownloadedAt: time.Now().UTC().Format("2006-01-02"),
		},
		Stations: make([]stationRec, 0, len(recs)),
	}
	for _, r := range recs {
		code := strings.TrimSpace(r.Code)
		line := strings.TrimSpace(r.Ligne)
		// Drop records with no code/line, or the null-island (0,0) sentinel
		// that marks a missing geometry. A both-zero check (not ||) so a real
		// coordinate is never dropped for one axis being 0 — moot for IDF
		// (~48.8N, 2.3E) but intent-revealing if templated elsewhere.
		if code == "" || line == "" || (r.GeoPt.Lat == 0 && r.GeoPt.Lon == 0) {
			continue
		}
		idx.Stations = append(idx.Stations, stationRec{
			Code: code,
			Name: strings.TrimSpace(r.Libelle),
			Line: line,
			Lat:  r.GeoPt.Lat,
			Lon:  r.GeoPt.Lon,
		})
	}
	if len(idx.Stations) == 0 {
		return errors.New("gpe: transform produced no stations")
	}
	sortStations(idx.Stations)
	idx.Meta.StationCount = len(idx.Stations)

	enc := json.NewEncoder(dst)
	enc.SetIndent("", " ")
	return enc.Encode(idx)
}

// validate gates publication: the rebuilt catalog must parse and be non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("gpe: validated catalog has no stations")
	}
	return nil
}
