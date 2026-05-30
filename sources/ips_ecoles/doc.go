// Package ips_ecoles is a gazetteer.Source that returns the median
// Indice de Position Sociale (IPS) over the écoles primaires of a
// commune.
//
// The IPS is a per-establishment score published by the DEPP (Direction
// de l'évaluation, de la prospective et de la performance) of the
// Ministry of National Education. It synthesises a battery of socio-
// economic attributes of pupils' families (parents' professional
// category, education level, household equipment, …) into a single
// number centred near 100 nationally. A high IPS marks a favourised
// catchment, a low IPS a precarious one.
//
// The signal matters for a rental investor because the IPS of the
// neighbourhood school is a leading proxy for:
//
//   - the social composition of the LOCAL catchment area (much finer
//     than commune-wide income statistics in mixed cities) ;
//   - the demand profile for primary residents — family stability,
//     tenant covenant strength, retention ;
//   - the school-zoning premium (parents picking apartments to qualify
//     for a particular école élémentaire).
//
// Granularity is the commune (5-digit INSEE), INCLUDING the per-
// arrondissement codes for Paris (75101..75120), Lyon (69381..69389)
// and Marseille (13201..13216). The Source does NOT fold
// arrondissements — this is a load-bearing feature: among the
// commune-level signals in the gazetteer (rpls, chomage, filosofi,
// delinquance, qpv, vacance, ips_ecoles), THIS is the only
// one that yields a real PER-ARRONDISSEMENT reading for the three big
// PLM cities. Paris 1er IPS ≈ 130 ; Paris 18e IPS ≈ 104 ; Paris 16e
// IPS ≈ 140 — these differences are crushed by every commune-key-
// folded source.
//
// The Source is fully offline: the merged dataset ships embedded
// under `data/ips_ecoles_communes.json.gz`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type is irrelevant — the IPS speaks to the catchment.
//
// Upstream data: data.gouv.fr DEPP per-school IPS dataset (resource
// r/896c2e97-6a64-4521-bcab-b5b0d3cf7065). Licence Ouverte 2.0.
// Vintage: rentrée scolaire 2024-2025 (latest in the dataset at build
// time). Re-run the build script
// (`/tmp/gazetteer-data/build_ips_ecoles.py`) to refresh the embedded
// blob against future vintages.
//
// METHODOLOGY — the per-commune IPS is the UNWEIGHTED MEDIAN of the
// per-school IPS values published for the commune. The upstream CSV
// does NOT publish per-school effectifs, so a weighted-by-effectif
// median is not available; the unweighted median is robust to
// outliers within a heterogenous catchment and stable across
// commune-level renamings.
//
// Tier thresholds — calibrated against the 2024-2025 distribution
// (national median ≈ 105):
//
//   - precaire  : median <  80  (≈  1 % of communes)
//   - mixte     : median ∈ [80, 95)   (≈ 16 %)
//   - moyen     : median ∈ [95, 120)  (≈ 72 %)
//   - favorise  : median ≥ 120        (≈ 11 %)
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := ips_ecoles.NewSource(ips_ecoles.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75118"}) // Paris 18e
//	if err != nil { log.Fatal(err) }
//	r := data.(*ips_ecoles.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune has no school in the DEPP dataset")
//	    return
//	}
//	fmt.Printf("IPS median: %.1f over %d schools (%s)\n",
//	    r.IPSMedian, r.SchoolCount, r.Tier)
package ips_ecoles

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceNone   = ""
)

// Tier is a coarse, distribution-relative bucket on the median IPS.
// Informative only — never folded into a score by this Source.
type Tier string

const (
	TierUnknown  Tier = "unknown"
	TierPrecaire Tier = "precaire"
	TierMixte    Tier = "mixte"
	TierMoyen    Tier = "moyen"
	TierFavorise Tier = "favorise"
)
