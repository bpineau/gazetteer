// Package delinquance is a gazetteer.Source exposing per-commune
// crime and security indicators recorded by police and gendarmerie.
//
// The data originate from the SSMSI (Service Statistique Ministériel
// de la Sécurité Intérieure) "bases statistiques communale" dataset
// on data.gouv.fr — the official État 4001 framework.
//
// For a rental investor this Source is a coarse first-order signal:
// communes with high burglary or vandalism rates carry more landlord
// risk (vacancy, churn, insurance premiums). The Source exposes a
// short list of indicators normalised to "per 1 000 inhabitants"
// rates for the latest reference year, plus a peer-relative risk
// flag.
//
// The Source is fully offline: the SSMSI extract ships embedded as a
// gzipped JSON under `data/`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := delinquance.NewSource(delinquance.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75119"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*delinquance.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune absent from the SSMSI dataset")
//	    return
//	}
//	fmt.Printf("population=%d risk=%s\n", r.Population, r.Flag)
//	fmt.Printf("burglaries per 1000 dwellings: %.2f\n",
//	    r.Rates["burglary"])
//	fmt.Printf("vandalism per 1000 inhabitants: %.2f\n",
//	    r.Rates["vandalism"])
package delinquance
