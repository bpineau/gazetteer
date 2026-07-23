package dvf

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/circuit"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/fallback"
	"github.com/bpineau/gazetteer/helpers/geodist"
	"github.com/bpineau/gazetteer/helpers/geopoly"
	"github.com/bpineau/gazetteer/helpers/httpx"
	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "dvf"

// sourceVersion bumps when the Source's internal logic changes.
// Stateful callers gate cache invalidation on it.
//
// History:
//   - v1: initial release (commune → neighborhood → department ladder, no
//     nature_mutation filter — pooled Vente + VEFA + adjudication +
//     échange + terrain).
//   - v2: FilterMutations now restricts to nature_mutation = "Vente"
//     (ordinary resales). VEFA neuf / adjudication / échange / terrain à
//     bâtir are excluded so the DVF cohort stays comparable to the
//     ancien-rue street-level surfaces MA and Pappersimmo measure.
//   - v3: sub-commune `address_radius` tier inserted at top of ladder
//     (500 m disk around `auction.lat/auction.lon`, MinSample 12).
//   - v4: `ValueEURCents` (price × surface) now rounds in float space
//     instead of truncating via `int64(surfaceM2)`. Removes a 0.5-1 %
//     downward bias on every non-integer surface ; visible on the
//     dossier's total-value field and on every appraisal aggregate.
//     n-per-id_parcelle cap of 4 applied globally to FilterMutations
//     as a defensive guard.
const sourceVersion = 4

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// DVFAddressRadiusMeters is the disk radius (in metres) used by the
// `address_radius` tier to filter the commune's mutations down to
// those within HaversineKm of the auction's geocoded
// `(lat, lon)`. Empirically chosen so ≈89 % of urban / suburban
// auctions clear MinSampleSizeAddressRadius while preserving a tight
// micro-quartier granularity.
const DVFAddressRadiusMeters = 500.0

// MaxConsecutiveTransportErrors is the threshold above which the DVF
// transport circuit breaker trips. data.gouv.fr is CDN-fronted and
// usually steady (180-280 ms), but a backend incident can cascade
// across every (insee, section) call for minutes; three consecutive
// transport / context-deadline failures with no 2xx in between aborts
// the rest of the run rather than burning the scheduler window on
// retry backoffs × N cadastre sections.
const MaxConsecutiveTransportErrors = 3

// MaxConsecutive429 is the threshold above which a sustained run of
// HTTP 429 responses against the DVF endpoint trips the breaker.
// DVF (data.gouv.fr) is not rate-limited under normal conditions —
// this is a defensive layer so an unexpected 429 burst (CDN outage,
// upstream throttling change) does not burn the maintenance run on
// retry-backoff. Sporadic 429s reset on the next 2xx.
const MaxConsecutive429 = 3

// maxSectionConcurrency bounds how many per-section GetMutations calls run
// at once within a single commune fan-out. The per-host token-bucket rate
// limiter is the real throttle; this just caps in-flight goroutines (and
// sockets) so a dense commune (~50 sections) doesn't open 50 at once.
const maxSectionConcurrency = 8

// sectionPrefilterMarginMeters is added to the address_radius disk when
// prefiltering sections by their bounding box, so a mutation geocoded just
// outside its section's box (geocoding noise) is never dropped before the
// precise per-mutation haversine cut runs.
const sectionPrefilterMarginMeters = 150.0

// dvfAPIHost / cadastreHost are the live endpoints the Source calls. Exposed
// indirectly through HostRateLimits so callers can grant them a higher
// per-host rate than the polite httpx default (2 req/s) — both data.gouv.fr
// APIs comfortably serve ~10 req/s, and the per-section fan-out is otherwise
// throttle-bound on dense communes.
const (
	dvfAPIHost   = "dvf-api.data.gouv.fr"
	cadastreHost = "cadastre.data.gouv.fr"

	// hostRateLimit is the per-host requests/second granted to the DVF and
	// cadastre endpoints. The DVF API documents headroom well above this.
	hostRateLimit = 10.0
	hostBurst     = 10
)

// HostRateLimits returns the recommended httpx per-host overrides for the
// live endpoints this Source calls. Wire it into httpx.Options.PerHost when
// constructing the shared client:
//
//	hc, _ := httpx.New(httpx.Options{PerHost: dvf.HostRateLimits()})
//
// Without it the default 2 req/s serializes the per-section fan-out and a
// single dense-commune lookup can take 20 s+.
func HostRateLimits() map[string]httpx.HostOptions {
	rl, burst := hostRateLimit, hostBurst
	return map[string]httpx.HostOptions{
		dvfAPIHost:   {RateLimit: &rl, Burst: &burst},
		cadastreHost: {RateLimit: &rl, Burst: &burst},
	}
}

// Options configures a dvf Source. The zero value is usable: a nil
// HTTP defaults to a fresh polite client (see the HTTP field), and
// Geocoder is only required when Listings arrive without a usable
// Listing.INSEE (Listing.INSEE is populated only when callers
// pre-resolve the commune themselves).
type Options struct {
	// HTTP is the production HTTP client. When nil, NewSource builds a
	// rate-limited httpx client seeded with HostRateLimits() — polite
	// but per-Source (no shared cache, no cross-Source rate-limit
	// pooling). Production wiring should pass the factory's shared
	// client instead.
	HTTP *httpx.Client

	// Geocoder resolves a free-form address to a 5-digit INSEE via the
	// BAN forward + reverse cascade. Required unless every Listing
	// passed to Query already carries a usable Listing.INSEE.
	Geocoder banx.Geocoder

	// Communes is the centroid + department table used by the
	// neighborhood / department tiers. Defaults to communes.Default()
	// when nil.
	Communes communes.Communes

	// SectionCache is the kvcache backend the SectionDiscoverer uses
	// to memoise per-commune section lists. Defaults to an in-memory
	// memcache when nil; callers that want cross-run persistence
	// supply a persistent kvcache.Cache backend here.
	SectionCache kvcache.Cache

	// Logger overrides slog.Default(). Optional.
	Logger *slog.Logger

	// APIBaseURL overrides the DVF mutations endpoint for this Source
	// (empty ⇒ the package default). Prefer this over mutating the
	// package-level APIBaseURL var.
	APIBaseURL string

	// CircuitTripped, when non-nil, is a process-local circuit breaker
	// shared with the API. The API observes its GetMutations outcomes
	// through this atomic via the embedded TransportCircuit; once the
	// threshold is hit, Query short-circuits with ErrCircuitTripped.
	// The flag is process-scoped: a fresh run starts fresh.
	CircuitTripped *atomic.Bool
}

// Source implements gazetteer.Source for the DVF Etalab API. Use
// NewSource to construct.
type Source struct {
	opts     Options
	api      *API
	sections *SectionDiscoverer
	communes communes.Communes

	// mutationsSF coalesces concurrent (insee, section) mutation fetches
	// across DIFFERENT Query calls on this shared Source. The per-Query
	// queryMemo already dedupes within one ladder walk; this closes the
	// remaining gap where two Collects for nearby addresses would fetch
	// the same section payload simultaneously. It only merges in-flight
	// duplicates — nothing is cached, so there is no staleness. See
	// fetchMutations.
	mutationsSF singleflight.Group
}

// ErrCircuitTripped is returned when the upstream DVF endpoint has
// tripped the process-local circuit breaker — i.e. a run of consecutive
// transport / context-deadline failures crossed the threshold. The flag
// is process-scoped; a fresh run starts fresh.
//
// errors.Is(err, dvf.ErrCircuitTripped) keeps working for dvf-specific
// matching. The error also matches gazetteer.ErrSourceCircuitTripped
// for cross-source diagnostics.
var ErrCircuitTripped = gazetteer.NewCircuitTrippedError(Name)

// NewSource builds a dvf Source. The zero-value Options is valid — a
// nil HTTP gets a fresh rate-limited client (see Options.HTTP) — so the
// constructor matches the uniform per-source contract:
//
//	src := dvf.NewSource(dvf.Options{HTTP: hc, Geocoder: ban})
//	client, _ := gazetteer.NewBuilder().With(src).Build()
//
// A corrupted embedded communes table (a build-time invariant) panics
// via communes.MustDefault rather than surfacing as a runtime error.
func NewSource(opts Options) *Source {
	if opts.HTTP == nil {
		// Never errors today (httpx.New normalises bad options); a nil
		// client is still guarded so a future validating httpx cannot
		// hand us one silently.
		if hc, err := httpx.New(httpx.Options{PerHost: HostRateLimits()}); err == nil {
			opts.HTTP = hc
		}
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Communes == nil {
		opts.Communes = communes.MustDefault()
	}
	if opts.SectionCache == nil {
		opts.SectionCache = memcache.New()
	}
	tc := circuit.NewTransportCircuit(Name, MaxConsecutiveTransportErrors, opts.CircuitTripped, opts.Logger)
	tc.SetMax429(MaxConsecutive429)
	return &Source{
		opts:     opts,
		api:      NewAPI(opts.HTTP, tc).WithBaseURL(opts.APIBaseURL),
		sections: NewSectionDiscoverer(opts.SectionCache, opts.Logger),
		communes: opts.Communes,
	}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// logger returns the Source's slog.Logger with the `comp` field set.
// Safe on nil — falls back to slog.Default().
func (s *Source) logger() *slog.Logger {
	if s == nil || s.opts.Logger == nil {
		return slog.Default().With(slog.String("comp", "gazetteer.dvf"))
	}
	return s.opts.Logger.With(slog.String("comp", "gazetteer.dvf"))
}

// Query implements gazetteer.Source. Pipeline:
//
//  1. Map listing.PropertyType to the DVF `type_local` filter; bail
//     with gazetteer.ErrUnsupportedPropertyType for parking / land /
//     mixed / other.
//  2. Resolve INSEE via the BAN cascade (forward + reverse).
//  3. Walk the 4-tier ladder (address_radius → commune →
//     neighborhood → department).
//  4. Compute medians + quartiles + confidence.
//  5. Return (*Result, nil). The framework records StatusOKEmpty when
//     Result.IsEmpty() returns true.
//
// Error mapping (the framework translates these to a Result.Status):
//   - Unsupported property_type → gazetteer.ErrUnsupportedPropertyType
//   - Missing address+city+zip+coords → gazetteer.ErrInsufficientInputs
//   - Geocoder cannot resolve INSEE → gazetteer.ErrInsufficientInputs
//   - Circuit tripped → ErrCircuitTripped (treated as transient)
//   - Ladder walk failure (every tier errored) → wrapped error
//
// Each call may issue several HTTP requests (one per section × tier
// fan-out). Respect ctx.Done().
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	if s.opts.CircuitTripped != nil && s.opts.CircuitTripped.Load() {
		return nil, ErrCircuitTripped
	}

	target := MapPropertyTypeToDVF(string(l.PropertyType))
	if target == "" {
		return nil, fmt.Errorf("dvf: %w: %q", gazetteer.ErrUnsupportedPropertyType, l.PropertyType)
	}

	insee, inseeSource, err := s.resolveINSEE(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("dvf: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	asOf := time.Now().UTC()
	if !l.AsOf.IsZero() {
		asOf = l.AsOf
	}
	cutoff := asOf.AddDate(-CutoffYears, 0, 0)

	tc := &tierContext{
		target:     target,
		cutoff:     cutoff,
		auctionLat: l.Lat,
		auctionLon: l.Lon,
		memo:       newQueryMemo(),
	}
	ladder := s.buildLadder(insee, tc)
	// The DVF tiers close over tc + their INSEE lists and ignore the
	// canonical fallback.Input, so pass the zero value.
	out, walkErr := fallback.Walk(ctx, s.logger(), ladder, fallback.Input{})
	if walkErr != nil {
		return nil, fmt.Errorf("dvf: ladder walk: %w", walkErr)
	}
	levelUsed := out.LevelUsed

	confidence := PickConfidence(len(tc.filtered), levelUsed)

	p25v, p50, p75v := PerM2Quartiles(tc.filtered)

	var valuePerM2Cents, valueCents *int64
	if p50 > 0 {
		v := int64(math.Round(p50 * 100))
		valuePerM2Cents = &v
		if l.SurfaceM2 != nil && *l.SurfaceM2 > 0 {
			// Compute the total in float space so a fractional surface
			// (90.5 m²) is not silently truncated to its integer floor —
			// the previous `v * int64(*l.SurfaceM2)` lost up to 0.99 m²
			// of value on every non-integer surface.
			tot := int64(math.Round(p50 * (*l.SurfaceM2) * 100))
			valueCents = &tot
		}
	}
	var p25c, p75c *int64
	if p25v > 0 {
		v := int64(math.Round(p25v * 100))
		p25c = &v
	}
	if p75v > 0 {
		v := int64(math.Round(p75v * 100))
		p75c = &v
	}

	ev := Evidence{
		LevelUsed:              levelUsed,
		CommunesQueried:        tc.communesQueried,
		PrimaryINSEE:           insee,
		INSEEResolutionSource:  inseeSource,
		TypeLocalFilter:        target,
		WindowYears:            CutoffYears,
		RawMutationsCount:      tc.totalRaw,
		FilteredMutationsCount: len(tc.filtered),
		SectionsQueried:        tc.sectionsQueried,
		NUniqueParcelles:       CountUniqueParcelles(tc.filtered),
	}
	if levelUsed == "address_radius" {
		ev.RadiusM = tc.radiusM
		ev.AuctionLat = l.Lat
		ev.AuctionLon = l.Lon
	}

	return &Result{
		ValueEURPerM2Cents: valuePerM2Cents,
		ValueEURCents:      valueCents,
		P25EURPerM2Cents:   p25c,
		P75EURPerM2Cents:   p75c,
		SampleSize:         len(tc.filtered),
		Confidence:         confidence,
		Evidence:           ev,
	}, nil
}

// resolveINSEE returns the 5-digit commune code for the listing: trust
// Listing.INSEE when set, else run the canonical BAN cascade
// (banx.ResolveINSEE → INSEEResolver):
//
//  1. listing.INSEE when non-empty (trusted).
//  2. BAN forward on the address (high-confidence trust). The bare
//     l.Address is passed (NOT pre-joined with zip/city) —
//     GeocodeQuery.String() appends zip/city only when absent, and
//     doubling them was a documented geocode-score regression.
//  3. BAN reverse on listing.Lat/Lon when present (enabled when the
//     Geocoder also implements banx.ReverseGeocoder).
//  4. Error otherwise.
//
// Returns (insee, source) where source ∈ {"listing", "ban_forward",
// "ban_reverse"} for traceability in Evidence.INSEEResolutionSource.
func (s *Source) resolveINSEE(ctx context.Context, l gazetteer.Listing) (insee, source string, err error) {
	if i := strings.TrimSpace(l.INSEE); i != "" {
		return i, "listing", nil
	}
	var lat, lon float64
	if l.Lat != nil {
		lat = *l.Lat
	}
	if l.Lon != nil {
		lon = *l.Lon
	}
	return banx.ResolveINSEE(ctx, s.opts.Geocoder, l.Address, l.City, l.Zip, lat, lon)
}

// fetchMutationsForCommunes fans out across the communes INSEE list,
// for each one enumerates sections (cached) and collects mutations.
// Returns the concatenated mutation list + sections queried (cumulative).
// The counts are LOGICAL per-tier numbers: a memo hit still counts its
// sections, so Evidence.SectionsQueried is identical with or without
// the per-Query memo.
//
// Per-section errors are swallowed (warn-logged) so a single bad
// commune in a multi-commune fan-out tier does not break the whole
// query. The circuit-breaker check inside GetMutations + the outer-loop
// breaker check below ensure runaway transport failures still abort.
func (s *Source) fetchMutationsForCommunes(ctx context.Context, memo *queryMemo, communesINSEE []string) ([]Mutation, int) {
	var all []Mutation
	totalSecs := 0
	for _, insee := range communesINSEE {
		if ctx.Err() != nil || s.circuitTripped() {
			return all, totalSecs
		}
		secs := s.resolveSections(ctx, memo, insee)
		totalSecs += len(secs)
		all = append(all, s.fetchSections(ctx, memo, insee, secs)...)
	}
	return all, totalSecs
}

// fetchSections fetches the given sections of one commune concurrently
// (bounded by maxSectionConcurrency) and returns their pooled mutations.
// Order is not preserved — every downstream consumer (FilterMutations,
// quantiles) is order-independent. Per-section failures are swallowed
// (warn-logged) so one bad section never sinks the fan-out; a tripped
// circuit or cancelled ctx stops launching further fetches.
//
// Each (insee, section) outcome is memoised in `memo` so the geographic
// superset tiers of one ladder walk fetch a section at most once per
// Query; only definitive outcomes (success / 404) are stored, so a
// transiently-failed section is still retried by a later tier.
func (s *Source) fetchSections(ctx context.Context, memo *queryMemo, insee string, secs []string) []Mutation {
	var (
		mu  sync.Mutex
		all []Mutation
		wg  sync.WaitGroup
		sem = make(chan struct{}, maxSectionConcurrency)
	)
	for _, sec := range secs {
		if ctx.Err() != nil || s.circuitTripped() {
			break
		}
		if cached, ok := memo.mutationsFor(insee, sec); ok {
			mu.Lock() // earlier iterations' goroutines may still append
			all = append(all, cached...)
			mu.Unlock()
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(sec string) {
			defer wg.Done()
			defer func() { <-sem }()
			if ctx.Err() != nil || s.circuitTripped() {
				return
			}
			r, err := s.fetchMutations(ctx, insee, sec)
			if err != nil {
				if errors.Is(err, ErrSectionNotFound) {
					// Definitive: the section has no DVF data.
					memo.storeMutations(insee, sec, nil)
					return
				}
				s.logger().Warn("dvf.mutations_fetch_failed",
					slog.String("insee", insee),
					slog.String("section", sec),
					slog.Any("err", err),
				)
				return
			}
			memo.storeMutations(insee, sec, r.Data)
			mu.Lock()
			all = append(all, r.Data...)
			mu.Unlock()
		}(sec)
	}
	wg.Wait()
	return all
}

// fetchMutations issues one GET /mutations/{insee}/{section}, coalescing
// concurrent fetches for the SAME (insee, section) across different Query
// calls into a single upstream request via mutationsSF. Nearby-address
// Collects that reach the same cadastral section therefore pay for it once
// while it is in flight — the per-Query queryMemo only dedupes within one
// ladder walk, so without this the shared payload would be re-fetched in
// parallel. It merges in-flight duplicates only; nothing is cached, so a
// completed fetch releases its key and the next call fetches fresh data
// (no staleness).
//
// Two properties are load-bearing:
//
//   - Context isolation. The shared fetch runs on context.WithoutCancel of
//     the initiating caller's ctx, so an initiator that gives up (its ctx
//     is cancelled or times out) does NOT cancel the request the surviving
//     waiters still depend on — a cancelled initiator must not poison the
//     shared result. GetMutations still wraps its GET in APICallTimeout, so
//     detaching from the caller's deadline cannot leak an unbounded
//     request; WithoutCancel keeps the ctx values (HTTP client, logger).
//     Each caller selects on its OWN ctx, so a cancelled caller returns
//     promptly with ctx.Err() without waiting on the shared flight.
//
//   - Error fidelity. singleflight delivers the (value, error) pair to
//     every waiter, so a failure — a transport error, or a breaker-tripped
//     ErrCircuitOpen that GetMutations returns before any HTTP call — is
//     propagated to all waiters as an error and is never shared as a
//     successful empty response.
func (s *Source) fetchMutations(ctx context.Context, insee, section string) (MutationsResponse, error) {
	key := insee + "/" + section
	// Detach the shared fetch from THIS caller's cancellation/deadline so
	// the caller that happens to initiate the flight cannot cancel it out
	// from under its siblings.
	shared := context.WithoutCancel(ctx)
	ch := s.mutationsSF.DoChan(key, func() (any, error) {
		return s.api.GetMutations(shared, insee, section)
	})
	select {
	case <-ctx.Done():
		return MutationsResponse{}, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return MutationsResponse{}, res.Err
		}
		return res.Val.(MutationsResponse), nil
	}
}

// circuitTripped reports whether the process-local DVF breaker is open.
func (s *Source) circuitTripped() bool {
	return s.opts.CircuitTripped != nil && s.opts.CircuitTripped.Load()
}

// fetchAddressRadiusMutations collects the mutations the address_radius tier
// post-filters. It prefilters the primary commune's cadastral sections to the
// few whose bounding box falls within the disk (radius + margin) around the
// point, then fetches only those — turning a ~50-section Paris-arrondissement
// fan-out into a handful of calls. If the prefilter is unavailable (cadastre
// fetch failed, or no section qualifies), it falls back to the full commune
// fan-out so the tier never silently loses coverage.
//
// Returns the pooled mutations and the number of sections actually queried
// (for Evidence.SectionsQueried).
func (s *Source) fetchAddressRadiusMutations(ctx context.Context, memo *queryMemo, communesINSEE []string, lat, lon float64) ([]Mutation, int) {
	if len(communesINSEE) == 0 {
		return nil, 0
	}
	insee := communesINSEE[0]
	secs := s.sectionsNearPoint(ctx, memo, insee, lat, lon, DVFAddressRadiusMeters+sectionPrefilterMarginMeters)
	if len(secs) == 0 {
		// Prefilter unavailable — preserve the original full-commune behavior.
		return s.fetchMutationsForCommunes(ctx, memo, communesINSEE[:1])
	}
	return s.fetchSections(ctx, memo, insee, secs), len(secs)
}

// sectionsNearPoint returns the DVF section codes for `insee` whose cadastral
// bounding box lies within radiusM of (lat, lon). Returns nil — signalling
// "prefilter unavailable, fall back" — when the cadastre geometry can't be
// fetched or yields no codes. The bbox test is conservative (a box never
// underestimates its geometry's extent), so a section is dropped only when no
// point inside it can possibly be within the radius.
func (s *Source) sectionsNearPoint(ctx context.Context, memo *queryMemo, insee string, lat, lon, radiusM float64) []string {
	geos, err := s.sectionGeos(ctx, memo, insee)
	if err != nil {
		if !errors.Is(err, ErrCadastreCommuneNotFound) {
			s.logger().Warn("dvf.section_geo_fetch_failed",
				slog.String("insee", insee),
				slog.Any("err", err),
			)
		}
		return nil
	}
	out := make([]string, 0, len(geos))
	for _, g := range geos {
		// An empty (inverted-infinity) box means the section's geometry was
		// absent or unparseable: its extent is UNKNOWN, so keep it rather than
		// risk dropping a section that could hold an in-disk mutation. Keeping
		// it is safe (the precise per-mutation haversine cut runs downstream);
		// dropping it would silently lose coverage.
		if bboxEmpty(g.Box) || pointToBBoxMeters(lat, lon, g.Box) <= radiusM {
			out = append(out, g.Code)
		}
	}
	return out
}

// sectionGeos returns the reduced per-section geometry list (code + bbox)
// for `insee`, resolving per-Query memo → kvcache (GeosForCommune) → live
// cadastre GeoJSON download. The raw commune GeoJSON runs hundreds of KB
// to MB; the reduced []SectionGeo is a few hundred bytes, so that is the
// form worth caching (cf. SectionDiscoverer.PrimeGeos).
//
// A live download also primes the section-CODE list — the geo response
// contains the codes, filtered and formatted exactly as
// FetchCadastreSections would return them — into the per-Query memo and
// the kvcache, so the commune tier's resolveSections never re-downloads
// the same GeoJSON seconds later. A kvcache hit primes the memo'd code
// list too, covering the case where the code cache was lost separately.
func (s *Source) sectionGeos(ctx context.Context, memo *queryMemo, insee string) ([]SectionGeo, error) {
	if geos, ok := memo.geosFor(insee); ok {
		return geos, nil
	}
	cached, err := s.sections.GeosForCommune(ctx, insee)
	if err != nil {
		s.logger().Warn("dvf.section_geos_lookup_failed",
			slog.String("insee", insee),
			slog.Any("err", err),
		)
		// Fall through — fetch from cadastre below.
	}
	if len(cached) > 0 {
		memo.storeGeos(insee, cached)
		memo.storeSections(insee, sectionCodes(cached))
		return cached, nil
	}
	geos, ferr := FetchCadastreSectionGeos(ctx, s.api.http, insee)
	if ferr != nil {
		return nil, ferr
	}
	memo.storeGeos(insee, geos)
	if perr := s.sections.PrimeGeos(ctx, insee, geos); perr != nil {
		s.logger().Warn("dvf.section_geos_prime_failed",
			slog.String("insee", insee),
			slog.Any("err", perr),
		)
	}
	if codes := sectionCodes(geos); len(codes) > 0 {
		memo.storeSections(insee, codes)
		if perr := s.sections.PrimeFromList(ctx, insee, codes); perr != nil {
			s.logger().Warn("dvf.cadastre_prime_failed",
				slog.String("insee", insee),
				slog.Any("err", perr),
			)
		}
	}
	return geos, nil
}

// sectionCodes projects geos down to its DVF section-code list. By
// construction this is the same set FetchCadastreSections returns for
// the commune — both fetchers apply the same commune/format filters and
// dedup — so it can safely prime the section-code cache.
func sectionCodes(geos []SectionGeo) []string {
	out := make([]string, 0, len(geos))
	for _, g := range geos {
		out = append(out, g.Code)
	}
	return out
}

// bboxEmpty reports whether b is the inverted-infinity box emptyBBox returns
// for a geometry with no vertices (Min > Max on either axis).
func bboxEmpty(b geopoly.BBox) bool {
	return b.MinLat > b.MaxLat || b.MinLon > b.MaxLon
}

// pointToBBoxMeters approximates the great-circle distance from (lat, lon) to
// the nearest point of box b by clamping the point to the box on each axis
// independently. A point inside the box clamps to itself → distance 0.
//
// The independent-axis clamp is a tight lower bound for the small boxes at
// mid-latitudes this package deals with (a cadastral section at 42–51 °N: the
// overestimate is sub-metre, dwarfed by sectionPrefilterMarginMeters). It is
// NOT a general primitive: near the poles the great-circle nearest point on a
// meridian edge bows poleward and the clamp can overestimate substantially.
// Callers must pass a non-empty box (see bboxEmpty) — an inverted box yields
// NaN here.
func pointToBBoxMeters(lat, lon float64, b geopoly.BBox) float64 {
	cLat := math.Max(b.MinLat, math.Min(lat, b.MaxLat))
	cLon := math.Max(b.MinLon, math.Min(lon, b.MaxLon))
	return geodist.MetersBetween(lat, lon, cLat, cLon)
}

// resolveSections returns the cadastral section codes (DVF-formatted)
// for `insee`. Strategy:
//
//  1. Per-Query memo hit (primed by an earlier tier or by sectionGeos).
//  2. Read the kv_cache via SectionsForCommune. Trust a non-empty result.
//  3. On cache miss, query cadastre.data.gouv.fr — which gives the
//     exact set of sections that exist for the commune, including
//     1-letter codes (e.g. Stains "0000A"). Re-prime the cache on
//     success.
//  4. On cadastre 404 / network failure, return empty (NOT memoised, so
//     a later tier retries) — the legacy 000AA..000ZZ brute-force walker
//     was removed since the cadastre primer covers 100 % of communes.
//
// Empty results bubble up as len==0; the caller (mutation collector)
// simply records 0 sections queried for that commune.
func (s *Source) resolveSections(ctx context.Context, memo *queryMemo, insee string) []string {
	if secs, ok := memo.sectionsFor(insee); ok {
		return secs
	}
	cached, err := s.sections.SectionsForCommune(ctx, insee)
	if err != nil {
		s.logger().Warn("dvf.sections_lookup_failed",
			slog.String("insee", insee),
			slog.Any("err", err),
		)
		// Fall through — try cadastre below.
	}
	if len(cached) > 0 {
		memo.storeSections(insee, cached)
		return cached
	}

	// Cache miss: query the cadastre primer. This is the ground truth
	// from cadastre.data.gouv.fr (same source the DVF webapp itself
	// consumes), so a non-empty result is authoritative.
	cad, cerr := FetchCadastreSections(ctx, s.api.http, insee)
	if cerr == nil && len(cad) > 0 {
		memo.storeSections(insee, cad)
		if perr := s.sections.PrimeFromList(ctx, insee, cad); perr != nil {
			s.logger().Warn("dvf.cadastre_prime_failed",
				slog.String("insee", insee),
				slog.Any("err", perr),
			)
		}
		s.logger().Debug("dvf.cadastre_primed",
			slog.String("insee", insee),
			slog.Int("sections", len(cad)),
		)
		return cad
	}
	if cerr != nil && !errors.Is(cerr, ErrCadastreCommuneNotFound) {
		s.logger().Warn("dvf.cadastre_lookup_failed",
			slog.String("insee", insee),
			slog.Any("err", cerr),
		)
	}
	return nil
}

// Sections exposes the Source's SectionDiscoverer to callers that
// need to prime the cache (e.g. tests) or share the discoverer across
// adapters.
func (s *Source) Sections() *SectionDiscoverer { return s.sections }

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

// QueryResult is Query with the package's typed Result — for callers
// holding a constructed Source instance. Equivalent to the package-level
// Query helper without rebuilding the Source per call.
func (s *Source) QueryResult(ctx context.Context, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, s, l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
