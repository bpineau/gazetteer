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
	"sort"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/httpx"
	"github.com/bpineau/gazetteer/internal/roster"
	"github.com/bpineau/gazetteer/sources/iris"
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

	// SourceOverrides swaps the constructor for individual roster
	// Sources while keeping everything else default — the way to tune
	// one Source's Options (a persistent dvf.Options.SectionCache, an
	// injected Fetcher, a custom BaseURL) without abandoning the
	// factory. The override receives the same shared Deps bundle the
	// default constructors use, so the Source shares the rate-limited
	// HTTP client and the cached geocoder with the rest of the roster:
	//
	//	factory.Options{SourceOverrides: map[string]func(factory.Deps) (gazetteer.Source, error){
	//		dvf.Name: func(d factory.Deps) (gazetteer.Source, error) {
	//			return dvf.NewSource(dvf.Options{
	//				HTTP: d.HTTP, Geocoder: d.Geocoder, Communes: d.Communes,
	//				SectionCache: myPersistentKV,
	//			})
	//		},
	//	}}
	//
	// A name that matches no roster Source is a configuration error
	// (BuilderDefault returns it) — unlike Exclude, an override typo
	// would otherwise silently leave the default in place. To ADD a
	// source (a plugin), use Builder.With instead.
	SourceOverrides map[string]func(Deps) (gazetteer.Source, error)
}

// Deps is the shared dependency bundle the factory hands to every
// Source constructor — the default roster's and SourceOverrides alike.
// Reuse these instances in overrides (rather than building parallel
// clients/geocoders) so caching and rate limiting stay process-wide.
type Deps struct {
	// HTTP is the shared rate-limited client (see HostRateLimits).
	HTTP *httpx.Client

	// Geocoder is the cache-wrapped, dept-guarded BAN geocoder
	// (banx.NewDefaultGeocoder) shared by every geocoding Source and
	// the Normalizer.
	Geocoder banx.Geocoder

	// Communes is the embedded commune table.
	Communes communes.Communes

	// DataDir is the resolved gazetteer data directory ("" = embedded
	// only).
	DataDir string
}

// HostRateLimits returns the recommended per-host rate-limit table for
// every upstream the default roster talks to (DVF/cadastre high-rate,
// BAN, bdnb quota, polite .gouv.fr defaults, Overpass, …). Custom
// wirings that pass Options.HTTPClient should start from this table and
// extend it rather than rediscover each host's tolerance:
//
//	limits := factory.HostRateLimits()
//	limits["my.upstream.example"] = httpx.HostOptions{…}
//	hc, _ := httpx.New(httpx.Options{PerHost: limits})
func HostRateLimits() map[string]httpx.HostOptions {
	return roster.HostRateLimits()
}

// LiveSourceNames returns the names of the default-roster Sources that
// may perform network I/O during Query (dvf, georisques, …, and
// osm_transit whose Overpass fallback can go live). Sorted.
func LiveSourceNames() []string { return liveNames(true) }

// OfflineSourceNames returns the names of the default-roster Sources
// that answer from embedded datasets only — instant, no network
// dependency. Use it to collect a fast first phase before paying for
// the live APIs:
//
//	quick := client.CollectSome(ctx, listing, factory.OfflineSourceNames()...)
//
// Sorted.
func OfflineSourceNames() []string { return liveNames(false) }

func liveNames(live bool) []string {
	var out []string
	for _, e := range roster.Entries() {
		if e.Live == live {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

// NewDefault builds a *gazetteer.Client wired with every stable
// in-tree Source — one per sources/<name> package (run `gazetteer
// sources list`, or see docs/sources.md, for the roster). All of them
// work out of the box: offline sources ship embedded datasets, and
// osm_transit pairs its embedded station catalog with a live Overpass
// fallback.
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
		built, err := roster.NewHTTPClient()
		if err != nil {
			return nil, fmt.Errorf("factory: %w", err)
		}
		hc = built
	}
	com := opts.Communes
	if com == nil {
		com = communes.MustDefault()
	}
	deps := Deps{
		HTTP:     hc,
		Geocoder: banx.NewDefaultGeocoder(hc),
		Communes: com,
		DataDir:  resolveDataDir(opts.DataDir),
	}
	rdeps := roster.Deps{HTTP: deps.HTTP, Geocoder: deps.Geocoder, Communes: deps.Communes, DataDir: deps.DataDir}

	b := gazetteer.NewBuilder().
		WithHTTPClient(hc.HTTPClient())

	// One Source per roster entry — the same single roster the CLI
	// consumes, so the two wirings cannot drift. SourceOverrides swap
	// individual constructors; a typo'd override name is an error.
	overridden := make(map[string]bool, len(opts.SourceOverrides))
	var irisSrc *iris.Source
	for _, e := range roster.Entries() {
		var src gazetteer.Source
		var err error
		if build, ok := opts.SourceOverrides[e.Name]; ok {
			overridden[e.Name] = true
			src, err = build(deps)
		} else {
			src, err = e.Build(rdeps)
		}
		if err != nil {
			return nil, fmt.Errorf("factory: %s: %w", e.Name, err)
		}
		b = b.With(src)
		// The IRIS source doubles as the Normalizer's IRISResolver.
		if ir, ok := src.(*iris.Source); ok {
			irisSrc = ir
		}
	}
	for name := range opts.SourceOverrides {
		if !overridden[name] {
			return nil, fmt.Errorf("factory: SourceOverrides[%q] matches no roster Source (use Builder.With to add new sources)", name)
		}
	}

	if !opts.SkipNormalizer {
		b = b.WithNormalizer(gazetteer.NewBANNormalizer(deps.Geocoder, com).WithIRIS(irisSrc))
	}

	// Apply the deny-list last, once the full roster is assembled, so a
	// caller's Exclude prunes Sources regardless of wiring order and
	// in-tree Sources added later still flow in by default.
	if len(opts.Exclude) > 0 {
		b = b.Without(opts.Exclude...)
	}
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
