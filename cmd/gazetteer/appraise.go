package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/gazetteer"
)

// runAppraise implements `gazetteer appraise [--source ...] [--json]
// [--verbose] [--dump] <addr>`. Reuses the query pipeline (normalize +
// Collect) and then folds the Dossier through the three appraisal
// synthesisers: PricePerM2, RentValue, HazardProfile.
func runAppraise(ctx context.Context, args []string) error {
	q, err := parseQueryFlags("appraise", args)
	if err != nil {
		return err
	}
	dossier, err := executeQuery(ctx, q)
	if err != nil {
		return err
	}

	price := appraisal.PricePerM2(dossier)
	rent := appraisal.RentValue(dossier)
	hazard := appraisal.HazardProfile(dossier)

	if q.jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(appraisalEnvelope{
			Dossier: dossier,
			Price:   price,
			Rent:    rent,
			Hazard:  hazard,
		})
	}
	printDossierSummary(os.Stdout, dossier)
	fmt.Fprintln(os.Stdout)
	printAppraisal(os.Stdout, price, rent, hazard)
	return nil
}

// appraisalEnvelope is the JSON wire shape for `appraise --json`: the
// raw Dossier alongside the three synthesised views. Field names are
// snake_case for parity with the rest of the lib's JSON.
type appraisalEnvelope struct {
	Dossier gazetteer.Dossier            `json:"dossier"`
	Price   appraisal.PriceConsolidated  `json:"price"`
	Rent    appraisal.RentConsolidated   `json:"rent"`
	Hazard  appraisal.HazardConsolidated `json:"hazard"`
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
