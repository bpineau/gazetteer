package carteloyers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "carteloyers"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial port from internal/core/enrich/rental/carte_loyers.
//     Lookup keyed on (INSEE, typology) where typology is picked from
//     property_type + rooms; fallback to TypologyApartment when the
//     rooms bucket is empty for the commune.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source (e.g.
// encheridor's rental wrapper) can mirror it without reaching into
// the package internals.
const Version = sourceVersion

// Options configures a carteloyers Source. The zero value is usable:
// every embedded dataset is loaded lazily on the first Query call.
type Options struct {
	// Index overrides the lazily-loaded singleton. Tests inject a
	// stub here; production callers leave it nil.
	Index *Index
}

// Source implements gazetteer.Source for the ANIL / DHUP carte des
// loyers offline dataset. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a carteloyers Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Reject non-residential property types with
//     gazetteer.ErrUnsupportedPropertyType.
//  2. Require Listing.INSEE (5-digit). Without it the Source emits
//     gazetteer.ErrInsufficientInputs — the wrapper is responsible
//     for resolving INSEE from (zip, city).
//  3. Pick a typology (TypologyHouse / TypologyApt12 / TypologyApt3 /
//     TypologyApartment) from property_type + rooms.
//  4. Look up (INSEE, typology). On miss for the rooms-bucket
//     dataset, fall back to TypologyApartment (Evidence.FallbackToGeneric
//     = true).
//  5. Return (*Result, nil). Empty rows are surfaced as IsEmpty()
//     so the framework records Status == StatusOKEmpty.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	typ, ok := pickTypology(string(l.PropertyType), intDeref(l.Rooms))
	if !ok {
		return nil, fmt.Errorf("carteloyers: %w: %q", gazetteer.ErrUnsupportedPropertyType, l.PropertyType)
	}

	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("carteloyers: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load()
		if err != nil {
			return nil, fmt.Errorf("carteloyers: load dataset: %w", err)
		}
		idx = loaded
	}

	row, hit := idx.Lookup(insee, typ)
	fallback := false
	if !hit && (typ == TypologyApt12 || typ == TypologyApt3) {
		// Fall back to the generic apartment dataset when the requested
		// rooms-bucket is empty for this commune (rare; only fires when
		// the more granular dataset is missing the INSEE).
		row, hit = idx.Lookup(insee, TypologyApartment)
		if hit {
			typ = TypologyApartment
			fallback = true
		}
	}

	ev := Evidence{
		INSEE:             insee,
		PropertyType:      string(l.PropertyType),
		FallbackToGeneric: fallback,
	}
	if !hit {
		return &Result{
			Confidence: ConfidenceNone,
			Evidence:   ev,
		}, nil
	}

	ev.PredType = row.PredType
	ev.Department = row.Department

	return &Result{
		LoyerMedEURPerM2CC:  row.LoyerMedCC,
		LoyerLowEURPerM2CC:  row.LoyerLowerCC,
		LoyerHighEURPerM2CC: row.LoyerUpperCC,
		Typology:            typ,
		NbObservations:      row.NbObsCommune,
		Confidence:          classifyConfidence(row),
		Evidence:            ev,
	}, nil
}

// pickTypology selects the carte des loyers dataset for the given
// property_type + rooms count. Houses go to TypologyHouse; flats split
// on rooms (1-2 vs 3+) and fall back to the generic apartment dataset
// when rooms is unknown.
//
// Returns (typ, true) for residential property types, ("", false) for
// commercial / land / parking / unknown.
func pickTypology(propertyType string, rooms int) (Typology, bool) {
	switch normalizePropertyType(propertyType) {
	case "house":
		return TypologyHouse, true
	case "apartment":
		switch {
		case rooms >= 3:
			return TypologyApt3, true
		case rooms >= 1:
			return TypologyApt12, true
		default:
			return TypologyApartment, true
		}
	default:
		return "", false
	}
}

// normalizePropertyType folds the rental enricher's property_type
// vocabulary (flat / apartment / apt / house / maison) into the two
// buckets the carte des loyers dataset covers.
func normalizePropertyType(t string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "flat", "apartment", "apt", "appartement":
		return "apartment"
	case "house", "maison":
		return "house"
	default:
		return ""
	}
}

// classifyConfidence assigns a per-row confidence based on the carte
// des loyers prediction-quality metadata. The dataset documents
// `TYPPRED = "commune"` as the well-sampled tier (the model fitted
// against ≥ N observations of the commune itself) and `"maille"` as
// the borrowed-neighbour tier. Sample-size cutoffs follow ANIL's own
// guidance.
func classifyConfidence(row Row) string {
	switch strings.ToLower(strings.TrimSpace(row.PredType)) {
	case "commune":
		switch {
		case row.NbObsCommune >= 30:
			return ConfidenceHigh
		case row.NbObsCommune >= 10:
			return ConfidenceMedium
		default:
			return ConfidenceLow
		}
	default: // "maille", "voisinage", unknown — borrowed signal
		return ConfidenceLow
	}
}

// intDeref dereferences a *int into 0 when nil.
func intDeref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
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
		return nil, errors.New("carteloyers: typed result mismatch")
	}
	return res, nil
}

// From extracts the typed *Result from a Dossier. Returns (nil, false)
// when the source is absent, failed, or the Data does not match.
func From(d gazetteer.Dossier) (*Result, bool) {
	return gazetteer.Get[*Result](d, Name)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
