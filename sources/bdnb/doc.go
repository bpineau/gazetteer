// Package bdnb is a gazetteer.Source for the Base de Données Nationale
// des Bâtiments (BDNB) `batiment_groupe_complet` PostgREST endpoint.
//
// The Source resolves the listing's 5-digit INSEE via the BAN cascade
// (banx.INSEEResolver), queries BDNB with an ILIKE address pattern
// over BDNB rows for that INSEE, picks the most likely candidate and
// returns a *Result carrying building age, construction class,
// dwelling count and parcel surface.
//
// Quota: BDNB enforces a rolling 10 000-request budget per API key,
// surfaced via the `x-quota-remaining` response header and HTTP 429
// once the budget is gone. Wire a helpers/circuit.HTTPFetcher to
// trip the breaker on either signal.
//
// Example:
//
//	src := bdnb.NewSource(bdnb.Options{
//	    BaseURL:  srv.URL,    // optional, defaults to package var BaseURL
//	    Geocoder: ban,        // banx.Geocoder (forward + reverse cascade)
//	})
//	r, err := src.Query(ctx, listing)
package bdnb
