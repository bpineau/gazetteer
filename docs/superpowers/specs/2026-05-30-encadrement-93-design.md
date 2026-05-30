# Design — Compléter `encadrement` : Plaine Commune + Est Ensemble (93)

Date : 2026-05-30
Statut : approuvé pour implémentation
Vertical : #1 de la roadmap d'amélioration gazetteer (priorité IDF hors Paris)

## Problème

La source `encadrement` couvre Paris (par arrondissement / zip) et Lyon-Villeurbanne
(par IRIS / INSEE). Le barème **Plaine Commune** est déjà embarqué mais le `Query` ne
résout jamais l'adresse vers une zone (il retourne `ConfidenceNone`), faute de mapping
adresse → zone. **Est Ensemble** est totalement absent.

Ce sont 18 communes du 93, en plein cœur de la zone de chasse « ≤ 1 h TC de Paris »,
où l'encadrement s'applique légalement (Plaine Commune depuis le 1ᵉʳ juin 2021, Est
Ensemble depuis le 1ᵉʳ décembre 2021) mais que la lib ne restitue pas.

## Données amont (vérifiées, licences ouvertes — data.gouv.fr)

Pour chaque EPT, deux artefacts open data :

1. **Barème** (loyers de référence / minoré / majoré par `zone × pièces × époque ×
   meublé × maison`), tableau JSON. Schéma **identique** entre PC et EE
   (`zone, nombre_de_piece, annee_de_construction, prix_min/med/max, meuble, maison`,
   décimales françaises `"16,4"`, pièce ouverte `"4 et plus"`).
   - Plaine Commune : déjà embarqué (millésime 2022), zones `{310,311,312,314,315,316,317,318}`.
   - Est Ensemble : `encadrements-est-ensemble-2023.json` (384 lignes), zones
     `{307,308,311,313,315,318}`.

2. **Zonage géographique** : `quartier-<ept>-geodata.json`, un GeoJSON
   `FeatureCollection` où **chaque feature = une commune** (`Polygon`/`MultiPolygon`)
   portant `INSEE_COM`/`com_code` + une propriété **`Zone`**.
   - Plaine Commune : 10 features / 9 communes. **Saint-Denis (93066) apparaît 2× →
     zones 311 et 312** (sous-communal).
   - Est Ensemble : 10 features / 9 communes. **Montreuil (93048) → zones 307 et 308**.

**Invariants vérifiés** : pour chaque EPT, l'ensemble des `Zone` du géo = l'ensemble des
`zone` du barème (match exact). Les numéros de zone **ne sont pas uniques entre EPT**
(311/315/318 existent des deux côtés) → la clé de jointure est **`(EPT, Zone)`**.

## Approches considérées

- **(A) Point-in-polygon sur géométrie embarquée — RETENUE.** Résolution précise, 100 %
  offline, indispensable pour Saint-Denis (PC) et Montreuil (EE) qui chevauchent 2 zones.
  Self-contained : ne dépend pas de l'infra IRIS (vertical #5).
- (B) IRIS → zone : couplerait ce vertical à l'infra IRIS, plus lourd, et le zonage
  officiel n'est pas strictement aligné IRIS. Rejeté.
- (C) Commune → zone unique : trivial mais **faux** pour Saint-Denis et Montreuil
  (zones aux loyers sensiblement différents). Rejeté.

## Conception

### `helpers/geopoly` (nouveau, réutilisable)

Petit paquet sans dépendance, testé, réutilisé ensuite par `cdsr` (#2) et `bruit` (#7) :

- `type Point struct{ Lon, Lat float64 }`
- `type Ring []Point`, `type Polygon []Ring` (ring 0 = contour, suivants = trous),
  `type MultiPolygon []Polygon`
- `func (MultiPolygon) Covers(Point) bool` — ray-casting règle pair/impair (les trous
  sont gérés naturellement par le comptage de croisements), avec pré-filtre par
  bounding-box. Construction `NewMultiPolygon` qui pré-calcule les bbox.

### Source `encadrement`

- Nouvelles `dataset.Set` : `setEstEnsemble` (barème), `setPlaineCommuneZones`,
  `setEstEnsembleZones` (géométrie compacte). 6 Sets au total.
- **Transforms de refresh** :
  - `transformEstEnsemble` : réutilise la logique `transformPlaineCommune` (schéma
    identique) — facteur commun extrait.
  - `transformZones` (paramétré PC/EE) : GeoJSON brut → artefact compact
    `[{ept, zone, insee, commune, rings:[[[lon,lat],…],…]}]` (toutes les autres
    propriétés droppées). Taille < 3 MiB → **embarqué**.
- **Index** étendu : `byEstEnsembleZone map[string][]Entry`, plus
  `eptZones []zonePolygon` (géométrie + EPT/zone/insee/commune) et
  `inseeToEPT map[string]string` dérivé du géo.
- **`Query` — nouvelle branche `resolve93`** (après Lyon, avant le `ConfidenceNone`
  final) :
  1. `inseeToEPT[insee]` absent → pas une commune encadrée 93 → continue.
  2. Coords présentes → point-in-polygon sur les polygones de l'EPT → zone exacte →
     `collapse` → `ConfidenceMedium`. Label = nom de commune (ex. « Saint-Denis »),
     zone + EPT en `Evidence`.
  3. Pas de coords (ou point hors polygones mais INSEE dans l'EPT) :
     - commune mono-zone → cette zone, `ConfidenceMedium` ;
     - commune multi-zone (Saint-Denis, Montreuil) → médiane sur ses zones,
       `ConfidenceLow` (ambiguïté honnête).
- Constantes : `ZoneSourceEstEnsemble = "est_ensemble"`, ajout de `ConfidenceLow = "low"`
  (+ mapping vers `appraisal.ConfidenceLow`).
- `sourceVersion` 1 → 2 (invalide le cache + gate de version datadir).

### Intégration appraisal

Aucune modification : dès que PC/EE renvoient un `Result` peuplé, le `RentEstimate()`
existant alimente `appraisal.RentValue` avec `Bracket = encadrement_<ept>_<zone>`. (La
refonte multi-source de `RentValue` est le vertical #4, séparé.)

### Millésimes

PC : paire 2022 (barème 2022 + géo 2022, zones cohérentes). EE : barème 2023 + géo 2022
(zones cohérentes). Millésime surfacé en doc + `Evidence`. La bascule PC→2023 (barème
publié en CSV) est un nice-to-have non bloquant, hors scope.

## Tests

- `geopoly` : unitaires (intérieur, extérieur, sommet/arête, trou, MultiPolygon, bbox).
- Résolution : coordonnées réelles → Saint-Denis zone correcte, Montreuil zone correcte,
  une commune mono-zone par EPT ; fallback INSEE sans coords (mono vs multi-zone) ;
  gating property-type ; `ConfidenceNone` hors périmètre.
- Transforms : round-trip GeoJSON→compact et barème EE→rows sur des échantillons testdata.

## Compatibilité encheres

`Result` inchangé ; ajouts additifs (constante ZoneSource, ConfidenceLow, branche de
résolution, bump de version). `encheres` doit compiler sans changement — à vérifier ;
corriger sinon.

## Definition of Done

`make fmt lint test build` vert ; API + code relus par un sub-agent strict ; testé sur
plusieurs adresses réelles des deux EPT avec des résultats sensés ; `encheres` corrigé si
nécessaire ; README + `doc/sources.md` + godoc à jour ; commit + push gazetteer (et
encheres si touché).
