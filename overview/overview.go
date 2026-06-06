package overview

import (
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/sources/anct"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dvfagg"
	"github.com/bpineau/gazetteer/sources/encadrement"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/qpv"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
	"github.com/bpineau/gazetteer/sources/zonageabc"
	"github.com/bpineau/gazetteer/sources/zonetendue"
)

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
	_, _ = anct.Load(o.DataDir) // loaded but not yet surfaced as fields
	fi, _ := filosofi.Load(o.DataDir)
	za, _ := zonageabc.Load(o.DataDir)
	zt, _ := zonetendue.Load(o.DataDir)
	enc, _ := encadrement.Load(o.DataDir)

	// Communes table: used for name resolution.
	com, err := communes.Default()
	if err != nil {
		return nil, err
	}

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

		// Commune name.
		if c, ok := com.Lookup(insee); ok {
			row.Name = c.Name
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
