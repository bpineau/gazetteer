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
	"github.com/bpineau/gazetteer/sources/qpv"
	"github.com/bpineau/gazetteer/sources/rpls"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
	"github.com/bpineau/gazetteer/sources/vacance_logements"
	"github.com/bpineau/gazetteer/sources/zonageabc"
	"github.com/bpineau/gazetteer/sources/zonetendue"
)

// Options tunes the defaults wired by NewDefault.
//
// The zero value is valid and produces a Client identical to the one
// the gazetteer CLI uses, minus the OSM transit source (which needs an
// offline catalog NewDefault does not currently install).
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
}

// NewDefault builds a *gazetteer.Client wired with every stable
// in-tree Source: dvf, ademe, anct, bdnb, bpe, cadastre, georisques,
// ips_ecoles, locservice, carteloyers, cartofriches, chomage,
// delinquance, dpedist, education, encadrement, filosofi, qpv, rpls,
// taxefonciere, vacance, vacance_logements, zonageabc, zonetendue.
//
// The OSM transit Source is NOT included by default — it requires
// an offline station catalog the factory does not load. Callers that
// need OSM should add it to the returned Client by reconstructing
// a Builder, or call BuilderDefault and append before .Build().
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
		built, err := httpx.New(httpx.Options{})
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

	dataDir, err := resolveDataDir(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("factory: datadir: %w", err)
	}

	dvfSrc, err := dvf.NewSource(dvf.Options{HTTP: hc, Geocoder: ban, Communes: com})
	if err != nil {
		return nil, fmt.Errorf("factory: dvf: %w", err)
	}

	b := gazetteer.NewBuilder().
		WithHTTPClient(hc.HTTPClient())
	if !opts.SkipNormalizer {
		b = b.WithNormalizer(gazetteer.NewBANNormalizer(ban, com))
	}
	b = b.With(dvfSrc).
		With(ademe.NewSource(ademe.Options{Geocoder: ban})).
		With(anct.NewSource(anct.Options{})).
		With(bdnb.NewSource(bdnb.Options{Geocoder: ban})).
		With(bpe.NewSource(bpe.Options{})).
		With(cadastre.NewSource(cadastre.Options{Geocoder: ban})).
		With(georisques.NewSource(georisques.Options{Geocoder: ban})).
		With(locservice.NewSource(locservice.Options{Geocoder: ban})).
		With(carteloyers.NewSource(carteloyers.Options{DataDir: dataDir})).
		With(cartofriches.NewSource(cartofriches.Options{DataDir: dataDir})).
		With(chomage.NewSource(chomage.Options{DataDir: dataDir})).
		With(delinquance.NewSource(delinquance.Options{DataDir: dataDir})).
		With(dpedist.NewSource(dpedist.Options{})).
		With(education.NewSource(education.Options{})).
		With(encadrement.NewSource(encadrement.Options{DataDir: dataDir})).
		With(filosofi.NewSource(filosofi.Options{DataDir: dataDir})).
		With(qpv.NewSource(qpv.Options{DataDir: dataDir})).
		With(rpls.NewSource(rpls.Options{DataDir: dataDir})).
		With(taxefonciere.NewSource(taxefonciere.Options{DataDir: dataDir})).
		With(vacance.NewSource(vacance.Options{DataDir: dataDir})).
		With(vacance_logements.NewSource(vacance_logements.Options{DataDir: dataDir})).
		With(ips_ecoles.NewSource(ips_ecoles.Options{DataDir: dataDir})).
		With(zonageabc.NewSource(zonageabc.Options{DataDir: dataDir})).
		With(zonetendue.NewSource(zonetendue.Options{DataDir: dataDir}))
	return b, nil
}

// resolveDataDir maps factory Options.DataDir onto a concrete directory.
// The sentinel "-" disables the datadir (embedded-only loading); any other
// value defers to dataset.ResolveDir (explicit > $GAZETTEER_DATA_DIR >
// os.UserCacheDir()/gazetteer).
func resolveDataDir(explicit string) (string, error) {
	if explicit == "-" {
		return "", nil
	}
	return dataset.ResolveDir(explicit)
}
