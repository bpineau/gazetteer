package sensible

// curatedZone is one hand-maintained circle entry: a documented sensitive
// perimeter the QRR polygons miss (or sharpen), approximated as a circle of
// RadiusM metres around (Lat, Lon). Every entry MUST cite its source in Note —
// this file is a curated overlay of OFFICIAL designations, not an opinion list.
type curatedZone struct {
	Name    string
	Kind    string // KindORCOD or KindCurated
	Dep     string
	Lat     float64
	Lon     float64
	RadiusM float64
	Note    string
}

// curatedZones is the overlay applied on top of the embedded QRR polygons.
//
// Today it carries the four ORCOD-IN — the copropriétés dégradées the State
// declared of national interest by décret en Conseil d'État, i.e. the
// best-documented "do not buy blind" perimeters in France (and the classic
// judicial-auction trap: lots there look cheap and carry crushing charges).
// Centers were geocoded on the quartier (BAN); radii approximate the decree
// perimeters.
//
// Adding an entry: official designations only (a décret, a ZSP order, a
// préfecture act…), with the reference in Note.
var curatedZones = []curatedZone{
	{
		Name: "Clichy-sous-Bois — Bas Clichy / Le Chêne Pointu",
		Kind: KindORCOD,
		Dep:  "93",
		Lat:  48.9023, Lon: 2.5457, RadiusM: 800,
		Note: "ORCOD-IN — copropriétés très dégradées, expropriation publique en cours (décret n° 2015-99 du 28 janvier 2015)",
	},
	{
		Name: "Grigny — Grigny 2",
		Kind: KindORCOD,
		Dep:  "91",
		Lat:  48.6506, Lon: 2.3945, RadiusM: 600,
		Note: "ORCOD-IN — copropriétés très dégradées, expropriation publique en cours (décret n° 2016-1439 du 26 octobre 2016)",
	},
	{
		Name: "Mantes-la-Jolie — Le Val Fourré",
		Kind: KindORCOD,
		Dep:  "78",
		Lat:  48.9965, Lon: 1.6926, RadiusM: 900,
		Note: "ORCOD-IN — copropriétés très dégradées, expropriation publique en cours (décret n° 2020-8 du 6 janvier 2020)",
	},
	{
		Name: "Villepinte — Parc de la Noue",
		Kind: KindORCOD,
		Dep:  "93",
		Lat:  48.9580, Lon: 2.5407, RadiusM: 450,
		Note: "ORCOD-IN — copropriétés très dégradées, expropriation publique en cours (décret n° 2021-638 du 20 mai 2021)",
	},
}
