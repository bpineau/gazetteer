package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

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

	// Inputs lists the Listing fields the Source REQUIRES (an empty/absent one
	// yields ErrInsufficientInputs or an empty Result). Use the cheat-sheet
	// vocabulary: "INSEE", "lat/lon", "Listing.IRIS", "surface", "rooms",
	// "property_type", "address".
	Inputs []string `json:"inputs"`

	// Coverage is the geographic / scope limit, e.g. "national",
	// "Île-de-France", "17 agglos (not Paris)".
	Coverage string `json:"coverage"`

	// Feeds names the appraisal / zonescore contributions, if any.
	Feeds []string `json:"feeds,omitempty"`
}

// sourceDescriptors is the single source of truth for the curated catalog
// prose. The completeness test enforces that its key set equals the registered
// source set, so it cannot silently drift.
var sourceDescriptors = map[string]sourceDescriptor{
	"dvf": {
		Summary:  "Demandes de Valeurs Foncières — historical transaction price €/m² (median + quartiles).",
		Inputs:   []string{"property_type", "INSEE or address", "surface (for €-total)"},
		Coverage: "national", Feeds: []string{"appraisal.PricePerM2", "zonescore: rendement (price)"},
	},
	"oll": {
		Summary: "Observed market rents (Observatoires Locaux des Loyers), median €/m²/month by zone × rooms.",
		Inputs:  []string{"INSEE", "rooms"}, Coverage: "17 agglomérations (Paris intra-muros excluded)",
		Feeds: []string{"appraisal.RentValue", "zonescore: rendement (rent)"},
	},
	"carteloyers": {
		Summary: "National rent-observatory reference tiers (médiane €/m²/month CC + IC band).",
		Inputs:  []string{"INSEE", "rooms"}, Coverage: "national",
		Feeds: []string{"appraisal.RentValue"},
	},
	"encadrement": {
		Summary:  "Legal rent-control caps (loyer de référence + majoré).",
		Inputs:   []string{"zip or INSEE", "property_type", "rooms", "lat/lon (for 93)"},
		Coverage: "Paris + Plaine Commune & Est Ensemble (93) + Lyon/Villeurbanne",
		Feeds:    []string{"appraisal.RentValue"},
	},
	"taxefonciere": {
		Summary: "Estimated annual taxe foncière (DGFiP voted TFPB/TEOM rates × valeur locative proxy × surface).",
		Inputs:  []string{"INSEE", "surface"}, Coverage: "national",
		Feeds: []string{"zonescore: fiscalité"},
	},
	"filosofi": {
		Summary: "INSEE Filosofi income — median revenu disponible + minima-sociaux %, by commune.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
		Feeds: []string{"zonescore: solvabilité (commune income)"},
	},
	"filoiris": {
		Summary: "INSEE Filosofi income at IRIS (sub-commune) level — median + taux de pauvreté + Gini.",
		Inputs:  []string{"Listing.IRIS"}, Coverage: "Île-de-France (IRIS of communes ≥5000 hab)",
		Feeds: []string{"zonescore: solvabilité (IRIS income, preferred over filosofi)"},
	},
	"logiris": {
		Summary: "INSEE census housing structure at IRIS — renter share, social-housing share, vacancy.",
		Inputs:  []string{"Listing.IRIS"}, Coverage: "Île-de-France",
		Feeds: []string{"zonescore: tension (IRIS vacancy + renter depth, preferred over vacance)"},
	},
	"chomage": {
		Summary: "INSEE local unemployment rate by zone d'emploi vs national (quarterly).",
		Inputs:  []string{"INSEE"}, Coverage: "national",
		Feeds: []string{"zonescore: solvabilité (employment)"},
	},
	"delinquance": {
		Summary: "SSMSI État 4001 per-commune crime indicators + a coarse risk flag.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
		Feeds: []string{"zonescore: sécurité"},
	},
	"locservice": {
		Summary: "Rental-market tension (supply tightness + tenant-budget scores, tension label).",
		Inputs:  []string{"address or INSEE"}, Coverage: "national",
		Feeds: []string{"zonescore: tension (rental tension)"},
	},
	"vacance": {
		Summary: "INSEE census demographic vacancy rate per commune/arrondissement.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
		Feeds: []string{"zonescore: tension (commune vacancy, fallback for logiris)"},
	},
	"lovac": {
		Summary: "Per-commune fiscal vacancy rate from the LOVAC file (TLV perimeter).",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"nuisances": {
		Summary: "IDF cumulative environmental-nuisance grid (noise + air, 500 m cells), tier + point-noir.",
		Inputs:  []string{"lat/lon"}, Coverage: "Île-de-France",
		Feeds: []string{"zonescore: accès (livability)"},
	},
	"osm_transit": {
		Summary: "Walking distance to the nearest métro / RER / tram / Transilien station.",
		Inputs:  []string{"lat/lon"}, Coverage: "national (embedded catalog + live Overpass fallback)",
		Feeds: []string{"zonescore: accès (transit walk)"},
	},
	"gpe": {
		Summary: "Nearest FUTURE Grand Paris Express station + line + distance (informational, not scored).",
		Inputs:  []string{"lat/lon"}, Coverage: "Île-de-France",
	},
	"georisques": {
		Summary: "Natural + technological hazards (flood, soil, industrial) at the coordinates.",
		Inputs:  []string{"lat/lon"}, Coverage: "national (live API)",
		Feeds: []string{"appraisal.HazardProfile"},
	},
	"catnat": {
		Summary: "Per-commune history of recognised natural-disaster (CatNat) decrees + recent-frequency tier.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
		Feeds: []string{"appraisal.HazardProfile"},
	},
	"cdsr": {
		Summary: "Nearby region-flagged distressed condominiums (IDF copro-difficulty signal).",
		Inputs:  []string{"lat/lon"}, Coverage: "Île-de-France",
	},
	"cadastre": {
		Summary: "Cadastral parcel id + contenance + viewer link (optional bâti emprise).",
		Inputs:  []string{"lat/lon"}, Coverage: "national (live API)",
	},
	"iris": {
		Summary: "INSEE IRIS code/name/type at the address; also resolves Listing.IRIS for downstream sources.",
		Inputs:  []string{"lat/lon"}, Coverage: "Île-de-France",
	},
	"ademe": {
		Summary: "DPE (energy performance certificate) at the address — class, GES, dwelling attrs.",
		Inputs:  []string{"address", "surface (disambiguates multiple DPE)"}, Coverage: "national (live API)",
	},
	"bdnb": {
		Summary: "Base de Données Nationale des Bâtiments — building age, type, dwelling count (opt-in).",
		Inputs:  []string{"address"}, Coverage: "national (per-key quota; opt-in)",
	},
	"dpedist": {
		Summary: "DPE class distribution per commune (passoire F+G share, efficient A+B share).",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"bpe": {
		Summary: "INSEE BPE — curated commerce / health / services facility counts per commune.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"education": {
		Summary: "Count of open schools (école/collège/lycée) per commune.",
		Inputs:  []string{"INSEE"}, Coverage: "national (live API)",
	},
	"ips_ecoles": {
		Summary: "DEPP median IPS (social index) over a commune's écoles primaires + heterogeneity band.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"rpls": {
		Summary: "Share of logements locatifs sociaux (loi SRU) per commune.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"qpv": {
		Summary: "Quartiers Prioritaires de la politique de la Ville membership for the commune.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"anct": {
		Summary: "ANCT programmes — Action Cœur de Ville / Petites Villes de Demain / ORT (Denormandie).",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"cartofriches": {
		Summary: "Cerema brownfield (friches) inventory aggregated per commune.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"zonageabc": {
		Summary: "Official A bis / A / B1 / B2 / C tension zoning (Pinel/PTZ).",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
	"zonetendue": {
		Summary: "\"Zone tendue\" + TLV-2013 + tendue-touristique fiscal flags.",
		Inputs:  []string{"INSEE"}, Coverage: "national",
	},
}

// catalogEntry is one row of the machine-readable catalog: the curated
// descriptor merged with the registry's version/default and the reflected
// Result schema.
type catalogEntry struct {
	Name         string          `json:"name"`
	Version      int             `json:"version"`
	Default      bool            `json:"default"`
	Summary      string          `json:"summary"`
	Inputs       []string        `json:"inputs"`
	Coverage     string          `json:"coverage"`
	Feeds        []string        `json:"feeds,omitempty"`
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
