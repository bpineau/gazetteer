package taxefonciere

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "taxefonciere"

// sourceVersion bumps when the Source's internal logic changes.
//
// v1 combines the legacy per-m² ratio fallback with taxe_fonciere_v2
// (DGFiP taux votés + VLC proxy + TEOM breakdown). V2 runs first; V1
// only when V2 misses on both commune and département.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a taxefonciere Source.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a stub
	// here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the taxe foncière estimator
// using the DGFiP voted TFPB/TEOM rates (V2) with a legacy per-m²
// ratio fallback (V1). Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a taxefonciere Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE (5-digit) and Listing.SurfaceM2 > 0;
//     emit gazetteer.ErrInsufficientInputs otherwise.
//  2. Try V2 first: TFPB & TEOM × VLC tariff × surface × abattement.
//     Commune hit → ConfidenceHigh; dept fallback → ConfidenceMedium.
//  3. When V2 has no signal at all (commune + dept both missing), try
//     V1: per-m² ratio × surface. Commune hit → ConfidenceMedium;
//     dept fallback → ConfidenceLow.
//  4. When neither V1 nor V2 has any signal: return a result with
//     IsEmpty()==true and ConfidenceNone.
//
// Property type is not consulted — the TF estimate applies to the
// habitable surface regardless of apartment vs house. Callers that
// care can filter via Listing.PropertyType themselves.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("taxefonciere: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}
	surface := 0.0
	if l.SurfaceM2 != nil {
		surface = *l.SurfaceM2
	}
	if surface <= 0 {
		return nil, fmt.Errorf("taxefonciere: %w: surface_m2 required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("taxefonciere: load dataset: %w", err)
		}
		idx = loaded
	}

	ev := Evidence{
		INSEE:         insee,
		SurfaceM2:     surface,
		VLCAbattement: idx.V2.Meta.VLCAbattement,
		V2DataYear:    idx.V2.Meta.DataYear,
	}

	// V2 first.
	if entry, fb, ok := idx.V2.LookupV2(insee); ok {
		vlc := idx.V2.Meta.VLCTariffEURPerM2
		abat := idx.V2.Meta.VLCAbattement
		base := vlc * surface * abat
		tfpb := entry.TFPBPct / 100.0 * base
		teom := entry.TEOMPct / 100.0 * base
		conf := ConfidenceHigh
		path := "v2_commune"
		if fb {
			conf = ConfidenceMedium
			path = "v2_dept"
		}
		ev.PathUsed = path
		return &Result{
			EstimatedEURPerYear: tfpb,
			TEOMEURPerYear:      teom,
			TauxTFPBApplied:     entry.TFPBPct,
			TauxTEOMApplied:     entry.TEOMPct,
			VLEURPerM2:          vlc,
			UsedDeptFallback:    fb,
			Confidence:          conf,
			Evidence:            ev,
		}, nil
	}

	// V2 missed completely — try V1 (legacy per-m² ratio).
	if vl, fb, ok := idx.V1.LookupV1(insee); ok {
		yearly := vl * surface
		conf := ConfidenceMedium
		path := "v1_commune"
		if fb {
			conf = ConfidenceLow
			path = "v1_dept"
		}
		ev.PathUsed = path
		return &Result{
			EstimatedEURPerYear: yearly,
			VLEURPerM2:          vl,
			UsedDeptFallback:    fb,
			UsedV1Fallback:      true,
			Confidence:          conf,
			Evidence:            ev,
		}, nil
	}

	// Neither V1 nor V2 has any data. Emit a none-confidence result.
	return &Result{
		UsedV1Fallback: true,
		Confidence:     ConfidenceNone,
		Evidence:       ev,
	}, nil
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("taxefonciere: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
