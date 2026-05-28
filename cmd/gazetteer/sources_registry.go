package main

import (
	"fmt"
	"sort"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/ademe"
	"github.com/bpineau/gazetteer/sources/anct"
	"github.com/bpineau/gazetteer/sources/bdnb"
	"github.com/bpineau/gazetteer/sources/bpe"
	"github.com/bpineau/gazetteer/sources/cadastre"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/cartofriches"
	"github.com/bpineau/gazetteer/sources/chomage"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dpedist"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/education"
	"github.com/bpineau/gazetteer/sources/encadrement"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/georisques"
	"github.com/bpineau/gazetteer/sources/ips_ecoles"
	"github.com/bpineau/gazetteer/sources/locservice"
	gzosm "github.com/bpineau/gazetteer/sources/osm"
	"github.com/bpineau/gazetteer/sources/qpv"
	"github.com/bpineau/gazetteer/sources/rpls"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
	"github.com/bpineau/gazetteer/sources/vacance_logements"
	"github.com/bpineau/gazetteer/sources/zonageabc"
	"github.com/bpineau/gazetteer/sources/zonetendue"
)

// sourceCatalog is the CLI-side enumeration of every gazetteer.Source
// the binary knows how to instantiate. Centralised here so query /
// appraise / sources-list share the same definition.
//
// Each entry is a factory closure that builds a configured Source from
// the runtimeDeps bundle. Errors returned from the factory are surfaced
// to the operator (rare — typically only OSM, which needs an offline
// catalog the CLI doesn't ship).
type sourceFactory struct {
	Name    string
	Build   func(deps *runtimeDeps) (gazetteer.Source, error)
	Default bool // included when --source is unset
}

// sourceCatalog returns the registry of source factories the CLI
// exposes. Order groups Sources by their primary thematic axis
// (building / market / commune-level / transit). The function returns
// a fresh slice on each call so callers can mutate / filter it without
// affecting peers.
//
// Defaults: every source EXCEPT osm_transit (it needs an offline
// catalog the CLI doesn't yet wire; --source osm_transit opts in and
// surfaces the missing-catalog error explicitly).
func sourceCatalog() []sourceFactory {
	return []sourceFactory{
		{
			Name:    dvf.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return dvf.NewSource(dvf.Options{HTTP: d.HTTP, Geocoder: d.BAN, Communes: d.Communes})
			},
		},
		{
			Name:    ademe.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return ademe.NewSource(ademe.Options{Geocoder: d.BAN}), nil
			},
		},
		{
			// BDNB opt-in only: the public PostgREST endpoint enforces a
			// rolling 10 000-request budget per API key and routinely
			// returns HTTP 429 to anonymous traffic, which would burn
			// the CLI's wall-clock on retries for a result most
			// interactive callers don't strictly need. Use
			// `--source bdnb` (or include it explicitly in a CSV list)
			// when the building-attributes signal matters.
			Name:    bdnb.Name,
			Default: false,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return bdnb.NewSource(bdnb.Options{Geocoder: d.BAN}), nil
			},
		},
		{
			Name:    cadastre.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return cadastre.NewSource(cadastre.Options{Geocoder: d.BAN}), nil
			},
		},
		{
			Name:    georisques.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return georisques.NewSource(georisques.Options{Geocoder: d.BAN}), nil
			},
		},
		{
			Name:    locservice.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return locservice.NewSource(locservice.Options{Geocoder: d.BAN}), nil
			},
		},
		{
			// OSM transit needs an offline catalog. Opt-in via --source.
			// When asked but no catalog is wired, Query returns
			// ErrNoCatalog and the framework records failed_transient.
			Name:    gzosm.Name,
			Default: false,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return gzosm.NewSource(gzosm.Options{}), nil
			},
		},
		{
			Name:    carteloyers.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return carteloyers.NewSource(carteloyers.Options{}), nil
			},
		},
		{
			Name:    encadrement.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return encadrement.NewSource(encadrement.Options{}), nil
			},
		},
		{
			Name:    filosofi.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return filosofi.NewSource(filosofi.Options{}), nil
			},
		},
		{
			Name:    taxefonciere.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return taxefonciere.NewSource(taxefonciere.Options{}), nil
			},
		},
		{
			Name:    vacance.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return vacance.NewSource(vacance.Options{}), nil
			},
		},
		{
			Name:    anct.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return anct.NewSource(anct.Options{}), nil
			},
		},
		{
			Name:    cartofriches.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return cartofriches.NewSource(cartofriches.Options{}), nil
			},
		},
		{
			Name:    chomage.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return chomage.NewSource(chomage.Options{}), nil
			},
		},
		{
			Name:    bpe.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return bpe.NewSource(bpe.Options{}), nil
			},
		},
		{
			Name:    dpedist.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return dpedist.NewSource(dpedist.Options{}), nil
			},
		},
		{
			Name:    delinquance.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return delinquance.NewSource(delinquance.Options{}), nil
			},
		},
		{
			Name:    education.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return education.NewSource(education.Options{}), nil
			},
		},
		{
			Name:    qpv.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return qpv.NewSource(qpv.Options{}), nil
			},
		},
		{
			Name:    rpls.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return rpls.NewSource(rpls.Options{}), nil
			},
		},
		{
			Name:    vacance_logements.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return vacance_logements.NewSource(vacance_logements.Options{}), nil
			},
		},
		{
			Name:    ips_ecoles.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return ips_ecoles.NewSource(ips_ecoles.Options{}), nil
			},
		},
		{
			Name:    zonageabc.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return zonageabc.NewSource(zonageabc.Options{}), nil
			},
		},
		{
			Name:    zonetendue.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return zonetendue.NewSource(zonetendue.Options{}), nil
			},
		},
	}
}

// allSourceNames returns the catalog's source names in registration
// order. Used by `sources list` and the `--source` flag's help text.
func allSourceNames() []string {
	cat := sourceCatalog()
	out := make([]string, len(cat))
	for i, f := range cat {
		out[i] = f.Name
	}
	return out
}

// resolveSources filters the catalog by the comma-separated names in
// `selected` (empty = use Default-tagged entries). Returns the
// instantiated Source slice ready to feed into a gazetteer.Builder.
// Unknown names yield an error listing what's available.
func resolveSources(deps *runtimeDeps, selected []string) ([]gazetteer.Source, error) {
	cat := sourceCatalog()
	byName := make(map[string]sourceFactory, len(cat))
	for _, f := range cat {
		byName[f.Name] = f
	}

	var picks []sourceFactory
	if len(selected) == 0 {
		for _, f := range cat {
			if f.Default {
				picks = append(picks, f)
			}
		}
	} else {
		for _, name := range selected {
			f, ok := byName[name]
			if !ok {
				avail := allSourceNames()
				sort.Strings(avail)
				return nil, fmt.Errorf("unknown source %q (available: %v)", name, avail)
			}
			picks = append(picks, f)
		}
	}

	out := make([]gazetteer.Source, 0, len(picks))
	for _, f := range picks {
		s, err := f.Build(deps)
		if err != nil {
			return nil, fmt.Errorf("build source %q: %w", f.Name, err)
		}
		out = append(out, s)
	}
	return out, nil
}
