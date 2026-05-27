// Package dvf is a gazetteer.Source that compiles a price-per-m²
// signal for an auction's commune from DVF Etalab open data.
//
// # API
//
// GET https://dvf-api.data.gouv.fr/mutations/{INSEE}/{section}. Both
// INSEE (5 digits) and section (5 chars, e.g. "000AD") are required.
//
// Migration history: switched from the legacy
// app.dvf.etalab.gouv.fr/api/mutations3/... endpoint to the production
// dvf-api.data.gouv.fr/mutations/... endpoint. Same path shape and same
// field names but the new API returns properly typed JSON (float/int/null
// vs strings / "None") and is fresher (rolling 5-year window updated
// more often). Envelope key changed `mutations` → `data`.
//
// # Strategy
//
// 4-tier zoom-out ladder:
//
//  1. address_radius — primary INSEE, kept to a 500 m disk around the
//     auction's (lat, lon).
//  2. commune        — primary INSEE only.
//  3. neighborhood   — communes within 5 km of the primary.
//  4. department     — every commune in primary's department.
//
// Each tier fetches every cadastral section's mutations, filters them
// with the anti-anomaly rules in matcher.go, and stops as soon as the
// post-filter sample crosses the level's MinSampleSize / MinSampleSizeAddressRadius.
//
// # Section catalog cache
//
// Cadastral sections (per INSEE) are looked up via cadastre.data.gouv.fr
// once and persisted in a kvcache.Cache. The package's SectionDiscoverer
// (sections.go) is the canonical façade for both production callers
// (wired against a persistent kvcache.Cache backend) and standalone
// users (wired against an in-memory cache).
//
// # Rhythm
//
// Standard rhythm. data.gouv.fr is CDN-fronted; the per-host budget set
// by the caller (10 req/s on dvf-api.data.gouv.fr today) is more than
// enough for one enricher.
package dvf

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/bpineau/gazetteer/helpers/circuit"
	"github.com/bpineau/gazetteer/helpers/httpx"
)

// APIBaseURL is the base URL of the DVF mutations endpoint.
// Exposed as a var so tests can swap it.
var APIBaseURL = "https://dvf-api.data.gouv.fr/mutations"

// APICallTimeout caps the wall-clock duration of a single
// GET /mutations/{insee}/{section} request.
//
// Historically (app.dvf.etalab.gouv.fr) we observed
// connections left ESTABLISHED for 12+ minutes on some commune/section
// pairs (e.g. 93027/000LZ). The new dvf-api.data.gouv.fr endpoint runs on
// data.gouv.fr's CDN-fronted production stack with no observed long tail
// Typically 180-280 ms steady, but the per-call ctx.WithTimeout is
// kept as belt-and-braces so a single slow section can never stall the
// whole enrichment.
//
// 30 s is comfortable: a healthy section returns in <1 s and a section
// that takes longer is almost certainly a soft-fail we should skip.
var APICallTimeout = 30 * time.Second

// Mutation is one row from the DVF API. We keep only the fields the
// matcher needs; unmodelled fields are ignored.
//
// Field types are native: `valeur_fonciere`,
// `surface_reelle_bati`, `surface_terrain`, `longitude`, `latitude` are
// float64 (nullable for surface*); `adresse_numero`,
// `nombre_pieces_principales`, `nombre_lots` are int. The legacy
// app.dvf.etalab.gouv.fr endpoint returned everything as strings (with
// `null` rendered as the literal string "None"); the new
// dvf-api.data.gouv.fr endpoint returns native JSON types. We store
// pointers for nullable numeric fields so the downstream filter can
// distinguish "0" from "missing".
type Mutation struct {
	IDMutation        string   `json:"id_mutation"`
	DateMutation      string   `json:"date_mutation"`
	NatureMutation    string   `json:"nature_mutation"`
	ValeurFonciere    *float64 `json:"valeur_fonciere"`
	AdresseNumero     *int     `json:"adresse_numero"`
	AdresseNomVoie    string   `json:"adresse_nom_voie"`
	CodePostal        string   `json:"code_postal"`
	CodeCommune       string   `json:"code_commune"`
	NomCommune        string   `json:"nom_commune"`
	CodeDepartement   string   `json:"code_departement"`
	IDParcelle        string   `json:"id_parcelle"`
	CodeTypeLocal     string   `json:"code_type_local"`
	TypeLocal         string   `json:"type_local"`
	SurfaceReelleBati *float64 `json:"surface_reelle_bati"`
	NombrePiecesPrinc *int     `json:"nombre_pieces_principales"`
	SurfaceTerrain    *float64 `json:"surface_terrain"`
	SectionPrefixe    string   `json:"section_prefixe"`
	Longitude         *float64 `json:"longitude"`
	Latitude          *float64 `json:"latitude"`
}

// Valeur returns valeur_fonciere as a float64, in euros (NOT cents).
// Returns 0 when the field is JSON null.
func (m Mutation) Valeur() float64 {
	if m.ValeurFonciere == nil {
		return 0
	}
	return *m.ValeurFonciere
}

// Surface returns surface_reelle_bati as a float64. 0 when null.
func (m Mutation) Surface() float64 {
	if m.SurfaceReelleBati == nil {
		return 0
	}
	return *m.SurfaceReelleBati
}

// API is the thin client for the DVF mutations endpoint.
type API struct {
	http    *httpx.Client
	circuit *circuit.TransportCircuit
}

// NewAPI wraps an httpx client. When tc is non-nil, every
// GetMutations call folds its transport-level outcome into the
// circuit's rolling counter (cf. circuit.TransportCircuit.Observe).
func NewAPI(c *httpx.Client, tc *circuit.TransportCircuit) *API {
	return &API{http: c, circuit: tc}
}

// MutationsResponse is the envelope returned by the DVF API.
//
// The dvf-api.data.gouv.fr endpoint wraps the rows in
// `{"data": [...]}` instead of the legacy `{"mutations": [...]}`. The
// Go field name `Data` is unchanged across the codebase via the
// migration; only the JSON tag differs.
type MutationsResponse struct {
	Data []Mutation `json:"data"`
}

// ErrSectionNotFound is returned when the API returns 404 for a
// (commune, section) pair. Callers may use it as the stop signal of the
// section discovery loop.
var ErrSectionNotFound = errors.New("dvf: section not found")

// GetMutations issues GET /mutations/{insee}/{section}. Returns
// ErrSectionNotFound when the API returns 404.
//
// A per-call ctx.WithTimeout(APICallTimeout) wraps the underlying GET so
// a single slow section can't hang the enrichment.
func (a *API) GetMutations(ctx context.Context, insee, section string) (MutationsResponse, error) {
	if a == nil || a.http == nil {
		return MutationsResponse{}, errors.New("dvf: nil http client")
	}
	// Pre-flight breaker check: once the shared circuit is tripped,
	// every further GET against the upstream is doomed and would
	// burn the httpx 5×exp-backoff retry tax before surfacing the
	// same error. Refuse outright so the per-commune / per-section
	// outer loops in fetchMutationsForCommunes bail in O(1).
	if a.circuit != nil && a.circuit.Tripped() {
		return MutationsResponse{}, fmt.Errorf("dvf: %w", circuit.ErrCircuitOpen)
	}
	u := fmt.Sprintf("%s/%s/%s",
		APIBaseURL,
		url.PathEscape(insee),
		url.PathEscape(section),
	)
	callCtx, cancel := context.WithTimeout(ctx, APICallTimeout)
	defer cancel()
	var r MutationsResponse
	err := a.http.GetJSON(callCtx, u, nil, &r)
	// Fold the transport outcome into the circuit BEFORE remapping 404 →
	// ErrSectionNotFound: 404 is application-level (section doesn't
	// exist), it is NOT a transport failure and Observe() filters it out
	// naturally; only DNS / dial / TLS / deadline tick the counter.
	a.circuit.Observe(err)
	if err != nil {
		if herr, ok := errors.AsType[*httpx.ErrHTTP](err); ok && herr.Status == 404 {
			return MutationsResponse{}, ErrSectionNotFound
		}
		return MutationsResponse{}, err
	}
	return r, nil
}
