package locservice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "locservice"

// sourceVersion bumps when the Source's internal logic changes. Callers
// (a stateful runner) gate cache invalidation on it.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source (e.g.
// a downstream adapter) can mirror it without reaching into the package
// internals.
const Version = sourceVersion

// Options configures a locservice Source. The zero value is usable: every
// field has a sane default (BaseURL → package var BaseURL; Geocoder →
// nil means the Source cannot resolve INSEE and will return
// ErrInsufficientInputs unless the Listing carries a usable INSEE;
// HTTPClient → gazetteer.HTTPClientFrom(ctx) at Query time).
type Options struct {
	// BaseURL overrides the LocService tensiometre endpoint. Tests use
	// this to point at httptest.NewServer. Default: package-level
	// BaseURL var.
	BaseURL string

	// Geocoder is consulted to resolve the listing's address into a
	// 5-digit INSEE — required by LocService since the URL embeds it.
	// When nil, the Source falls back to Listing.INSEE; if neither is
	// usable it returns ErrInsufficientInputs.
	Geocoder banx.Geocoder

	// HTTPClient overrides the per-query HTTP client. When nil, the
	// Source uses gazetteer.HTTPClientFrom(ctx).
	HTTPClient *http.Client
}

// Source implements gazetteer.Source for the LocService Tensiomètre
// Locatif page. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a locservice Source. Zero-valued Options is fine
// but the Source will return ErrInsufficientInputs on every call whose
// Listing has neither a usable INSEE nor an address the Geocoder can
// map.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. It resolves the listing's INSEE
// (preferring Listing.INSEE; falling back to the Geocoder), maps the
// listing's property_type+rooms onto a LocService logement keyword,
// fetches the tensiometre HTML page, parses it, and returns a *Result.
//
// On a no-data response for a logement-specific call, the Source
// widens to the commune-wide call (logement="") in a single retry and
// stamps Evidence.FellBack=true on success.
//
// Error mapping (the framework translates these to a Result.Status per
// the table in gazetteer/source.go):
//
//   - Missing address+city+zip → gazetteer.ErrInsufficientInputs (wrapped)
//   - Geocoder cannot resolve INSEE → gazetteer.ErrInsufficientInputs (wrapped)
//   - URL builder rejects INSEE → gazetteer.ErrInsufficientInputs (wrapped)
//   - HTTP 5xx / transport / parse failure → gazetteer.ErrUpstreamUnavailable (wrapped)
//   - HTTP 4xx (other than 404) → gazetteer.ErrUpstreamPermanent (wrapped)
//
// Successful "no data" responses (LocService rendered "marché pas
// suffisamment actif") are NOT treated as errors — the Source returns
// a *Result whose IsEmpty()==true and the framework records
// StatusOKEmpty.
//
// Logging: emits one DEBUG log line per query via
// gazetteer.LoggerFrom(ctx) at the "locservice" component. The
// a downstream consumer adapter on top adds INFO once per work-unit.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if l.Address == "" && l.City == "" && l.Zip == "" && strings.TrimSpace(l.INSEE) == "" {
		return nil, fmt.Errorf("locservice: %w: no address/city/zip/insee", gazetteer.ErrInsufficientInputs)
	}

	insee, err := s.resolveINSEE(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("locservice: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	logementWanted := NormalizeLogement(MapTypeToLogement(l.PropertyType, l.Rooms))
	logementUsed := logementWanted
	fellBack := false

	u, err := URLForINSEE(insee, logementWanted)
	if err != nil {
		return nil, fmt.Errorf("locservice: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	body, err := s.fetch(ctx, u)
	if err != nil {
		return nil, err
	}

	parsed, err := Parse(body)
	if err != nil {
		return nil, fmt.Errorf("locservice: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}

	// Fallback: if a logement-specific call returned no data but a
	// logement was requested, retry with the commune-wide call.
	if !parsed.HasData && logementWanted != "" {
		logger.Debug("locservice.fallback_to_all_types",
			slog.String("insee", insee),
			slog.String("logement_requested", logementWanted),
		)
		u2, uerr := URLForINSEE(insee, "")
		if uerr == nil {
			body2, ferr := s.fetch(ctx, u2)
			if ferr == nil {
				if p2, perr := Parse(body2); perr == nil {
					parsed = p2
					logementUsed = ""
					fellBack = true
					u = u2
				}
			}
		}
	}

	out := BuildResult(parsed, fellBack)
	out.Evidence = Evidence{
		INSEE:         insee,
		Logement:      logementWanted,
		LogementUsed:  logementUsed,
		FellBack:      fellBack,
		CityLabel:     parsed.CityLabel,
		NoData:        !parsed.HasData,
		NoDataMessage: parsed.NoDataMessage,
		URL:           u,
	}
	return out, nil
}

// BuildResult renders a ParsedResult into the typed Result blob. Pure
// function — exposed so a downstream adapter can reuse the same
// projection without re-implementing the rules.
//
// Confidence calibration:
//
//   - high   : the page returned an arrow without falling back
//   - medium : the page returned an arrow but only after we widened to
//     logement="" (=> the requested type-specific signal is
//     unavailable, but the commune-wide signal exists)
//   - low    : the page returned the "marche pas suffisamment actif"
//     placeholder (no usable data)
func BuildResult(p ParsedResult, fellBack bool) *Result {
	out := &Result{
		ScoreScale:  "0..8 (LocService arrow position; 0=détendu, 8=extrêmement tendu)",
		Description: p.Description,
		SampleSize:  0,
		Confidence:  PickConfidence(p, fellBack),
		// SENTINEL: stamped on the no-data branch too (HasData=false)
		// for backwards-compat with payloads written before consumers
		// learned to gate on method.params.no_data. Consumers (e.g.
		// enrichview fillLocservice) MUST check the no_data flag before
		// rendering any "Tension <label>" pill ; otherwise the UI
		// surfaces a FALSE "Tension équilibré" signal on every commune
		// LocService said "marché pas suffisamment actif" for.
		// Overwritten with the real p.Label in the HasData=true branch
		// below.
		TensionLabel: string(LabelEquilibre),
	}
	if p.HasData {
		score := p.TensionScore
		out.TensionScore = &score
		out.SupplyScore = &score // alias: high score == landlord-friendly = high supply tightness
		out.TensionLabel = string(p.Label)
		out.SampleSize = 1
		if p.HasBudget {
			b := p.BudgetScore
			out.BudgetScore = &b
		}
	}
	return out
}

// PickConfidence implements the spec's confidence calibration. See
// BuildResult docstring for the rule table.
func PickConfidence(p ParsedResult, fellBack bool) string {
	if !p.HasData {
		return ConfidenceLow
	}
	if fellBack {
		return ConfidenceMedium
	}
	return ConfidenceHigh
}

// fetch performs the HTTP GET and translates transport / status-code
// failures to gazetteer sentinels.
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	client := s.opts.HTTPClient
	if client == nil {
		client = gazetteer.HTTPClientFrom(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("locservice: build request: %w", err)
	}
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("locservice: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("locservice: %w: http %d", gazetteer.ErrUpstreamUnavailable, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		// 404 = unknown INSEE / unknown logement. Surface as a
		// permanent upstream error so the caller does not retry; the
		// listing carried a coherent INSEE/logement combination but
		// LocService rejects it.
		return nil, fmt.Errorf("locservice: %w: http 404 (unknown insee/logement)", gazetteer.ErrUpstreamPermanent)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("locservice: %w: http %d", gazetteer.ErrUpstreamPermanent, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("locservice: %w: read body: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	return body, nil
}

// resolveINSEE returns the 5-digit commune code for the listing.
// Preference order:
//
//  1. Listing.INSEE when non-empty (trusted).
//  2. The Geocoder's result (when configured).
//  3. An error otherwise.
func (s *Source) resolveINSEE(ctx context.Context, l gazetteer.Listing) (string, error) {
	if i := strings.TrimSpace(l.INSEE); i != "" {
		return i, nil
	}
	if s.opts.Geocoder == nil {
		return "", errors.New("locservice: no geocoder configured")
	}
	q := banx.GeocodeQuery{
		Address: strings.TrimSpace(l.Address + " " + l.Zip + " " + l.City),
		City:    l.City,
		Zip:     l.Zip,
	}
	res, err := s.opts.Geocoder.Geocode(ctx, q)
	if err != nil {
		return "", err
	}
	if res.CityCode == "" {
		return "", errors.New("locservice: geocoder returned no citycode (INSEE)")
	}
	return res.CityCode, nil
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// no-data response still returns a non-nil *Result with IsEmpty() ==
// true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("locservice: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
