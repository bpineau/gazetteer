// Package filosofi is a gazetteer.Source for the INSEE Filosofi 2021
// per-commune income and minima-sociaux statistics, served from an
// embedded JSON snapshot.
//
// The Source needs the listing's INSEE; property type is irrelevant
// (the Filosofi profile applies to the whole commune). It returns a
// *Result carrying median household disposable income, minima-sociaux
// percentage and a coarse income-risk flag (low / medium / high /
// unknown).
//
// Example:
//
//	src := filosofi.NewSource(filosofi.Options{})
//	r, err := src.Query(ctx, gazetteer.Listing{INSEE: "93066"})
package filosofi
