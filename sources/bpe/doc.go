// Package bpe is a gazetteer.Source that returns a curated subset of
// INSEE's Base Permanente des Équipements (BPE) 2024 counts for the
// commune of a Listing. Each commune is summarised by a small set of
// rental-investor-relevant buckets (post office, supermarket, bakery,
// general practitioner, pharmacy, school, daycare, train station,
// sports facility, …) rather than the full 188-type catalog.
//
// Why a curated subset:
//
//   - the full BPE counts table is > 1.2M rows; the per-commune
//     vector our downstream consumers actually use covers ~25 of the
//     188 types,
//   - the curated subset stays under 250 KB compressed and ships
//     embedded; no upstream call at Query time,
//   - the buckets line up with the "is this commune liveable for a
//     tenant?" checklist a marchand-de-bien runs by reflex.
//
// Bucket → BPE FACILITY_TYPE mapping (stable; see Bucket constants):
//
//	BucketPoste              : A206 Bureau de poste, A208 Agence postale
//	BucketGrandeSurface      : B104 Hyper / B105 Super (≥ 400 m²)
//	BucketSuperette          : B201 Supérette
//	BucketBoulangerie        : B207 Boulangerie-pâtisserie
//	BucketEcolePrimaire      : C107 Maternelle + C108 Élémentaire
//	BucketCollege            : C201
//	BucketLycee              : C301 général/techno + C302 professionnel
//	BucketStructureSante     : D106 Urgences + D108 Centre de santé + D113 Maison de santé pluridisciplinaire
//	BucketMedecinGeneraliste : D265
//	BucketInfirmier          : D281
//	BucketPharmacie          : D307
//	BucketCreche             : D502 Établissement d'accueil du jeune enfant + D504 Relais petite enfance
//	BucketGare               : E107 nationale + E108 régionale + E109 locale
//	BucketSportSalle         : F121 Salles multisports / gymnases
//	BucketSportPiscine       : F101 Bassin de natation
//	BucketSportTerrain       : F107 Terrain de tennis
//
// The Source is fully offline: the aggregate ships embedded as gzipped
// JSON under `data/`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type is irrelevant — equipment counts apply to the whole
// commune.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := bpe.NewSource(bpe.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75056"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*bpe.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no facilities indexed for this commune")
//	    return
//	}
//	fmt.Printf("%d facilities total\n", r.TotalFacilities)
//	fmt.Printf("  %d boulangeries, %d pharmacies, %d generalistes\n",
//	    r.Counts[bpe.BucketBoulangerie],
//	    r.Counts[bpe.BucketPharmacie],
//	    r.Counts[bpe.BucketMedecinGeneraliste])
package bpe

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Bucket is the curated bucket key the Source surfaces. Stable strings
// — downstream consumers can match on them as JSON map keys without
// importing this package.
type Bucket string

const (
	BucketPoste              Bucket = "poste"
	BucketGrandeSurface      Bucket = "grande_surface"
	BucketSuperette          Bucket = "superette"
	BucketBoulangerie        Bucket = "boulangerie"
	BucketEcolePrimaire      Bucket = "ecole_primaire"
	BucketCollege            Bucket = "college"
	BucketLycee              Bucket = "lycee"
	BucketStructureSante     Bucket = "structure_sante"
	BucketMedecinGeneraliste Bucket = "medecin_generaliste"
	BucketInfirmier          Bucket = "infirmier"
	BucketPharmacie          Bucket = "pharmacie"
	BucketCreche             Bucket = "creche"
	BucketGare               Bucket = "gare"
	BucketSportSalle         Bucket = "sport_salle"
	BucketSportPiscine       Bucket = "sport_piscine"
	BucketSportTerrain       Bucket = "sport_terrain"
)

// AllBuckets enumerates the curated buckets in stable display order
// (services first, then commerce, education, health, family,
// transport, sport). Useful for renderers that want a deterministic
// iteration.
var AllBuckets = []Bucket{
	BucketPoste,
	BucketGrandeSurface,
	BucketSuperette,
	BucketBoulangerie,
	BucketEcolePrimaire,
	BucketCollege,
	BucketLycee,
	BucketStructureSante,
	BucketMedecinGeneraliste,
	BucketInfirmier,
	BucketPharmacie,
	BucketCreche,
	BucketGare,
	BucketSportSalle,
	BucketSportPiscine,
	BucketSportTerrain,
}
