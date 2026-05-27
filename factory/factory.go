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

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/httpx"
	"github.com/bpineau/gazetteer/sources/ademe"
	"github.com/bpineau/gazetteer/sources/anct"
	"github.com/bpineau/gazetteer/sources/bdnb"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/cartofriches"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/encadrement"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/georisques"
	"github.com/bpineau/gazetteer/sources/locservice"
	"github.com/bpineau/gazetteer/sources/pinel"
	"github.com/bpineau/gazetteer/sources/qpv"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
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
}

// NewDefault builds a *gazetteer.Client wired with every stable
// in-tree Source: dvf, ademe, anct, bdnb, georisques, locservice,
// carteloyers, cartofriches, delinquance, encadrement, filosofi,
// pinel, qpv, taxefonciere, vacance.
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
// .Build() to add their own Sources (typically out-of-tree plugins
// like bienici, castorus, licitorweb, pappersimmo).
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
		With(georisques.NewSource(georisques.Options{Geocoder: ban})).
		With(locservice.NewSource(locservice.Options{Geocoder: ban})).
		With(carteloyers.NewSource(carteloyers.Options{})).
		With(cartofriches.NewSource(cartofriches.Options{})).
		With(delinquance.NewSource(delinquance.Options{})).
		With(encadrement.NewSource(encadrement.Options{})).
		With(filosofi.NewSource(filosofi.Options{})).
		With(pinel.NewSource(pinel.Options{})).
		With(qpv.NewSource(qpv.Options{})).
		With(taxefonciere.NewSource(taxefonciere.Options{})).
		With(vacance.NewSource(vacance.Options{}))
	return b, nil
}
