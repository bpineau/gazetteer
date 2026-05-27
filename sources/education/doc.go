// Package education provides a gazetteer Source that counts open
// public + private schools in the commune of a Listing, broken down by
// school type (école / collège / lycée / médico-social / autre).
//
// It hits the public Opendatasoft API hosted at
// `data.education.gouv.fr` for the `fr-en-annuaire-education` dataset
// — the canonical Annuaire de l'Éducation Nationale, updated daily by
// the Ministère de l'Éducation Nationale. One HTTP GET per Listing,
// no auth required, no documented rate limit.
//
// Why this matters for a real-estate / rental investor:
//
//   - presence of an école primaire keeps family demand high
//   - presence of a lycée stabilises late-teen / student demand
//   - REP / REP+ classified establishments correlate with social
//     tension — not surfaced here directly (the field is on every
//     row) but the raw count by type still informs neighbourhood
//     attractiveness
//
// Required Listing inputs:
//
//   - INSEE  (5-digit commune code). Without it the Source returns
//     gazetteer.ErrInsufficientInputs.
//
// Property type is irrelevant. The school catalog covers every French
// commune (including DOM-TOM).
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := education.NewSource(education.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75111"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*education.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no schools registered for this commune")
//	    return
//	}
//	fmt.Printf("%d schools total in commune:\n", r.NbTotal)
//	fmt.Printf("  %d écoles, %d collèges, %d lycées\n",
//	    r.NbEcole, r.NbCollege, r.NbLycee)
package education

// DefaultBaseURL is the Opendatasoft v2.1 endpoint root for the
// Annuaire de l'Éducation Nationale dataset. The Source appends the
// `where=` + `group_by=` query string at runtime.
const DefaultBaseURL = "https://data.education.gouv.fr/api/explore/v2.1/catalog/datasets/fr-en-annuaire-education/records"

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	// ConfidenceHigh : the underlying dataset is the legal Annuaire,
	// refreshed daily. We return high when at least one establishment
	// was found.
	ConfidenceHigh = "high"

	// ConfidenceNone : the commune was found but reports zero
	// establishments. Still a meaningful answer (the API responded
	// correctly) but the result is empty.
	ConfidenceNone = ""
)

// Type enumerates the establishment buckets the Source surfaces.
// Mirrors the dataset's `type_etablissement` field, with "Service
// Administratif" + null folded into TypeOther.
type Type string

const (
	TypeEcole     Type = "ecole"
	TypeCollege   Type = "college"
	TypeLycee     Type = "lycee"
	TypeMedicoSoc Type = "medico_social"
	TypeOther     Type = "other"
)
