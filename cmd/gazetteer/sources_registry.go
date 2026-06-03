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
	"github.com/bpineau/gazetteer/sources/catnat"
	"github.com/bpineau/gazetteer/sources/cdsr"
	"github.com/bpineau/gazetteer/sources/chomage"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dpedist"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/education"
	"github.com/bpineau/gazetteer/sources/encadrement"
	"github.com/bpineau/gazetteer/sources/filoiris"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/georisques"
	"github.com/bpineau/gazetteer/sources/gpe"
	"github.com/bpineau/gazetteer/sources/ips_ecoles"
	"github.com/bpineau/gazetteer/sources/iris"
	"github.com/bpineau/gazetteer/sources/links"
	"github.com/bpineau/gazetteer/sources/locservice"
	"github.com/bpineau/gazetteer/sources/logiris"
	"github.com/bpineau/gazetteer/sources/lovac"
	"github.com/bpineau/gazetteer/sources/nuisances"
	"github.com/bpineau/gazetteer/sources/oll"
	gzosm "github.com/bpineau/gazetteer/sources/osm"
	"github.com/bpineau/gazetteer/sources/qpv"
	"github.com/bpineau/gazetteer/sources/rnc"
	"github.com/bpineau/gazetteer/sources/rpls"
	"github.com/bpineau/gazetteer/sources/sitadel"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
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
// Defaults: every source EXCEPT bdnb (its public PostgREST endpoint
// throttles anonymous traffic, so it is opt-in via --source bdnb; see
// the per-entry note below).
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
			// OSM transit ships an embedded baseline catalog (overridable
			// from the datadir via `refresh osm_transit`) plus a live
			// Overpass fallback for points the catalog doesn't cover.
			Name:    gzosm.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return gzosm.NewSource(gzosm.Options{
					DataDir: d.DataDir,
					Fetcher: gzosm.NewHTTPOverpassFetcher(d.HTTP, ""),
				}), nil
			},
		},
		{
			Name:    carteloyers.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return carteloyers.NewSource(carteloyers.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    oll.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return oll.NewSource(oll.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    cdsr.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return cdsr.NewSource(cdsr.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    catnat.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return catnat.NewSource(catnat.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    nuisances.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return nuisances.NewSource(nuisances.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    iris.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return iris.NewSource(iris.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    encadrement.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return encadrement.NewSource(encadrement.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    filosofi.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return filosofi.NewSource(filosofi.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    filoiris.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return filoiris.NewSource(filoiris.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    logiris.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return logiris.NewSource(logiris.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    gpe.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return gpe.NewSource(gpe.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    taxefonciere.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return taxefonciere.NewSource(taxefonciere.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    lovac.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return lovac.NewSource(lovac.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    anct.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return anct.NewSource(anct.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    cartofriches.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return cartofriches.NewSource(cartofriches.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    chomage.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return chomage.NewSource(chomage.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    bpe.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return bpe.NewSource(bpe.Options{DataDir: d.DataDir}), nil
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
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return delinquance.NewSource(delinquance.Options{DataDir: d.DataDir}), nil
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
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return qpv.NewSource(qpv.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    rpls.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return rpls.NewSource(rpls.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    vacance.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return vacance.NewSource(vacance.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    sitadel.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return sitadel.NewSource(sitadel.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    rnc.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return rnc.NewSource(rnc.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    ips_ecoles.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return ips_ecoles.NewSource(ips_ecoles.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    zonageabc.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return zonageabc.NewSource(zonageabc.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    zonetendue.Name,
			Default: true,
			Build: func(d *runtimeDeps) (gazetteer.Source, error) {
				return zonetendue.NewSource(zonetendue.Options{DataDir: d.DataDir}), nil
			},
		},
		{
			Name:    links.Name,
			Default: true,
			Build: func(_ *runtimeDeps) (gazetteer.Source, error) {
				return links.NewSource(links.Options{}), nil
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
