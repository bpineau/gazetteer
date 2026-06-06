package main

import "fmt"

// sourceDimensions groups the sources by the investor-evaluation DIMENSION they
// inform — discovery by intent ("I need rental-demand data → these sources"),
// the inverse of the by-source catalog. Every registered source appears in
// exactly one dimension (enforced by TestDimensionsComplete). Order within a
// dimension is rough relevance, best first.
var sourceDimensions = []struct {
	Dimension string
	Sources   []string
}{
	{"Prix / valeur", []string{"dvf", "dvfagg", "cadastre"}},
	{"Loyers", []string{"oll", "carteloyers", "encadrement"}},
	{"Demande locative / tension", []string{"locservice", "logiris", "vacance", "lovac"}},
	{"Offre / construction", []string{"sitadel"}},
	{"Solvabilité des locataires", []string{"filoiris", "filosofi", "chomage"}},
	{"Fiscalité", []string{"taxefonciere"}},
	{"Sécurité", []string{"delinquance"}},
	{"Transports", []string{"osm_transit", "gpe"}},
	{"Risques & nuisances", []string{"georisques", "catnat", "nuisances"}},
	{"Bâti & énergie", []string{"ademe", "bdnb", "dpedist"}},
	{"Copropriété", []string{"cdsr", "rnc"}},
	{"Équipements & écoles", []string{"bpe", "education", "ips_ecoles"}},
	{"Contexte social & réglementaire", []string{"rpls", "qpv", "anct", "cartofriches", "zonageabc", "zonetendue"}},
	{"Localisation", []string{"iris"}},
	{"Liens externes", []string{"links"}},
}

// runSourcesDimensions implements `gazetteer sources dimensions`: the sources
// grouped by investor-evaluation dimension, each with its one-line summary.
func runSourcesDimensions(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("sources dimensions takes no arguments")
	}
	for _, g := range sourceDimensions {
		fmt.Printf("%s\n", g.Dimension)
		for _, name := range g.Sources {
			fmt.Printf("    %-14s %s\n", name, sourceDescriptors[name].Summary)
		}
		fmt.Println()
	}
	return nil
}
