package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// sourceDescriptor is the curated, machine-readable description of a Source:
// the prose an agent (or human) needs to use it correctly that cannot be
// derived by reflection. Keep it in sync with the source — the
// catalog-completeness test fails the build if a registered source has no
// descriptor (or a descriptor names an unregistered source).
type sourceDescriptor struct {
	// Summary is a one-line "what it provides".
	Summary string `json:"summary"`

	// Inputs lists the Listing-field requirements as typed clauses (see
	// inputClause). `query --explain` reasons over these directly, and the
	// JSON catalog renders each clause in its prose form. Tokens are drawn
	// from the canonical vocabulary (inputTokenPresent in explain.go) and
	// validated by test, so a typo cannot silently break the diagnosis.
	Inputs []inputClause `json:"inputs"`

	// Coverage is the geographic / scope limit, e.g. "national",
	// "Île-de-France", "17 agglos (not Paris)".
	Coverage string `json:"coverage"`

	// Feeds names the appraisal / zonescore contributions, if any.
	Feeds []string `json:"feeds,omitempty"`

	// Batch reports that the source package exports a batch-read path
	// (`Load(dir)` returning a per-process index with direct lookups,
	// e.g. dvfagg.Load(dir).Lookup(insee)) that skips the Listing/Query
	// machinery — for whole-territory screening. See docs/helpers.md.
	Batch bool `json:"batch,omitempty"`
}

// inputClause is one typed requirement on the Listing: satisfied when ANY
// of the AnyOf tokens is present (so "INSEE or address" is one clause).
// Optional clauses enrich the Result but never gate it — `--explain` must
// not report them as missing requirements.
type inputClause struct {
	AnyOf    []string
	Note     string // human qualifier, rendered in parentheses
	Optional bool
}

// need declares a required clause: at least one of tokens must be present.
func need(tokens ...string) inputClause { return inputClause{AnyOf: tokens} }

// optional declares a non-gating clause with a qualifier explaining what
// the extra input adds.
func optional(note string, tokens ...string) inputClause {
	return inputClause{AnyOf: tokens, Note: note, Optional: true}
}

// String renders the clause in the catalog's prose form: tokens joined by
// " or ", with the note in parentheses ("surface (for €-total)").
func (c inputClause) String() string {
	out := strings.Join(c.AnyOf, " or ")
	if c.Note != "" {
		out += " (" + c.Note + ")"
	}
	return out
}

// MarshalJSON emits the prose string, keeping the machine catalog's
// "inputs" format a plain string array.
func (c inputClause) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

// sourceDescriptors is the single source of truth for the curated catalog
// prose. The completeness test enforces that its key set equals the registered
// source set, so it cannot silently drift.
var sourceDescriptors = map[string]sourceDescriptor{
	"dvf": {
		Summary:  "Demandes de Valeurs Foncières — historical transaction price €/m² (median + quartiles).",
		Inputs:   []inputClause{need("property_type"), need("INSEE", "address"), optional("for €-total", "surface")},
		Coverage: "national", Feeds: []string{"appraisal.PricePerM2", "zonescore: rendement (price)"},
	},
	"oll": {
		Batch:   true,
		Summary: "Observed market rents (Observatoires Locaux des Loyers), median €/m²/month by zone × rooms.",
		Inputs:  []inputClause{need("INSEE"), need("rooms")}, Coverage: "17 agglomérations (Paris intra-muros excluded)",
		Feeds: []string{"appraisal.RentValue", "zonescore: rendement (rent)"},
	},
	"carteloyers": {
		Batch:   true,
		Summary: "National rent-observatory reference tiers (médiane €/m²/month CC + IC band).",
		Inputs:  []inputClause{need("INSEE"), need("rooms")}, Coverage: "national",
		Feeds: []string{"appraisal.RentValue"},
	},
	"dvfagg": {
		Batch:   true,
		Summary: "Per-commune DVF aggregate — median €/m² + quartiles (3-year window, apartment sales, offline).",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
		Feeds: []string{"prospection: commune-level price benchmark"},
	},
	"encadrement": {
		Batch:    true,
		Summary:  "Legal rent-control caps (loyer de référence + majoré).",
		Inputs:   []inputClause{need("zip", "INSEE"), need("property_type"), need("rooms"), optional("for 93", "lat/lon")},
		Coverage: "Paris + Plaine Commune & Est Ensemble (93) + Lyon/Villeurbanne",
		Feeds:    []string{"appraisal.RentValue"},
	},
	"taxefonciere": {
		Batch:   true,
		Summary: "Estimated annual taxe foncière (DGFiP voted TFPB/TEOM rates × valeur locative proxy × surface).",
		Inputs:  []inputClause{need("INSEE"), need("surface")}, Coverage: "national",
		Feeds: []string{"zonescore: fiscalité"},
	},
	"filosofi": {
		Batch:   true,
		Summary: "INSEE Filosofi income — median revenu disponible + minima-sociaux %, by commune.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
		Feeds: []string{"zonescore: solvabilité (commune income)"},
	},
	"filoiris": {
		Batch:   true,
		Summary: "INSEE Filosofi income at IRIS (sub-commune) level — median + taux de pauvreté + Gini.",
		Inputs:  []inputClause{need("Listing.IRIS")}, Coverage: "national dataset; effective coverage Île-de-France (the only IRIS resolver is IDF-scoped)",
		Feeds: []string{"zonescore: solvabilité (IRIS income, preferred over filosofi)"},
	},
	"logiris": {
		Batch:   true,
		Summary: "INSEE census housing structure at IRIS — renter share, social-housing share, vacancy.",
		Inputs:  []inputClause{need("Listing.IRIS")}, Coverage: "Île-de-France",
		Feeds: []string{"zonescore: tension (IRIS vacancy + renter depth, preferred over vacance)"},
	},
	"chomage": {
		Batch:   true,
		Summary: "INSEE local unemployment rate by zone d'emploi vs national (quarterly).",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
		Feeds: []string{"zonescore: solvabilité (employment)"},
	},
	"delinquance": {
		Batch:   true,
		Summary: "SSMSI État 4001 per-commune crime indicators + a coarse risk flag.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
		Feeds: []string{"zonescore: sécurité"},
	},
	"locservice": {
		Summary: "Rental-market tension (supply tightness + tenant-budget scores, tension label).",
		Inputs:  []inputClause{need("address", "INSEE")}, Coverage: "national",
		Feeds: []string{"zonescore: tension (rental tension)"},
	},
	"vacance": {
		Batch:   true,
		Summary: "INSEE census demographic vacancy rate per commune/arrondissement.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
		Feeds: []string{"zonescore: tension (commune vacancy, fallback for logiris)"},
	},
	"rnc": {
		Batch:   true,
		Summary: "Copropriété context from the Registre National d'Immatriculation (syndic type, mandate status, lots, construction period, QPV, ANAH-aided). Low-confidence 'à vérifier' triage hint; NO hard distress flag — procedures/arrêtés are redacted upstream.",
		Inputs:  []inputClause{need("INSEE"), optional("improves unit matching", "lat/lon", "address")}, Coverage: "national",
		Feeds: []string{"buyer due-diligence: copropriété context (fiche only)"},
	},
	"lovac": {
		Batch:   true,
		Summary: "Per-commune fiscal vacancy rate from the LOVAC file (TLV perimeter).",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"nuisances": {
		Batch:   true,
		Summary: "IDF cumulative environmental-nuisance grid (noise + air, 500 m cells), tier + point-noir.",
		Inputs:  []inputClause{need("lat/lon")}, Coverage: "Île-de-France",
		Feeds: []string{"zonescore: accès (livability)"},
	},
	"osm_transit": {
		Batch:   true,
		Summary: "Walking distance to the nearest métro / RER / tram / Transilien station.",
		Inputs:  []inputClause{need("lat/lon")}, Coverage: "national (embedded catalog + live Overpass fallback)",
		Feeds: []string{"zonescore: accès (transit walk)"},
	},
	"gpe": {
		Batch:   true,
		Summary: "Nearest FUTURE Grand Paris Express station + line + distance (informational, not scored).",
		Inputs:  []inputClause{need("lat/lon")}, Coverage: "Île-de-France",
	},
	"georisques": {
		Summary: "Natural + technological hazards (flood, soil, industrial) at the coordinates.",
		Inputs:  []inputClause{need("lat/lon")}, Coverage: "national (live API)",
		Feeds: []string{"appraisal.HazardProfile"},
	},
	"catnat": {
		Batch:   true,
		Summary: "Per-commune history of recognised natural-disaster (CatNat) decrees + recent-frequency tier.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
		Feeds: []string{"appraisal.HazardProfile"},
	},
	"cdsr": {
		Batch:   true,
		Summary: "Nearby region-flagged distressed condominiums (IDF copro-difficulty signal).",
		Inputs:  []inputClause{need("lat/lon")}, Coverage: "Île-de-France",
	},
	"cadastre": {
		Summary: "Cadastral parcel id + contenance + viewer link (optional bâti emprise).",
		Inputs:  []inputClause{need("lat/lon")}, Coverage: "national (live API)",
	},
	"iris": {
		Batch:   true,
		Summary: "INSEE IRIS code/name/type at the address; also resolves Listing.IRIS for downstream sources.",
		Inputs:  []inputClause{need("lat/lon")}, Coverage: "Île-de-France",
	},
	"ademe": {
		Summary: "DPE (energy performance certificate) at the address — class, GES, dwelling attrs.",
		Inputs:  []inputClause{need("address"), optional("disambiguates multiple DPE", "surface")}, Coverage: "national (live API)",
	},
	"bdnb": {
		Summary: "Base de Données Nationale des Bâtiments — building age, type, dwelling count (opt-in).",
		Inputs:  []inputClause{need("address")}, Coverage: "national (per-key quota; opt-in)",
	},
	"dpedist": {
		Summary: "DPE class distribution per commune (passoire F+G share, efficient A+B share).",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"bpe": {
		Batch:   true,
		Summary: "INSEE BPE — curated commerce / health / services facility counts per commune.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"education": {
		Summary: "Count of open schools (école/collège/lycée) per commune.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national (live API)",
	},
	"ips_ecoles": {
		Batch:   true,
		Summary: "DEPP median IPS (social index) over a commune's écoles primaires + heterogeneity band.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"sensible": {
		Summary: "Flags addresses inside (or within 400 m of) the State's hardest-neighbourhood " +
			"perimeters: the 62 QRR police-priority zones (official polygons, far more selective " +
			"than QPV) and the 4 ORCOD-IN copropriétés dégradées (décrets). Informational, not scored.",
		Inputs: []inputClause{need("lat/lon")}, Coverage: "France (QRR national, ORCOD-IN Île-de-France)",
	},
	"rpls": {
		Batch:   true,
		Summary: "Share of logements locatifs sociaux (loi SRU) per commune.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"sitadel": {
		Batch:   true,
		Summary: "Per-commune housing-construction dynamics from SDES Sitadel: dwellings authorised (permits) + started, latest year, 5-year mean, collectif share and the per-year authorised series.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national (metropole + DOM)",
		Feeds: []string{"supply-side signal: new-housing pipeline (rents/prices headwind)"},
	},
	"qpv": {
		Batch: true,
		Summary: "With coordinates, answers via point-in-polygon whether THIS address is inside " +
			"a Quartier Prioritaire (QPV 2024 contours). Without coordinates, falls back to the " +
			"commune-level QPV list (lower confidence).",
		Inputs: []inputClause{need("INSEE"), optional("polygon containment + nearest-QPV hint", "lat/lon")}, Coverage: "France métropole + outre-mer (≈1660 QPV 2024 polygons, WGS84)",
	},
	"anct": {
		Batch:   true,
		Summary: "ANCT programmes — Action Cœur de Ville / Petites Villes de Demain / ORT (Denormandie).",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"cartofriches": {
		Batch:   true,
		Summary: "Cerema brownfield (friches) inventory aggregated per commune.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"zonageabc": {
		Batch:   true,
		Summary: "Official A bis / A / B1 / B2 / C tension zoning (Pinel/PTZ).",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
	"zonetendue": {
		Batch:   true,
		Summary: "\"Zone tendue\" + TLV-2013 + tendue-touristique fiscal flags.",
		Inputs:  []inputClause{need("INSEE")}, Coverage: "national",
	},
}

// catalogEntry is one row of the machine-readable catalog: the curated
// descriptor merged with the registry's version/default and the reflected
// Result schema.
type catalogEntry struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	// Default reports whether a default CLI run (no --source flag)
	// includes this source. CLI policy ONLY: factory.NewDefault wires
	// every source regardless (prune with factory.Options.Exclude).
	Default      bool            `json:"default"`
	Summary      string          `json:"summary"`
	Inputs       []inputClause   `json:"inputs"`
	Coverage     string          `json:"coverage"`
	Feeds        []string        `json:"feeds,omitempty"`
	Batch        bool            `json:"batch,omitempty"`
	ResultSchema json.RawMessage `json:"result_schema"`
}

// buildCatalog assembles the full catalog: every registered source, in name
// order, with its curated descriptor, its Version/Default from the CLI
// registry (best-effort — "version 0" when the source can't be built without
// live deps), and its reflected zero-value Result schema.
func buildCatalog() []catalogEntry {
	deps := &runtimeDeps{}
	if d, err := newRuntimeDeps(); err == nil {
		deps = d
	}
	byName := make(map[string]sourceFactory)
	for _, f := range sourceCatalog() {
		byName[f.Name] = f
	}

	names := gazetteer.RegisteredNames()
	sort.Strings(names)
	out := make([]catalogEntry, 0, len(names))
	for _, name := range names {
		desc := sourceDescriptors[name]
		e := catalogEntry{
			Name:     name,
			Summary:  desc.Summary,
			Inputs:   desc.Inputs,
			Coverage: desc.Coverage,
			Feeds:    desc.Feeds,
			Batch:    desc.Batch,
		}
		if f, ok := byName[name]; ok {
			e.Default = f.Default
			if src, err := f.Build(deps); err == nil {
				e.Version = src.Version()
			}
		}
		if factory := gazetteer.Lookup(name); factory != nil {
			if b, err := json.Marshal(factory()); err == nil {
				e.ResultSchema = b
			}
		}
		out = append(out, e)
	}
	return out
}

// runSourcesCatalog implements `gazetteer sources catalog [--json]`.
func runSourcesCatalog(args []string) error {
	fs := flag.NewFlagSet("sources catalog", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	var asJSON bool
	fs.BoolVar(&asJSON, "json", false, "Emit the full machine-readable catalog as indented JSON")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: gazetteer sources catalog [--json]")
		fmt.Fprintln(fs.Output(), "\nThe complete capability map of every source: inputs, coverage, what it")
		fmt.Fprintln(fs.Output(), "returns and which appraisal/zonescore axis it feeds. --json is the form")
		fmt.Fprintln(fs.Output(), "an AI agent should ingest; it also ships committed at docs/sources.json.")
	}
	if err := fs.Parse(args); err != nil {
		return errUsage
	}
	cat := buildCatalog()
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(cat)
	}
	for _, e := range cat {
		tag := ""
		if !e.Default {
			tag = " (opt-in)"
		}
		fmt.Printf("%-14s v%d%s\n    %s\n", e.Name, e.Version, tag, e.Summary)
		fmt.Printf("    inputs: %v | coverage: %s\n", e.Inputs, e.Coverage)
		if len(e.Feeds) > 0 {
			fmt.Printf("    feeds:  %v\n", e.Feeds)
		}
	}
	return nil
}
