package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/appraisal/zonescore"
	"github.com/bpineau/gazetteer/gazetteer"
)

// zonescoreOptions resolves the --profile flag to a zonescore.Options slice
// (empty when unset → default yield-first weights). An unknown profile name
// is a usage error listing the valid presets.
func (q *queryFlags) zonescoreOptions(w streams) ([]zonescore.Options, error) {
	if q.profile == "" {
		return nil, nil
	}
	weights, ok := zonescore.WeightsForProfile(q.profile)
	if !ok {
		fmt.Fprintf(w.err, "gazetteer: unknown --profile %q (valid: %s)\n",
			q.profile, strings.Join(zonescore.ProfileNames(), ", "))
		return nil, errUsage
	}
	return []zonescore.Options{{Weights: weights}}, nil
}

// profileLabel names the weighting thesis a printed header announces. An
// unset --profile is the default yield-first preset; anything else must be
// named for what it is, or the header contradicts the weights the score was
// actually computed with.
func profileLabel(profile string) string {
	if profile == "" || profile == zonescore.ProfileYield {
		return "yield-first"
	}
	return profile
}

// runAppraise implements `gazetteer appraise [--source ...] [--json]
// [--verbose] [--explain] [--profile ...] <addr>`. Reuses the query
// pipeline (normalize + Collect) and then folds the Dossier through the
// three appraisal synthesisers (PricePerM2, RentValue, HazardProfile)
// plus the zone score.
//
// --explain swaps the per-source summary for the why-nothing diagnosis
// and keeps the synthesis: a thin appraisal is exactly the case where the
// operator needs to know which Sources came back empty, and why.
func runAppraise(ctx context.Context, args []string, w streams) error {
	q, err := parseQueryFlags(cmdAppraise, args, w)
	if err != nil {
		return err
	}
	zopts, err := q.zonescoreOptions(w) // validate --profile before the network work
	if err != nil {
		return err
	}
	dossier, err := executeQuery(ctx, q, w)
	if err != nil {
		return err
	}

	price := appraisal.PricePerM2(dossier)
	rent := appraisal.RentValue(dossier)
	hazard := appraisal.HazardProfile(dossier)
	score := zonescore.Compute(dossier, zopts...)

	if q.jsonOut {
		enc := json.NewEncoder(w.out)
		enc.SetIndent("", "  ")
		return enc.Encode(appraisalEnvelope{
			Dossier:   dossier,
			Price:     price,
			Rent:      rent,
			Hazard:    hazard,
			ZoneScore: score,
		})
	}
	printSourceBlock(w.out, dossier, q.explain)
	fmt.Fprintln(w.out)
	printAppraisal(w.out, price, rent, hazard)
	printZoneScore(w.out, score, profileLabel(q.profile))
	return nil
}

// appraisalEnvelope is the JSON wire shape for `appraise --json`: the
// raw Dossier alongside the three synthesised views. Field names are
// snake_case for parity with the rest of the lib's JSON.
type appraisalEnvelope struct {
	Dossier   gazetteer.Dossier            `json:"dossier"`
	Price     appraisal.PriceConsolidated  `json:"price"`
	Rent      appraisal.RentConsolidated   `json:"rent"`
	Hazard    appraisal.HazardConsolidated `json:"hazard"`
	ZoneScore zonescore.Score              `json:"zone_score"`
}

// printAppraisal renders the consolidated price / rent / hazard
// synthesis in a human-readable block under `printDossierSummary`'s
// per-source output.
func printAppraisal(out io.Writer, p appraisal.PriceConsolidated, r appraisal.RentConsolidated, h appraisal.HazardConsolidated) {
	fmt.Fprintln(out, "appraisal:")

	// price
	fmt.Fprintln(out, "  price:")
	if len(p.Inputs) == 0 {
		fmt.Fprintln(out, "    (no contributing sources)")
	} else {
		fmt.Fprintf(out, "    eur_per_m2     %.2f  (confidence=%s, %d input(s))\n",
			float64(p.EurPerM2Cents)/100.0, p.Confidence.String(), len(p.Inputs))
		for _, in := range p.Inputs {
			status := ""
			if in.Excluded {
				status = " EXCLUDED: " + in.ExcludedWhy
			}
			fmt.Fprintf(out, "      %-14s weight=%.2f  est=%.2f%s\n",
				in.Source, in.Weight,
				float64(in.Estimate.EurPerM2Cents)/100.0,
				status)
		}
	}

	// rent
	fmt.Fprintln(out, "  rent:")
	if len(r.Inputs) == 0 {
		fmt.Fprintln(out, "    (no contributing sources)")
	} else {
		fmt.Fprintf(out, "    eur_per_m2_mo  %.2f  (confidence=%s, %d input(s))",
			float64(r.EurPerM2Cents)/100.0, r.Confidence.String(), len(r.Inputs))
		if r.Bracket != "" {
			fmt.Fprintf(out, ", bracket=%s", r.Bracket)
		}
		fmt.Fprintln(out)
		for _, in := range r.Inputs {
			status := ""
			if in.Excluded {
				status = " EXCLUDED: " + in.ExcludedWhy
			}
			fmt.Fprintf(out, "      %-14s weight=%.2f  est=%.2f%s\n",
				in.Source, in.Weight,
				float64(in.Estimate.EurPerM2Cents)/100.0,
				status)
		}
	}

	// hazard
	fmt.Fprintln(out, "  hazard:")
	if len(h.Inputs) == 0 {
		fmt.Fprintln(out, "    (no contributing sources)")
		return
	}
	fmt.Fprintf(out, "    confidence     %s  (%d input(s))\n", h.Confidence.String(), len(h.Inputs))
	if len(h.NaturalRisks) > 0 {
		fmt.Fprintf(out, "    natural        %s\n", strings.Join(h.NaturalRisks, ", "))
	}
	if len(h.IndustrialRisks) > 0 {
		fmt.Fprintf(out, "    industrial     %s\n", strings.Join(h.IndustrialRisks, ", "))
	}
}

// printZoneScore renders the composite zone score and its explainable
// per-axis breakdown. profile is the label of the weight preset the score
// was computed with (see profileLabel): the header used to hard-code
// "yield-first" whatever --profile asked for.
func printZoneScore(out io.Writer, s zonescore.Score, profile string) {
	fmt.Fprintf(out, "  zone_score (%s):\n", profile)
	fmt.Fprintf(out, "    composite      %.1f / 100  (confidence=%s)\n", s.Composite, s.Confidence.String())
	for _, a := range s.Axes {
		if !a.Present {
			fmt.Fprintf(out, "      %-12s (n/a)        weight=%.2f\n", a.Name, a.Weight)
			continue
		}
		fmt.Fprintf(out, "      %-12s %5.1f        weight=%.2f  %s\n", a.Name, a.Value, a.Weight, a.Reason)
	}
}
