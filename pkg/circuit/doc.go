// Package circuit consolidates the per-source circuit-breaker pattern
// used by every HTTP-backed scraper/enricher: trip on N consecutive
// transport errors, trip on a sliding-window error rate (RateWindow,
// catches slow-burn failures the consecutive-streak counter misses),
// trip on observed quota exhaustion (HTTP 429 or response header
// x-quota-remaining=0), trip on N consecutive HTTP-200 responses with
// no signal-bearing content (ContentStreakBreaker, the "paywall stub"
// / "anti-bot interstitial" failure mode that the transport breakers
// cannot see), and expose process-local gauge + monotonic counter for
// metrics.
//
// The breaker pointer is shared with HTTPFetcher so the caller's
// "should I bother scheduling more work?" check is a single atomic
// Load.
//
// # Usage
//
// Three parallel APIs are offered:
//
//   - HTTPFetcher implements Fetcher (Fetch(ctx, url) ([]byte, error))
//     and wraps the breaker logic around the shared httpx.Client GET
//     path. Use this for the common "GET → bytes" enricher shape.
//   - TransportCircuit is a stand-alone breaker that you feed manually
//     via Observe(err). Use it when your enricher does POSTs, custom
//     decoding, or talks to httpx.Client directly.
//   - ContentStreakBreaker is a stand-alone breaker that you feed
//     manually via Observe(body, validator) or ObserveSignal(bool).
//     Use it as a complementary layer when "HTTP 200 + no useful body"
//     is a real failure mode for your upstream (paywall stubs, anti-bot
//     interstitials cached at CDN, stale-cache placeholders). The
//     breaker shares the same *atomic.Bool flag as TransportCircuit so
//     both layers feed one unified "should I keep scheduling work?"
//     check on the hot path.
//
// Both register their *atomic.Bool flag in a process-wide map; the
// snapshot helpers SnapshotCircuitStates / SnapshotCircuitTripCounts
// expose the live state and the monotonic flip counter so a metrics
// handler can render them as gauges / counters.
//
// # Example: HTTPFetcher
//
//	var tripped atomic.Bool
//	fetcher := circuit.NewHTTPFetcher(httpClient, circuit.HTTPFetcherOptions{
//	    ErrPrefix:                     "bdnb",
//	    CircuitTripped:                &tripped,
//	    MaxConsecutiveTransportErrors: 5,
//	    Logger:                        slog.Default(),
//	})
//	body, err := fetcher.Fetch(ctx, "https://example.org/api/v1/x")
//	if tripped.Load() {
//	    // skip remaining work for this run
//	}
//
// # Example: TransportCircuit
//
//	var tripped atomic.Bool
//	cb := circuit.NewTransportCircuit("dvf", 5, &tripped, slog.Default())
//	resp, err := httpClient.Do(req)
//	cb.Observe(err)
//	if cb.Tripped() {
//	    // skip remaining work for this run
//	}
//
// # Example: ContentStreakBreaker
//
//	var tripped atomic.Bool
//	// Same atomic — TransportCircuit + ContentStreakBreaker feed one flag.
//	cs := circuit.NewContentStreakBreaker("licitorweb", 10, &tripped, slog.Default())
//	body, err := fetcher.Fetch(ctx, url)
//	if err == nil {
//	    cs.Observe(body, func(b []byte) bool {
//	        return bytes.Contains(b, []byte("PartnerOffer"))
//	    })
//	}
//	if tripped.Load() {
//	    // skip remaining work for this run — transport-OK but content-empty streak
//	}
package circuit
