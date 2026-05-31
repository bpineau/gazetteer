package links

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier.
const Name = "links"

// sourceVersion bumps when the set of links or their URL formats change.
//
// v1 builds map/imagery, price, hazard, urbanism and commune deep links from
// the listing's coordinates, INSEE and address.
const sourceVersion = 1

// Version exposes sourceVersion so callers can mirror it.
const Version = sourceVersion

// Options configures a links Source. The zero value is valid; this Source has
// no dataset and performs no HTTP, so DataDir is unused (kept only to match the
// uniform Source contract / factory wiring).
type Options struct {
	// DataDir is unused by this Source (no embedded dataset).
	DataDir string
}

// Source builds deep links for a listing. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a links Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source { return &Source{opts: opts} }

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. It builds every deep link whose required
// inputs are present. It needs at least Lat/Lon, INSEE, or an address;
// otherwise it emits gazetteer.ErrInsufficientInputs. Never touches the
// network.
func (s *Source) Query(_ context.Context, l gazetteer.Listing) (any, error) {
	out := build(l)
	if len(out) == 0 {
		return nil, fmt.Errorf("links: %w: needs Lat/Lon, INSEE, or an address", gazetteer.ErrInsufficientInputs)
	}
	ev := Evidence{INSEE: l.INSEE, Count: len(out)}
	if l.Lat != nil {
		ev.Lat = *l.Lat
	}
	if l.Lon != nil {
		ev.Lon = *l.Lon
	}
	return &Result{Links: out, Evidence: ev}, nil
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("links: typed result mismatch")
	}
	return res, nil
}

// build assembles every link whose required inputs are present, in a stable,
// concern-ordered sequence (map → prices → risks → urbanism → context).
func build(l gazetteer.Listing) []Link {
	var out []Link
	add := func(key, label, category, u string) {
		out = append(out, Link{Key: key, Label: label, Category: category, URL: u})
	}

	if l.Lat != nil && l.Lon != nil {
		lat := strconv.FormatFloat(*l.Lat, 'f', 6, 64)
		lon := strconv.FormatFloat(*l.Lon, 'f', 6, 64)

		// map & aerial imagery
		add("googlemaps", "Google Maps", CategoryMap,
			"https://www.google.com/maps/search/?api=1&query="+lat+","+lon)
		add("streetview", "Google Street View", CategoryMap,
			"https://www.google.com/maps/@?api=1&map_action=pano&viewpoint="+lat+","+lon)
		add("openstreetmap", "OpenStreetMap", CategoryMap,
			"https://www.openstreetmap.org/?mlat="+lat+"&mlon="+lon+"#map=18/"+lat+"/"+lon)
		add("geoportail", "Géoportail (ortho)", CategoryMap,
			"https://www.geoportail.gouv.fr/carte?c="+lon+","+lat+"&z=19")
		add("cadastre", "Cadastre (Géoportail)", CategoryMap,
			"https://www.geoportail.gouv.fr/carte?c="+lon+","+lat+"&z=19&l0=CADASTRALPARCELS.PARCELLAIRE_EXPRESS::GEOPORTAIL:OGC:WMTS(1)")
		add("remonterletemps", "IGN — Remonter le temps", CategoryMap,
			"https://remonterletemps.ign.fr/comparer?lon="+lon+"&lat="+lat+"&z=18")

		// prices & transactions
		add("pappersimmo", "Pappers Immobilier", CategoryPrices,
			"https://immobilier.pappers.fr/?lat="+lat+"&lon="+lon+"&z=18.00")
		add("dvf", "DVF — Demandes de valeurs foncières", CategoryPrices,
			"https://explore.data.gouv.fr/fr/immobilier?onglet=carte&lat="+lat+"&lng="+lon+"&zoom=18")

		// hazards: Géorisques report, enriched with INSEE/city when available
		geo := "https://www.georisques.gouv.fr/mes-risques/connaitre-les-risques-pres-de-chez-moi/rapport?lon=" + lon + "&lat=" + lat
		if l.INSEE != "" {
			geo += "&codeInsee=" + l.INSEE
		}
		if l.City != "" {
			geo += "&city=" + url.QueryEscape(l.City)
		}
		add("georisques", "Géorisques (rapport)", CategoryRisks, geo)

		// urbanism: PLU / zonage
		add("gpu", "Géoportail de l'Urbanisme (PLU)", CategoryUrbanism,
			"https://www.geoportail-urbanisme.gouv.fr/map/#tile=1&lon="+lon+"&lat="+lat+"&zoom=18")
	}

	// context: commune fact-sheet (INSEE) and a plain web search (address)
	if l.INSEE != "" {
		add("insee_commune", "INSEE — fiche commune", CategoryContext,
			"https://www.insee.fr/fr/metadonnees/cog/commune/COM"+l.INSEE)
	}
	if addr := addressLine(l); addr != "" {
		add("google_search", "Recherche web", CategoryContext,
			"https://www.google.com/search?q="+url.QueryEscape(addr))
	}

	return out
}

// addressLine renders a one-line address from the listing's address fields, or
// "" when none are set.
func addressLine(l gazetteer.Listing) string {
	parts := make([]string, 0, 3)
	if l.Address != "" {
		parts = append(parts, l.Address)
	}
	zipCity := strings.TrimSpace(l.Zip + " " + l.City)
	if zipCity != "" {
		parts = append(parts, zipCity)
	}
	return strings.Join(parts, ", ")
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
