package sensible

// zoneCommunes maps a QRR zone code (zoneRow.Code) to the INSEE codes of the
// communes its perimeter intersects — the commune-grain view that batch
// consumers (gazetteer/overview, screening tables) join on, where the
// point-in-polygon path has no coordinates.
//
// Generated offline from the official contours (2026-06): for each zone, the
// centroid plus ~16 evenly-spaced boundary vertices pulled 12 %% toward the
// centroid were kept only when strictly INSIDE the polygon (so a vertex lying
// exactly on a commune boundary can never flag the neighbouring commune), then
// reverse-geocoded against geo.api.gouv.fr/communes. Conservative by
// construction: a commune hosting only a boundary sliver of a zone may be
// absent; every listed commune genuinely contains zone area. The QRR list is
// frozen (2021) so this table is stable; TestZoneCommunesComplete trips if a
// refreshed artifact ever disagrees with it.
var zoneCommunes = map[string][]string{
	"06_1":  {"06088"},                   // Nice - L'Ariane/Les Moulins
	"13_1":  {"13055"},                   // Marseille - Saint-Charles
	"13_2":  {"13055"},                   // Marseille - Quartiers Nord (3e, 4e et 15e arrondissement)
	"16_1":  {"16015", "16374"},          // Angoulême/Soyaux - le champs de Manœuvre, Bel-Air/La Grand-Font/Basseau, La Grande Garenne
	"21_1":  {"21166", "21231"},          // Dijon - Chenôve/Grésilles
	"25_1":  {"25056"},                   // Besançon - Planoise
	"28_1":  {"28134", "28404"},          // Dreux/Vernouillet - Les Oriels/Dunant-Kennedy/Les Bates/La Tabellionne
	"30_1":  {"30189"},                   // Nîmes - Pissevin/Valdegour
	"31_1":  {"31555"},                   // Toulouse - le Mirail
	"31_2":  {"31555"},                   // Toulouse - Izards/Borderouge
	"33_1":  {"33063"},                   // Bordeaux - Bordeaux maritime
	"33_2":  {"33108", "33243", "33324"}, // Libourne/Castillon-la-Bataille/Pineuilh/Sainte-Foy-la-Grande
	"34_1":  {"34145", "34154"},          // Lunel/Mauguio
	"34_2":  {"34172"},                   // Montpellier - La Mosson/La Paillade
	"35_1":  {"35238"},                   // Rennes - Maurepas
	"37_1":  {"37122", "37233", "37261"}, // Tours/Saint-Pierre-des-Corps/Joué-lès-Tours - Sanitas/La Rabaterie/La Rabière
	"38_1":  {"38193", "38537", "38553"}, // L'Isle-d'Abeau/Villefontaine/La Verpillière
	"38_2":  {"38151", "38185", "38421"}, // Grenoble/Echirolles/St Martin d'Hères - La Villeneuve/Renaudie/Champberton
	"42_1":  {"42218"},                   // Saint-Etienne - Montchovet/Tarentaize Beaubrun/La Cotonne/Montreynaud
	"42_2":  {"42044", "42183"},          // La Ricamarie - Montrambert/Meline
	"44_1":  {"44109", "44162"},          // Nantes - Bellevue/Malakoff/Dervallières
	"51_1":  {"51454"},                   // Reims - Croix-Rouge/Wilson/Orgeval
	"57_1":  {"57227"},                   // Forbach - Wiesberg/Bellevue
	"59_1":  {"59512", "59599"},          // Roubaix/Tourcoing - Quartier Intercommunal Blanc Seau/Croix Bas Saint Pierre
	"59_2":  {"59392"},                   // Maubeuge - Quartier Sous-Le-Bois/L'Epinette
	"59_3":  {"59350"},                   // Lille - Moulins/Fives
	"60_1":  {"60175"},                   // Creil - Les Hauts de Creil
	"62_1":  {"62193"},                   // Calais - Beaumarais/Centre Ville
	"64_1":  {"64445"},                   // Pau - L'Ousse des Bois/Saragosse
	"67_1":  {"67218", "67482"},          // Strasbourg - Neuhof/Meinau
	"67_2":  {"67482"},                   // Strasbourg - Hautepierre/Cronenbourg
	"68_1":  {"68224"},                   // Mulhouse - Bourtzwiller
	"68_2":  {"68066"},                   // Colmar - Europe/St Vincent de Paul
	"69_1":  {"69259"},                   // Vénissieux - Les Mingettes
	"69_2":  {"69123"},                   // Lyon - 8e arrondissement
	"69_3":  {"69286"},                   // Rillieux la Pape - Ville nouvelle
	"69_4":  {"69256"},                   // Vaulx en Velin - Vaulx Nord
	"74_1":  {"74012"},                   // Annemasse - Perrier/Livron/Chateau Rouge
	"74_2":  {"74042", "74081", "74264"}, // Bonneville/Cluses/Marnaz/Scionzier
	"75_1":  {"75056"},                   // Paris - Barbès/La Chapelle
	"76_1":  {"76351"},                   // Le Havre - Mont Gaillard/Mare-Rouge
	"76_2":  {"76095", "76540"},          // Rouen - Les Hauts De Rouen
	"76_3":  {"76351"},                   // Le Havre - Aristide Briand/Rond Point/Cours de la Republique
	"77_1":  {"77337", "77468"},          // Torcy/Noisiel - ZSP Torcy/cours des Roches/cours du Luzard
	"78_1":  {"78440"},                   // Les Mureaux - Gare/Cité Renault/Bougimont/Vigne Blanche/Les Musiciens/Bécheville
	"78_2":  {"78621"},                   // Trappes - Les Merisiers
	"83_1":  {"83126"},                   // La Seyne-sur-Mer
	"83_2":  {"83137"},                   // Toulon - Beaucaire/Pontcarral/Sainte-Musse
	"91_1":  {"91286", "91687"},          // Grigny - La Grande Borne/Grigny II
	"91_2":  {"91174"},                   // Corbeil-Essonnes - Les tarterêts
	"92_1":  {"92004", "92025", "92036"}, // Gennevilliers/Colombes/Asnières/Nanterre - ZSP Boucle Nord/ZSP Petit Colombes
	"93_1":  {"93001"},                   // Aubervilliers - Villette/Quatre Chemins
	"93_2":  {"93066"},                   // Saint-Denis
	"93_3":  {"93005", "93071"},          // Aulnay-sous-Bois/Sevran - Gros-Saule/Beudottes
	"93_4":  {"93027"},                   // La Courneuve
	"93_5":  {"93070"},                   // Saint-Ouen
	"94_1":  {"94017", "94019"},          // Champigny-sur-Marne - ZSP Champigny-sur-Marne/Les Mordacs
	"95_1":  {"95250", "95351"},          // Fosses/Louvres
	"95_2":  {"95018"},                   // Argenteuil - centre ville
	"95_3":  {"95268", "95585"},          // Sarcelles/Garges-lès-Gonesse - Les Lochères/Dame Blanche
	"976_1": {"97615"},                   // Pamandzi
	"988_1": {"98818"},                   // Nouméa - Tindu/Montravel-Pierre-Lenquette
}
