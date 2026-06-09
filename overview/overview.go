package overview

import (
	"math"
	"sort"

	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/geodist"
	"github.com/bpineau/gazetteer/helpers/stats"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dvfagg"
	"github.com/bpineau/gazetteer/sources/encadrement"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/osm"
	"github.com/bpineau/gazetteer/sources/qpv"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
	"github.com/bpineau/gazetteer/sources/zonageabc"
	"github.com/bpineau/gazetteer/sources/zonetendue"
)

// parisLat / parisLon are the WGS-84 centroid of Paris used for
// DistanceParisKm. Notre-Dame de Paris (~geometric centre of the city).
const (
	parisLat = 48.8566
	parisLon = 2.3522
)

// transitRadiusM is the haversine search radius for TransitLines: stations
// within this distance of the commune centroid are considered "served".
const transitRadiusM = 2500.0

// maxTransitLines caps the TransitLines slice to avoid noise on hubs like
// Châtelet that sit at the centroid of several arrondissements.
const maxTransitLines = 6

// CommuneOverview is the per-commune data row produced by the Build join.
// It merges DVF price aggregates with rent, socio-economic and regulatory
// data from the embedded gazetteer sources.
type CommuneOverview struct {
	// Identity
	INSEE string `json:"insee"`
	Name  string `json:"name"`
	Dept  string `json:"dept"`

	// Price (DVF, all apartments, 3-year window)
	PriceMedianEURM2      float64 `json:"price_median_eur_m2"`
	PriceP25EURM2         float64 `json:"price_p25_eur_m2"`
	PriceP75EURM2         float64 `json:"price_p75_eur_m2"`
	PriceMedianSmallEURM2 float64 `json:"price_median_small_eur_m2"`
	PriceN                int     `json:"price_n"`
	PriceNSmall           int     `json:"price_n_small"`

	// Rent (carte des loyers, apt 1-2 pièces, HC)
	RentMarketEURM2HC float64  `json:"rent_market_eur_m2_hc"`
	RentCapEURM2HC    *float64 `json:"rent_cap_eur_m2_hc,omitempty"`
	Encadree          bool     `json:"encadree"`

	// Socio-economic
	DelinquanceLevel string   `json:"delinquance_level"`
	VacancyPct       *float64 `json:"vacancy_pct,omitempty"`
	TFPBPct          *float64 `json:"tfpb_pct,omitempty"`
	QPV              bool     `json:"qpv"`
	IncomeMedianEUR  *int     `json:"income_median_eur,omitempty"`

	// Regulatory / location
	ZonageABC  string `json:"zonage_abc"`
	ZoneTendue string `json:"zone_tendue"`

	// Geo (embedded-only, computed from commune centroid)
	DistanceParisKm float64  `json:"distance_paris_km"`
	TransitLines    []string `json:"transit_lines"`
}

// Options configures a Build call.
type Options struct {
	// DataDir is the gazetteer data directory (empty = embedded only).
	DataDir string

	// Depts restricts output to the given department codes (e.g. ["75","95"]).
	// Empty means all departments present in the dvfagg source.
	Depts []string
}

// Build joins all embedded gazetteer sources for the communes that have DVF
// price data. It returns one CommuneOverview per matching commune, in the
// order produced by dvfagg.Codes() (sorted by INSEE). Only sources that are
// embedded are used; missing data for a particular source is a graceful zero /
// nil for that field.
func Build(o Options) ([]CommuneOverview, error) {
	dv, err := dvfagg.Load(o.DataDir)
	if err != nil {
		return nil, err
	}
	cl, err := carteloyers.Load(o.DataDir)
	if err != nil {
		return nil, err
	}

	// Sources where errors are non-fatal (embedded-only graceful degradation).
	dl, _ := delinquance.Load(o.DataDir)
	va, _ := vacance.Load(o.DataDir)
	tf, _ := taxefonciere.Load(o.DataDir)
	qp, _ := qpv.Load(o.DataDir)
	fi, _ := filosofi.Load(o.DataDir)
	za, _ := zonageabc.Load(o.DataDir)
	zt, _ := zonetendue.Load(o.DataDir)
	enc, _ := encadrement.Load(o.DataDir)

	// Communes table: used for name resolution and geo fields.
	com, err := communes.Default()
	if err != nil {
		return nil, err
	}

	// OSM embedded station catalog — loaded once, offline, no network call.
	// On failure (should never happen with the embedded gz) transit is silently
	// skipped so the other fields are unaffected.
	osmCat, _ := osm.Load(o.DataDir)

	deptSet := map[string]bool{}
	for _, d := range o.Depts {
		deptSet[d] = true
	}

	var out []CommuneOverview
	for _, insee := range dv.Codes() {
		price, _ := dv.Lookup(insee)
		if len(deptSet) > 0 && !deptSet[price.Dept] {
			continue
		}
		row := CommuneOverview{
			INSEE:                 insee,
			Dept:                  price.Dept,
			PriceMedianEURM2:      price.PriceMedianEURM2,
			PriceP25EURM2:         price.PriceP25EURM2,
			PriceP75EURM2:         price.PriceP75EURM2,
			PriceMedianSmallEURM2: price.PriceMedianSmallEURM2,
			PriceN:                price.N,
			PriceNSmall:           price.NSmall,
		}

		// Commune name + geo fields derived from the centroid.
		if c, ok := com.Lookup(insee); ok {
			row.Name = c.Name
			row.DistanceParisKm = stats.Round(geodist.KmBetween(c.Lat, c.Lon, parisLat, parisLon), 1)
			row.TransitLines = transitLinesNear(osmCat, c.Lat, c.Lon)
		}

		// Carte des loyers: apt 1-2 pièces, HC.
		if r, ok := cl.Lookup(insee, carteloyers.TypologyApt12); ok {
			row.RentMarketEURM2HC = r.HCEURPerM2()
		}

		// Encadrement representative T2 majoré cap.
		if cap, ok := RepresentativeT2Majore(enc, insee); ok {
			row.Encadree = true
			row.RentCapEURM2HC = &cap
		}

		// Délinquance level.
		row.DelinquanceLevel = dl.Level(insee).String()

		// Vacance.
		if e, ok := va.Lookup(insee); ok {
			v := e.VacancyRatePct
			row.VacancyPct = &v
		}

		// Taxe foncière (TFPB rate from V2 index).
		if tf != nil && tf.V2 != nil {
			if e, _, ok := tf.V2.LookupV2(insee); ok {
				v := e.TFPBPct
				row.TFPBPct = &v
			}
		}

		// QPV.
		row.QPV = qp.HasQPV(insee)

		// Filosofi (income median).
		if e, ok := fi.Lookup(insee); ok {
			v := e.MedianEUR
			row.IncomeMedianEUR = &v
		}

		// Zonage ABC.
		if z, ok := za.Lookup(insee); ok {
			row.ZonageABC = string(z)
		}

		// Zone tendue.
		if e, ok := zt.Lookup(insee); ok {
			row.ZoneTendue = string(e.Tier)
		}

		out = append(out, row)
	}
	return out, nil
}

// transitTypeOrder returns a sort key for transit types so Metro < RER <
// Transilien < Tram < Train in the output slice. Lower = higher priority.
func transitTypeOrder(tt osm.TransitType) int {
	switch tt {
	case osm.TransitTypeMetro:
		return 0
	case osm.TransitTypeRER:
		return 1
	case osm.TransitTypeTransilien:
		return 2
	case osm.TransitTypeTram:
		return 3
	case osm.TransitTypeTrain:
		return 4
	}
	return 5
}

// transitLinesNear collects unique line labels (e.g. "Métro 5", "RER A",
// "T1") for all stations within transitRadiusM of (lat, lon). The result is
// capped at maxTransitLines, de-duplicated, and sorted by mode priority then
// alphabetically within the mode. Returns nil when the catalog is nil or no
// stations are nearby.
func transitLinesNear(cat *osm.Catalog, lat, lon float64) []string {
	if cat == nil || len(cat.Stations) == 0 {
		return nil
	}
	if lat == 0 && lon == 0 {
		return nil
	}

	// Collect (label → TransitType) so we can sort by priority.
	type entry struct {
		label string
		order int
	}
	seen := make(map[string]struct{}, 8)
	var entries []entry

	// Degree-space pre-reject before the trig-heavy haversine: Build joins
	// ~9k communes against ~9k stations and most pairs are nowhere near
	// each other. The 10 % slack means the box can only over-accept; the
	// exact metric test below still decides.
	dLat := transitRadiusM / 111_320.0 * 1.1
	dLon := dLat / math.Max(math.Cos(lat*math.Pi/180), 0.01)

	for i := range cat.Stations {
		st := &cat.Stations[i]
		if math.Abs(st.Lat-lat) > dLat || math.Abs(st.Lon-lon) > dLon {
			continue
		}
		d := geodist.MetersBetween(lat, lon, st.Lat, st.Lon)
		if d > transitRadiusM {
			continue
		}
		// Stations without any line refs are skipped: a type-only label
		// ("Train") adds little value when nearby stations already carry
		// specific refs, and is noise when they don't.
		for _, ref := range st.Lines {
			lbl := osm.LineLabel(st.Type, ref)
			if _, dup := seen[lbl]; !dup {
				seen[lbl] = struct{}{}
				entries = append(entries, entry{lbl, transitTypeOrder(st.Type)})
			}
		}
	}

	if len(entries) == 0 {
		return nil
	}

	// Sort: by mode priority first, then alphabetically within the mode.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].order != entries[j].order {
			return entries[i].order < entries[j].order
		}
		return entries[i].label < entries[j].label
	})

	// Cap and extract labels.
	if len(entries) > maxTransitLines {
		entries = entries[:maxTransitLines]
	}
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.label
	}
	return out
}
