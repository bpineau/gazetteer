// Package roster is the single enumeration of every in-tree Source and of
// how to construct it from the shared dependency bundle. The library's
// one-call wiring (factory.BuilderDefault) and the CLI's source registry
// (cmd/gazetteer) both consume it, so adding a Source means adding exactly
// one Entry here — the two consumers can no longer drift apart on which
// sources exist or how they are configured.
//
// The package is internal: the public surfaces remain factory.* for
// library callers and the CLI's --source flag for operators.
package roster

import (
	"fmt"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/httpx"
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
	"github.com/bpineau/gazetteer/sources/dvfagg"
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

// Deps is the shared dependency bundle every Entry.Build draws from.
type Deps struct {
	// HTTP is the shared rate-limited client (see NewHTTPClient).
	HTTP *httpx.Client

	// Geocoder resolves free-form addresses for the live spatial sources.
	// Production wiring uses a cache-wrapped BAN client (see NewGeocoder).
	Geocoder banx.Geocoder

	// Communes is the embedded commune table (INSEE ↔ name/zip/centroid).
	Communes communes.Communes

	// DataDir is the gazetteer data directory; refreshed artifacts found
	// there override the embedded copies. Empty means embedded-only.
	DataDir string
}

// Entry binds a Source name to its constructor.
type Entry struct {
	// Name is the Source's registry name (== sources/<pkg>.Name).
	Name string

	// CLIOptIn marks sources the CLI excludes from a default run unless
	// named explicitly via --source. The library's factory roster always
	// includes them (callers prune with factory Options.Exclude).
	//
	// Today: bdnb only — its public PostgREST endpoint enforces a rolling
	// request budget and routinely 429s anonymous traffic, which would
	// burn interactive CLI wall-clock on a result most callers don't need.
	CLIOptIn bool

	// Live marks sources that may perform network I/O during Query.
	// Offline (!Live) sources answer from embedded datasets only —
	// instant and dependency-free. osm_transit counts as Live: its
	// embedded catalog answers most points, but the Overpass fallback
	// can go to the network when a Fetcher is configured.
	Live bool

	// Build constructs the configured Source. Errors are rare (typically
	// only constructors that validate required deps, e.g. dvf).
	Build func(Deps) (gazetteer.Source, error)
}

// HostRateLimits is the recommended per-host rate-limit table covering
// every upstream the default roster talks to. Values are operationally
// proven (tuned in production against each endpoint's observed
// throttling behaviour):
//
//   - DVF + cadastre (data.gouv.fr CDN): high — DVF fans out one call
//     per cadastral section, so the polite default 2 req/s would
//     serialize a dense-commune lookup into 20 s+ (see dvf.HostRateLimits).
//   - BAN (api-adresse.data.gouv.fr): advertises 50 req/s; 20 is ample.
//   - api.bdnb.io: 10 000 req/month quota — 1 req/s stays well under it.
//   - Public .gouv.fr APIs (georisques, ADEME data-fair, éducation,
//     API Carto): no documented quota; 2 req/s is polite.
//   - Overpass (overpass-api.de): community-funded, ~2 concurrent slots
//     per IP; 1 req/s (catalog refresh is the only caller).
//   - locservice.fr: scraped HTML; 5 req/s observed safe.
//
// Custom wirings (factory.Options.HTTPClient, standalone Sources)
// should start from this table — exposed as factory.HostRateLimits —
// and extend it rather than rediscover each host's tolerance.
func HostRateLimits() map[string]httpx.HostOptions {
	lim := func(rate float64, burst int) httpx.HostOptions {
		return httpx.HostOptions{RateLimit: &rate, Burst: &burst}
	}
	m := dvf.HostRateLimits() // dvf-api.data.gouv.fr + cadastre, high rate
	m["api-adresse.data.gouv.fr"] = lim(20, 30)
	m["api.bdnb.io"] = lim(1, 2)
	m["georisques.gouv.fr"] = lim(2, 4)
	m["data.ademe.fr"] = lim(2, 4)
	m["data.education.gouv.fr"] = lim(2, 4)
	m["apicarto.ign.fr"] = lim(2, 4)
	m["overpass-api.de"] = lim(1, 1)
	m["www.locservice.fr"] = lim(5, 10)
	return m
}

// NewHTTPClient builds the shared httpx client both the factory and the
// CLI use, pre-configured with HostRateLimits.
func NewHTTPClient() (*httpx.Client, error) {
	hc, err := httpx.New(httpx.Options{PerHost: HostRateLimits()})
	if err != nil {
		return nil, fmt.Errorf("httpx: %w", err)
	}
	return hc, nil
}

// Entries returns one Entry per in-tree Source, in the CLI's curated
// thematic order (building / market / commune-level / transit). A fresh
// slice is returned on each call so callers can filter freely.
//
// Completeness is enforced by tests: every gazetteer.Register'ed name has
// exactly one Entry (and the CLI's catalog test keeps descriptors in sync
// with the same registry), so this roster cannot silently drift.
func Entries() []Entry {
	return []Entry{
		{Name: dvf.Name, Live: true, Build: func(d Deps) (gazetteer.Source, error) {
			return dvf.NewSource(dvf.Options{HTTP: d.HTTP, Geocoder: d.Geocoder, Communes: d.Communes})
		}},
		{Name: ademe.Name, Live: true, Build: func(d Deps) (gazetteer.Source, error) {
			return ademe.NewSource(ademe.Options{Geocoder: d.Geocoder}), nil
		}},
		{Name: bdnb.Name, CLIOptIn: true, Live: true, Build: func(d Deps) (gazetteer.Source, error) {
			return bdnb.NewSource(bdnb.Options{Geocoder: d.Geocoder}), nil
		}},
		{Name: cadastre.Name, Live: true, Build: func(d Deps) (gazetteer.Source, error) {
			return cadastre.NewSource(cadastre.Options{Geocoder: d.Geocoder}), nil
		}},
		{Name: georisques.Name, Live: true, Build: func(d Deps) (gazetteer.Source, error) {
			return georisques.NewSource(georisques.Options{Geocoder: d.Geocoder}), nil
		}},
		{Name: locservice.Name, Live: true, Build: func(d Deps) (gazetteer.Source, error) {
			return locservice.NewSource(locservice.Options{Geocoder: d.Geocoder}), nil
		}},
		// OSM transit ships an embedded baseline catalog (overridable from
		// the datadir via `refresh osm_transit`) plus a live Overpass
		// fallback for points the catalog doesn't cover.
		{Name: gzosm.Name, Live: true, Build: func(d Deps) (gazetteer.Source, error) {
			return gzosm.NewSource(gzosm.Options{
				DataDir: d.DataDir,
				Fetcher: gzosm.NewHTTPOverpassFetcher(d.HTTP, ""),
			}), nil
		}},
		{Name: carteloyers.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return carteloyers.NewSource(carteloyers.Options{DataDir: d.DataDir}), nil
		}},
		{Name: dvfagg.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return dvfagg.NewSource(dvfagg.Options{DataDir: d.DataDir}), nil
		}},
		{Name: oll.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return oll.NewSource(oll.Options{DataDir: d.DataDir}), nil
		}},
		{Name: cdsr.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return cdsr.NewSource(cdsr.Options{DataDir: d.DataDir}), nil
		}},
		{Name: catnat.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return catnat.NewSource(catnat.Options{DataDir: d.DataDir}), nil
		}},
		{Name: nuisances.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return nuisances.NewSource(nuisances.Options{DataDir: d.DataDir}), nil
		}},
		{Name: iris.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return iris.NewSource(iris.Options{DataDir: d.DataDir}), nil
		}},
		{Name: encadrement.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return encadrement.NewSource(encadrement.Options{DataDir: d.DataDir}), nil
		}},
		{Name: filosofi.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return filosofi.NewSource(filosofi.Options{DataDir: d.DataDir}), nil
		}},
		{Name: filoiris.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return filoiris.NewSource(filoiris.Options{DataDir: d.DataDir}), nil
		}},
		{Name: logiris.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return logiris.NewSource(logiris.Options{DataDir: d.DataDir}), nil
		}},
		{Name: gpe.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return gpe.NewSource(gpe.Options{DataDir: d.DataDir}), nil
		}},
		{Name: taxefonciere.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return taxefonciere.NewSource(taxefonciere.Options{DataDir: d.DataDir}), nil
		}},
		{Name: lovac.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return lovac.NewSource(lovac.Options{DataDir: d.DataDir}), nil
		}},
		{Name: anct.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return anct.NewSource(anct.Options{DataDir: d.DataDir}), nil
		}},
		{Name: cartofriches.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return cartofriches.NewSource(cartofriches.Options{DataDir: d.DataDir}), nil
		}},
		{Name: chomage.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return chomage.NewSource(chomage.Options{DataDir: d.DataDir}), nil
		}},
		{Name: bpe.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return bpe.NewSource(bpe.Options{DataDir: d.DataDir}), nil
		}},
		{Name: dpedist.Name, Live: true, Build: func(Deps) (gazetteer.Source, error) {
			return dpedist.NewSource(dpedist.Options{}), nil
		}},
		{Name: delinquance.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return delinquance.NewSource(delinquance.Options{DataDir: d.DataDir}), nil
		}},
		{Name: education.Name, Live: true, Build: func(Deps) (gazetteer.Source, error) {
			return education.NewSource(education.Options{}), nil
		}},
		{Name: qpv.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return qpv.NewSource(qpv.Options{DataDir: d.DataDir}), nil
		}},
		{Name: rpls.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return rpls.NewSource(rpls.Options{DataDir: d.DataDir}), nil
		}},
		{Name: vacance.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return vacance.NewSource(vacance.Options{DataDir: d.DataDir}), nil
		}},
		{Name: sitadel.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return sitadel.NewSource(sitadel.Options{DataDir: d.DataDir}), nil
		}},
		{Name: rnc.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return rnc.NewSource(rnc.Options{DataDir: d.DataDir}), nil
		}},
		{Name: ips_ecoles.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return ips_ecoles.NewSource(ips_ecoles.Options{DataDir: d.DataDir}), nil
		}},
		{Name: zonageabc.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return zonageabc.NewSource(zonageabc.Options{DataDir: d.DataDir}), nil
		}},
		{Name: zonetendue.Name, Build: func(d Deps) (gazetteer.Source, error) {
			return zonetendue.NewSource(zonetendue.Options{DataDir: d.DataDir}), nil
		}},
		{Name: links.Name, Build: func(Deps) (gazetteer.Source, error) {
			return links.NewSource(links.Options{}), nil
		}},
	}
}
