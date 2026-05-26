// Package fraddr is the canonical home for the small French
// street-address parser used by any project that has to deconstruct
// free-form prose into a leading street number, a list of
// discriminating street-name tokens, and a postcode boundary.
//
// Parse is tolerant — it always returns a Parts value even when the
// input is malformed; callers inspect the populated fields. See Parse
// for the full normalisation pipeline.
//
// Example:
//
//	p := fraddr.Parse("Résidence Le Méridien, 32 rue Dareau, 75014 Paris")
//	fmt.Println(p.Number)        // "32"
//	fmt.Println(p.StreetTokens)  // []string{"Dareau"}
//	fmt.Println(p.Query())       // "32 Dareau"
package fraddr
