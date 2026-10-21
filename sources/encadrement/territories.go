package encadrement

// Rent-cap territories this Source ships no grille for.
//
// Article 140 of loi 2018-1021 (ELAN) caps rents only where a decree has
// delimited a territory and a prefectoral arrêté has set its grille. The
// Source embeds the grilles of Paris, Plaine Commune, Est Ensemble and Lyon /
// Villeurbanne; every other decreed territory is listed here, so an address
// there is reported as REGULATED with an unknown cap (RegulationNotCovered or
// RegulationPartialCommune, ConfidenceNone) rather than as unregulated, which
// would be the wrong answer: its rent IS capped, by a figure this package does
// not carry.
//
// The list is the full set of decreed territories outside the shipped ones,
// as published by ANIL ("Expérimentation de l'encadrement du niveau des loyers
// dans les zones tendues", read 2026-10-21,
// https://www.anil.org/aj-experimentation-encadrement-niveau-loyers-zones-tendues/)
// and service-public.gouv.fr (F1314, "Vérifié le 01 août 2026"), each commune
// list read off its decree on Légifrance. No territory of Aix-Marseille, nor
// any overseas one, has been decreed: the 2025-534 law reopened applications
// for the overseas collectivities until June 2027.
//
// INSEE codes are the current commune codes (the communes table guards their
// names in territories_test.go). Adding a grille for one of these territories
// means removing it from this list in the same change.
var uncoveredTerritories = []territory{
	{
		// Décret n° 2020-41 du 22 janvier 2020: "la commune de Lille", which
		// includes its communes associées Hellemmes and Lomme. Their former
		// INSEE codes (59298, 59355) are kept so a listing carrying one is
		// not read as unregulated.
		name:   "Lille (Hellemmes, Lomme)",
		decree: "décret n° 2020-41 du 22 janvier 2020",
		whole:  []string{"59350", "59298", "59355"},
	},
	{
		// Décret n° 2021-1144 du 2 septembre 2021: "la commune de Montpellier".
		name:   "Montpellier",
		decree: "décret n° 2021-1144 du 2 septembre 2021",
		whole:  []string{"34172"},
	},
	{
		// Décret n° 2021-1145 du 2 septembre 2021: "la commune de Bordeaux".
		name:   "Bordeaux",
		decree: "décret n° 2021-1145 du 2 septembre 2021",
		whole:  []string{"33063"},
	},
	{
		// Décret n° 2023-981 du 23 octobre 2023, article 1: 24 communes of the
		// communauté d'agglomération du Pays basque.
		name:   "Pays basque",
		decree: "décret n° 2023-981 du 23 octobre 2023",
		whole: []string{
			"64009", // Ahetze
			"64024", // Anglet
			"64035", // Arbonne
			"64038", // Arcangues
			"64065", // Ascain
			"64100", // Bassussarry
			"64102", // Bayonne
			"64122", // Biarritz
			"64125", // Bidart
			"64130", // Biriatou
			"64140", // Boucau
			"64189", // Ciboure
			"64249", // Guéthary
			"64260", // Hendaye
			"64282", // Jatxou
			"64304", // Lahonce
			"64317", // Larressore
			"64407", // Mouguerre
			"64483", // Saint-Jean-de-Luz
			"64496", // Saint-Pierre-d'Irube
			"64540", // Urcuit
			"64545", // Urrugne
			"64547", // Ustaritz
			"64558", // Villefranque
		},
	},
	{
		// Décret n° 2023-1046 du 16 novembre 2023: 13 communes of
		// Grenoble-Alpes Métropole in full (article 1) and 8 in part (article
		// 2), where the perimeter is the set of IRIS listed in the decree's
		// annex. That IRIS list is not transcribed: a partial commune is
		// reported as possibly regulated, whatever Listing.IRIS says.
		name:   "Grenoble-Alpes Métropole",
		decree: "décret n° 2023-1046 du 16 novembre 2023",
		whole: []string{
			"38057", // Bresson
			"38111", // Claix
			"38150", // Domène
			"38158", // Eybens
			"38170", // Le Fontanil-Cornillon
			"38179", // Gières
			"38229", // Meylan
			"38271", // Murianette
			"38309", // Poisat
			"38486", // Seyssins
			"38516", // La Tronche
			"38524", // Varces-Allières-et-Risset
			"38533", // Venon
		},
		partial: []string{
			"38151", // Échirolles
			"38169", // Fontaine
			"38185", // Grenoble
			"38317", // Le Pont-de-Claix
			"38382", // Saint-Égrève
			"38421", // Saint-Martin-d'Hères
			"38474", // Sassenage
			"38485", // Seyssinet-Pariset
		},
	},
}

// territory is one decreed rent-cap territory whose grille is not shipped.
type territory struct {
	// name is the human-readable territory label surfaced in Result.Territory.
	name string
	// decree names the decree that delimits it, for the Evidence note.
	decree string
	// whole lists the INSEE codes of the communes covered in full.
	whole []string
	// partial lists the INSEE codes of the communes covered in part.
	partial []string
}

// UncoveredTerritory reports whether the commune insee lies in a decreed
// rent-cap territory whose grille this Source does not ship. name is the
// territory's label ("Bordeaux", "Pays basque"), and partial is true when the
// decree covers only part of the commune (some Grenoble-Alpes Métropole
// communes), in which case the address may or may not be capped. ok is false
// for every other commune, including the shipped ones (Paris, Lyon /
// Villeurbanne, Plaine Commune, Est Ensemble).
func UncoveredTerritory(insee string) (name string, partial bool, ok bool) {
	t, partial, ok := lookupTerritory(insee)
	if !ok {
		return "", false, false
	}
	return t.name, partial, true
}

func lookupTerritory(insee string) (territory, bool, bool) {
	if insee == "" {
		return territory{}, false, false
	}
	for _, t := range uncoveredTerritories {
		for _, c := range t.whole {
			if c == insee {
				return t, false, true
			}
		}
		for _, c := range t.partial {
			if c == insee {
				return t, true, true
			}
		}
	}
	return territory{}, false, false
}
