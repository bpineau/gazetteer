// Package factory wires every in-tree gazetteer Source with sensible
// defaults so callers can obtain a working *gazetteer.Client in a
// single function call.
//
// Typical use:
//
//	ctx := context.Background()
//	client, err := factory.NewDefault(ctx)
//	if err != nil { /* handle */ }
//	listing, err := client.Normalize(ctx, "1 rue de Rivoli, 75001 Paris")
//	dossier := client.Collect(ctx, listing)
//
// The factory installs a BAN-backed Normalizer on the Client so
// client.Normalize works without further setup. Callers that need to
// override individual Sources should construct their own
// *gazetteer.Builder directly — see gazetteer/doc.go.
//
// This package lives outside the top-level gazetteer package to avoid
// the import cycle that would otherwise arise from gazetteer importing
// every concrete sources/* package.
package factory

import (
	"context"
	"fmt"

	"github.com/bpineau/gazetteer/dataset"
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

// Options tunes the defaults wired by NewDefault.
//
// The zero value is valid and produces a Client identical to the one
// the gazetteer CLI uses.
type Options struct {
	// HTTPClient overrides the default httpx.Client. When nil, the
	// factory builds one with httpx.New(httpx.Options{}).
	HTTPClient *httpx.Client

	// Communes overrides the embedded communes table. When nil, the
	// factory loads the embedded default via communes.MustDefault.
	Communes communes.Communes

	// SkipNormalizer leaves Client.Normalize unconfigured (it will
	// return gazetteer.ErrNormalizerNotConfigured). Set to true when
	// the caller plans to install a custom Normalizer via the
	// Builder path returned by BuilderDefault.
	SkipNormalizer bool

	// DataDir is the gazetteer data directory injected into every
	// block-dataset Source, so a refreshed copy of a processed artifact
	// found there overrides the embedded one. Empty resolves via
	// dataset.ResolveDir (explicit > $GAZETTEER_DATA_DIR >
	// os.UserCacheDir()/gazetteer). Set "-" to disable the datadir and
	// force embedded-only loading.
	DataDir string

	// Exclude drops the named Sources (matched on Source.Name) from the
	// default roster — to cut the fetch latency and failure surface of
	// Sources the caller never consumes. Unknown names are ignored. The
	// rest of the roster stays auto-updated as in-tree Sources are added,
	// so this is a deny-list, not an allow-list.
	Exclude []string
}

// NewDefault builds a *gazetteer.Client wired with every stable
// in-tree Source: dvf, ademe, anct, bdnb, bpe, cadastre, carteloyers,
// cartofriches, catnat, cdsr, georisques, gpe, iris, ips_ecoles, locservice,
// oll, nuisances, chomage, delinquance, dpedist, education, encadrement, filosofi,
// filoiris, logiris, qpv, rnc, rpls, sitadel, taxefonciere, lovac, vacance, zonageabc,
// zonetendue, links, osm_transit.
//
// osm_transit ships an embedded baseline station catalog (overridable
// from the datadir) and a live Overpass fallback for points the catalog
// does not cover, so it works out of the box.
//
// On any wiring failure (httpx, BAN, communes, Source construction)
// NewDefault returns a non-nil error and a nil *Client.
func NewDefault(ctx context.Context) (*gazetteer.Client, error) {
	return NewDefaultWith(ctx, Options{})
}

// NewDefaultWith is the override-friendly variant of NewDefault.
// Pass an Options struct to swap individual defaults; the zero value
// behaves identically to NewDefault.
func NewDefaultWith(ctx context.Context, opts Options) (*gazetteer.Client, error) {
	b, err := BuilderDefault(ctx, opts)
	if err != nil {
		return nil, err
	}
	return b.Build()
}

// BuilderDefault returns a *gazetteer.Builder pre-populated with every
// stable in-tree Source. Callers can chain .With(extra) before
// .Build() to add their own out-of-tree Source plugins.
//
//	b, err := factory.BuilderDefault(ctx, factory.Options{})
//	if err != nil { ... }
//	client, _ := b.With(myPlugin).Build()
func BuilderDefault(ctx context.Context, opts Options) (*gazetteer.Builder, error) {
	_ = ctx // reserved for future ctx-scoped configuration
	hc := opts.HTTPClient
	if hc == nil {
		// Grant the data.gouv.fr DVF + cadastre endpoints a higher per-host
		// rate than the polite default: DVF fans out one call per cadastral
		// section, so the default 2 req/s serializes a dense-commune lookup
		// into 20 s+.
		built, err := httpx.New(httpx.Options{PerHost: dvf.HostRateLimits()})
		if err != nil {
			return nil, fmt.Errorf("factory: httpx: %w", err)
		}
		hc = built
	}
	ban := banx.NewBANClient(hc)
	com := opts.Communes
	if com == nil {
		com = communes.MustDefault()
	}

	dataDir := resolveDataDir(opts.DataDir)

	dvfSrc, err := dvf.NewSource(dvf.Options{HTTP: hc, Geocoder: ban, Communes: com})
	if err != nil {
		return nil, fmt.Errorf("factory: dvf: %w", err)
	}

	// The IRIS source doubles as the Normalizer's IRISResolver, so build it once
	// and use it for both roles.
	irisSrc := iris.NewSource(iris.Options{DataDir: dataDir})

	b := gazetteer.NewBuilder().
		WithHTTPClient(hc.HTTPClient())
	if !opts.SkipNormalizer {
		b = b.WithNormalizer(gazetteer.NewBANNormalizer(ban, com).WithIRIS(irisSrc))
	}
	b = b.With(dvfSrc).
		With(ademe.NewSource(ademe.Options{Geocoder: ban})).
		With(anct.NewSource(anct.Options{})).
		With(bdnb.NewSource(bdnb.Options{Geocoder: ban})).
		With(bpe.NewSource(bpe.Options{})).
		With(cadastre.NewSource(cadastre.Options{Geocoder: ban})).
		With(georisques.NewSource(georisques.Options{Geocoder: ban})).
		With(gpe.NewSource(gpe.Options{DataDir: dataDir})).
		With(locservice.NewSource(locservice.Options{Geocoder: ban})).
		With(oll.NewSource(oll.Options{DataDir: dataDir})).
		With(carteloyers.NewSource(carteloyers.Options{DataDir: dataDir})).
		With(cartofriches.NewSource(cartofriches.Options{DataDir: dataDir})).
		With(cdsr.NewSource(cdsr.Options{DataDir: dataDir})).
		With(catnat.NewSource(catnat.Options{DataDir: dataDir})).
		With(nuisances.NewSource(nuisances.Options{DataDir: dataDir})).
		With(irisSrc).
		With(chomage.NewSource(chomage.Options{DataDir: dataDir})).
		With(delinquance.NewSource(delinquance.Options{DataDir: dataDir})).
		With(dpedist.NewSource(dpedist.Options{})).
		With(education.NewSource(education.Options{})).
		With(encadrement.NewSource(encadrement.Options{DataDir: dataDir})).
		With(filosofi.NewSource(filosofi.Options{DataDir: dataDir})).
		With(filoiris.NewSource(filoiris.Options{DataDir: dataDir})).
		With(logiris.NewSource(logiris.Options{DataDir: dataDir})).
		With(qpv.NewSource(qpv.Options{DataDir: dataDir})).
		With(rpls.NewSource(rpls.Options{DataDir: dataDir})).
		With(sitadel.NewSource(sitadel.Options{DataDir: dataDir})).
		With(taxefonciere.NewSource(taxefonciere.Options{DataDir: dataDir})).
		With(lovac.NewSource(lovac.Options{DataDir: dataDir})).
		With(vacance.NewSource(vacance.Options{DataDir: dataDir})).
		With(rnc.NewSource(rnc.Options{DataDir: dataDir})).
		With(ips_ecoles.NewSource(ips_ecoles.Options{DataDir: dataDir})).
		With(zonageabc.NewSource(zonageabc.Options{DataDir: dataDir})).
		With(zonetendue.NewSource(zonetendue.Options{DataDir: dataDir})).
		With(links.NewSource(links.Options{})).
		With(gzosm.NewSource(gzosm.Options{DataDir: dataDir, Fetcher: gzosm.NewHTTPOverpassFetcher(hc, "")}))
	return b, nil
}

// resolveDataDir maps factory Options.DataDir onto a concrete directory.
// The sentinel "-" disables the datadir (embedded-only loading); any other
// value defers to dataset.ResolveDir (explicit > $GAZETTEER_DATA_DIR >
// os.UserCacheDir()/gazetteer).
//
// An unresolvable user cache dir is not fatal: the datadir is only an
// optional override of the embedded data, so a resolution failure degrades
// to embedded-only ("") rather than sinking the whole Client — matching the
// CLI's behaviour.
func resolveDataDir(explicit string) string {
	if explicit == "-" {
		return ""
	}
	dir, err := dataset.ResolveDir(explicit)
	if err != nil {
		return ""
	}
	return dir
}
