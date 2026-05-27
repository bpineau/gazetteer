// Package georisques is a gazetteer.Source that pulls a consolidated
// environmental-risk report from Georisques BRGM (data.gouv).
//
// # Strategy
//
// Georisques exposes a public JSON API at `georisques.gouv.fr`. The
// endpoint
//
//	GET /api/v1/resultats_rapport_risque?latlon=<lon>,<lat>
//
// returns a single object describing 12 natural risks (inondation,
// seisme, retrait-gonflement des argiles, radon, …) and 6
// technological risks (ICPE, sols pollues, canalisations, …) at the
// queried point. No auth, no quota documented.
//
// **Important** : the `latlon` parameter takes longitude **first**,
// latitude second — counter-intuitively, the parameter name suggests
// the opposite order. Inverting silently returns an empty report
// (not a 400). See url.go.
//
// # Rhythm & rate-limit
//
// Standard rhythm. 2 req/s on `georisques.gouv.fr` (per-host). The
// 373-auction IDF corpus backfills in ~3 minutes.
package georisques

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrEmptyBody is returned by ParseReport when the input is empty or
// not parseable as JSON. The Source wraps it as
// gazetteer.ErrUpstreamUnavailable.
var ErrEmptyBody = errors.New("georisques: empty / unparseable body")

// Risk is the shape of every per-risk sub-object in the Georisques
// report. All five fields are optional — when `Present == false` the
// status fields are typically null and the JSON encoder leaves them
// empty.
type Risk struct {
	Present              bool   `json:"present"`
	Libelle              string `json:"libelle"`
	LibelleStatutCommune string `json:"libelleStatutCommune"`
	LibelleStatutAdresse string `json:"libelleStatutAdresse"`
	Specifique           string `json:"specifique"`
}

// RawAddress is the shape of the `adresse` object in the raw report.
// Internal — the Source's BuildResult projects it onto the public
// Address type (which uses snake_case JSON tags for persistence).
type RawAddress struct {
	Libelle   string  `json:"libelle"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// RawCommune is the shape of the `commune` object in the raw report.
// Internal — the Source's BuildResult projects it onto the public
// Commune type.
type RawCommune struct {
	Libelle    string `json:"libelle"`
	CodePostal string `json:"codePostal"`
	CodeInsee  string `json:"codeInsee"`
}

// Report is the full parsed shape returned by Georisques. We expose
// every field so the parser tests can lock the JSON decoding; the
// Source then builds a stable, flattened payload from this structure
// via BuildResult.
//
// Note: the API surfaces 12 natural risks + 6 technological risks. We
// list them all explicitly for readability.
type Report struct {
	Adresse RawAddress `json:"adresse"`
	Commune RawCommune `json:"commune"`
	URL     string     `json:"url"`

	RisquesNaturels struct {
		Inondation              Risk `json:"inondation"`
		RemonteeNappe           Risk `json:"remonteeNappe"`
		RisqueCotier            Risk `json:"risqueCotier"`
		Seisme                  Risk `json:"seisme"`
		MouvementTerrain        Risk `json:"mouvementTerrain"`
		ReculTraitCote          Risk `json:"reculTraitCote"`
		RetraitGonflementArgile Risk `json:"retraitGonflementArgile"`
		Avalanche               Risk `json:"avalanche"`
		FeuForet                Risk `json:"feuForet"`
		EruptionVolcanique      Risk `json:"eruptionVolcanique"`
		Cyclone                 Risk `json:"cyclone"`
		Radon                   Risk `json:"radon"`
	} `json:"risquesNaturels"`

	RisquesTechnologiques struct {
		ICPE                             Risk `json:"icpe"`
		Nucleaire                        Risk `json:"nucleaire"`
		CanalisationsMatieresDangereuses Risk `json:"canalisationsMatieresDangereuses"`
		PollutionSols                    Risk `json:"pollutionSols"`
		RuptureBarrage                   Risk `json:"ruptureBarrage"`
		RisqueMinier                     Risk `json:"risqueMinier"`
	} `json:"risquesTechnologiques"`
}

// ParseReport decodes the Georisques JSON object into the Report
// struct. Returns ErrEmptyBody on an unparseable / empty body.
//
// An empty top-level object `{}` parses successfully and yields a
// zero-valued Report — callers can still distinguish "no data" via
// the empty Adresse.Libelle / Commune.CodeInsee.
func ParseReport(body []byte) (*Report, error) {
	if len(body) == 0 {
		return nil, ErrEmptyBody
	}
	r := &Report{}
	if err := json.Unmarshal(body, r); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmptyBody, err)
	}
	return r, nil
}

// CanonicalNaturels returns the (key, Risk) pairs for the natural
// risks in stable, alphabetical-ish order. Used by BuildResult to
// flatten the report into a deterministic payload.
//
// The keys are snake_case stable identifiers — they MUST NOT change
// across versions because they are stored verbatim in the persisted
// payload JSON consumed by downstream UIs and the scoring pipeline.
func CanonicalNaturels(r *Report) []NamedRisk {
	if r == nil {
		return nil
	}
	n := r.RisquesNaturels
	return []NamedRisk{
		{"inondation", n.Inondation},
		{"remontee_nappe", n.RemonteeNappe},
		{"risque_cotier", n.RisqueCotier},
		{"seisme", n.Seisme},
		{"mouvement_terrain", n.MouvementTerrain},
		{"recul_trait_cote", n.ReculTraitCote},
		{"retrait_argile", n.RetraitGonflementArgile},
		{"avalanche", n.Avalanche},
		{"feu_foret", n.FeuForet},
		{"eruption_volcanique", n.EruptionVolcanique},
		{"cyclone", n.Cyclone},
		{"radon", n.Radon},
	}
}

// CanonicalTechnos returns the (key, Risk) pairs for the technological
// risks in stable order.
func CanonicalTechnos(r *Report) []NamedRisk {
	if r == nil {
		return nil
	}
	t := r.RisquesTechnologiques
	return []NamedRisk{
		{"icpe", t.ICPE},
		{"nucleaire", t.Nucleaire},
		{"canalisations_md", t.CanalisationsMatieresDangereuses},
		{"pollution_sols", t.PollutionSols},
		{"rupture_barrage", t.RuptureBarrage},
		{"risque_minier", t.RisqueMinier},
	}
}

// NamedRisk pairs a stable payload key with the parsed Risk.
type NamedRisk struct {
	Key  string
	Risk Risk
}

// StatutAdresseExisting reports whether the risk's "statut adresse"
// indicates an existing risk — i.e. a hit at the building's exact
// position rather than just at the commune level. Used to populate
// the `summary.red_flags` list.
//
// We treat the libelle as a hit when it contains "Existant" or
// "Concerne" (case-insensitive substring) but NOT when it starts with
// "Risque non" — that prefix marks an explicit "no risk at this
// address" verdict from BRGM.
func StatutAdresseExisting(r Risk) bool {
	return statutIsExisting(r.LibelleStatutAdresse)
}

// StatutCommuneExisting is the same logic at the commune scale —
// usually broader than the adresse-level verdict.
func StatutCommuneExisting(r Risk) bool {
	return statutIsExisting(r.LibelleStatutCommune)
}

func statutIsExisting(s string) bool {
	if s == "" {
		return false
	}
	if hasPrefixCI(s, "risque non") {
		return false
	}
	return containsCI(s, "existant") || containsCI(s, "concerne")
}

// containsCI is a tiny helper to avoid a strings.ToLower allocation on
// the hot path. The two needles we test are short ASCII substrings.
func containsCI(s, sub string) bool {
	// We only ever pass ASCII lowercase substrings (statutIsExisting
	// callers above), so a manual lower-bound iteration is fine.
	if len(sub) == 0 {
		return true
	}
	if len(s) < len(sub) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			c := s[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// hasPrefixCI is the case-insensitive ASCII prefix check.
func hasPrefixCI(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := range len(prefix) {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}

// BuildResult renders a Report into the typed Result blob. Pure
// function — exposed so callers can reuse the same projection without
// re-implementing the flattening rules.
//
// Stamps `level_used`:
//   - "address" when BRGM resolved the request down to the building's
//     exact location (Adresse.Libelle populated, statutAdresse
//     surfaceable).
//   - "commune" otherwise (only commune-scope data available;
//     statutAdresse fields are empty).
//
// Confidence calibration:
//   - high: at least one risk is present at any scale AND we have a
//     resolved address OR a commune INSEE — i.e. the request landed on
//     a real point inside France.
//   - medium: zero risks present (rare but legitimate — middle-of-
//     nowhere coords with full BRGM coverage but no live hazard).
//   - low: zero coords + zero INSEE (the BRGM bounced the request
//     entirely).
func BuildResult(r *Report) *Result {
	if r == nil {
		return &Result{
			Confidence: ConfidenceLow,
			LevelUsed:  LevelCommune,
			Summary:    Summary{RedFlags: []string{}},
		}
	}

	out := &Result{
		ReportURL: r.URL,
	}
	if r.Adresse.Libelle != "" {
		out.LevelUsed = LevelAddress
	} else {
		out.LevelUsed = LevelCommune
	}

	if r.Adresse.Libelle != "" || r.Adresse.Latitude != 0 || r.Adresse.Longitude != 0 {
		out.Address = &Address{
			Libelle: r.Adresse.Libelle,
			Lat:     r.Adresse.Latitude,
			Lon:     r.Adresse.Longitude,
		}
	}
	if r.Commune.CodeInsee != "" || r.Commune.CodePostal != "" || r.Commune.Libelle != "" {
		out.Commune = &Commune{
			Insee:      r.Commune.CodeInsee,
			CodePostal: r.Commune.CodePostal,
			Libelle:    r.Commune.Libelle,
		}
	}

	naturels := CanonicalNaturels(r)
	technos := CanonicalTechnos(r)

	naturelsMap := make(map[string]RiskBlob, len(naturels))
	natPresent := 0
	for _, nr := range naturels {
		naturelsMap[nr.Key] = RiskBlob{
			Present:       nr.Risk.Present,
			StatutCommune: nr.Risk.LibelleStatutCommune,
			StatutAdresse: nr.Risk.LibelleStatutAdresse,
			Specifique:    nr.Risk.Specifique,
		}
		if nr.Risk.Present {
			natPresent++
		}
	}
	out.Naturels = naturelsMap

	technosMap := make(map[string]RiskBlob, len(technos))
	techPresent := 0
	for _, tr := range technos {
		technosMap[tr.Key] = RiskBlob{
			Present:       tr.Risk.Present,
			StatutCommune: tr.Risk.LibelleStatutCommune,
			StatutAdresse: tr.Risk.LibelleStatutAdresse,
			Specifique:    tr.Risk.Specifique,
		}
		if tr.Risk.Present {
			techPresent++
		}
	}
	out.Technos = technosMap

	// Build red_flags : risks that are reported "Existant" /
	// "Concerne" at the **address** scale, in canonical order. This is
	// the operator-actionable subset.
	redFlags := []string{}
	for _, nr := range naturels {
		if StatutAdresseExisting(nr.Risk) {
			redFlags = append(redFlags, nr.Key)
		}
	}
	for _, tr := range technos {
		if StatutAdresseExisting(tr.Risk) {
			redFlags = append(redFlags, tr.Key)
		}
	}

	out.Summary = Summary{
		NaturelsPresentCount: natPresent,
		TechnosPresentCount:  techPresent,
		RedFlags:             redFlags,
	}

	switch {
	case natPresent+techPresent == 0:
		out.Confidence = ConfidenceMedium
	case r.Adresse.Latitude == 0 && r.Adresse.Longitude == 0 && r.Commune.CodeInsee == "":
		out.Confidence = ConfidenceLow
	default:
		out.Confidence = ConfidenceHigh
	}

	return out
}
