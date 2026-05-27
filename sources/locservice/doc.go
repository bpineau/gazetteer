// Package locservice is a gazetteer.Source for the LocService
// Tensiomètre Locatif HTML page — a per-INSEE × typology rental
// market tension label (`tendu` / `équilibré` / `détendu`) plus a
// median rent reading.
//
// The Source resolves the listing's INSEE (preferring Listing.INSEE;
// falling back to a banx.Geocoder), maps the listing's
// property_type + rooms to a logement keyword, fetches the HTML page,
// parses it, and returns a *Result. When the logement-specific call
// returns no data the Source widens to the commune-wide call in a
// single retry and stamps Evidence.FellBack = true on success.
//
// Example:
//
//	src := locservice.NewSource(locservice.Options{
//	    BaseURL:  srv.URL,
//	    Geocoder: ban,
//	})
//	r, err := src.Query(ctx, listing)
package locservice
