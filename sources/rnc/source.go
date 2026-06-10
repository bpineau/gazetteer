package rnc

import (
	"context"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// rncDatasetURL is the public landing page used for WebURL until/unless a
// stable per-copropriété public deep-link is confirmed.
const rncDatasetURL = "https://www.data.gouv.fr/datasets/registre-national-dimmatriculation-des-coproprietes"

// Options configures an rnc Source.
type Options struct {
	// Index overrides the lazily-loaded singleton (tests inject a stub).
	Index *Index
	// DataDir is the gazetteer data directory; a refreshed artifact there
	// takes precedence over the embedded one. Empty means embedded only.
	DataDir string
}

// Source implements gazetteer.Source for the RNC copropriété context.
type Source struct {
	opts Options
}

// NewSource builds an rnc Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return Version }

// Datasets exposes the embedded extract to the refresh tooling.
func (s *Source) Datasets() []dataset.Set { return []dataset.Set{set} }

// Query implements gazetteer.Source: require INSEE, match by geo+street
// within the commune, and project to a Result. Buildings absent from the
// dataset surface as IsEmpty() == true.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	_ = ctx
	if strings.TrimSpace(l.INSEE) == "" {
		return nil, fmt.Errorf("rnc: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}
	idx := s.opts.Index
	if idx == nil {
		loaded, err := Load(s.opts.DataDir)
		if err != nil {
			return nil, fmt.Errorf("rnc: %w: load dataset: %w", gazetteer.ErrUpstreamPermanent, err)
		}
		idx = loaded
	}

	e, method, conf, dist := idx.match(l)
	ev := Evidence{
		INSEE:         strings.TrimSpace(l.INSEE),
		RowCount:      idx.Count(),
		DataVintage:   idx.Meta.DataVintage,
		MatchDistance: dist,
		VoieQuery:     normVoie(l.Address),
	}
	if l.Lat != nil {
		ev.QueryLat = *l.Lat
	}
	if l.Lon != nil {
		ev.QueryLon = *l.Lon
	}
	if e == nil {
		return &Result{Confidence: ConfidenceNone, MatchMethod: MatchNone, Evidence: ev}, nil
	}
	ev.VoieMatched = e.VoieNorm

	signals := amberSignals(e)
	return &Result{
		Immatriculation:    e.Immatriculation,
		NomUsage:           e.NomUsage,
		Attention:          len(signals) > 0,
		Signals:            signals,
		TypeSyndic:         e.TypeSyndic,
		MandatEnCours:      e.MandatEnCours,
		CoproAidee:         e.CoproAidee,
		LotsTotal:          e.LotsTotal,
		LotsHabitation:     e.LotsHabitation,
		ConstructionPeriod: e.ConstructionPeriod,
		SyndicatCooperatif: e.SyndicatCooperatif,
		ResidenceService:   e.ResidenceService,
		QPVCode:            e.QPVCode,
		QPVName:            e.QPVName,
		WebURL:             webURL(e.Immatriculation),
		MatchMethod:        method,
		Confidence:         conf,
		Evidence:           ev,
	}, nil
}

// amberSignals derives the low-confidence triage hints from the published
// governance fields. These are NOT a distress verdict (see doc.go).
func amberSignals(e *Entry) []string {
	var s []string
	m := strings.ToLower(e.MandatEnCours)
	if strings.Contains(m, "pas de mandat") || strings.Contains(m, "sans successeur") {
		s = append(s, "no_active_mandate")
	}
	switch strings.ToLower(strings.TrimSpace(e.TypeSyndic)) {
	case "", "non connu":
		s = append(s, "syndic_unknown")
	case "bénévole", "benevole":
		s = append(s, "syndic_benevole")
	}
	if e.CoproAidee {
		s = append(s, "copro_aidee")
	}
	return s
}

// webURL returns the public RNC reference for the copro. A stable per-copro
// public deep-link could not be confirmed, so we return the dataset page.
func webURL(imm string) string {
	_ = imm
	return rncDatasetURL
}

// Query is the atomic helper for callers who don't want the builder. A
// successful but empty response returns a non-nil *Result with IsEmpty().
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
