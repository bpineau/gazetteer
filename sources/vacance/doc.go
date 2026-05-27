// Package vacance is a gazetteer.Source for the LOVAC 2025 per-commune
// housing vacancy dataset, served from an embedded CSV.
//
// The Source needs Listing.INSEE. It returns the commune-wide
// vacancy rate plus a long-term-vacancy split. Property type is not
// consulted (vacance is a commune-wide metric). Missing communes
// (secret statistique) surface as IsEmpty()==true.
//
// Example:
//
//	src := vacance.NewSource(vacance.Options{})
//	r, err := src.Query(ctx, gazetteer.Listing{INSEE: "75101"})
package vacance
