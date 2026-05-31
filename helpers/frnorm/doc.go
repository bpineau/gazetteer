// Package frnorm hosts side-effect-free, allocation-bounded helpers for
// parsing French free-form text shapes: prices, dates, postal codes,
// accents, whitespace and hearing-times.
//
// # Scope
//
// frnorm is purely lexical. Inputs are strings; outputs are strings,
// int64 cents or time.Month values. The package performs no I/O, has no
// auction vocabulary, and is safe for concurrent use. Anything that
// touches a network, a database, or a pricing model belongs elsewhere.
//
// # Conventions
//
// French formatting wins by default. "1 336 500,50 €" parses; the
// Anglo "1,336,500.50" shape is rejected on purpose by
// ParseFRPriceToCentimes (see its doc for the disambiguation rules).
// Accent folding is case-preserving: "Étoile" → "Etoile". NBSP and
// narrow NBSP collapse to plain space wherever they appear.
//
// # When to reach for the stdlib instead
//
// frnorm is for the messy, human-typed FR input surface that arrives via
// HTML scraping, PDF extraction or operator-supplied search filters. If
// the input is already canonical — ISO-8601 dates, decimal-dot
// monetary values from a JSON API, ASCII-only addresses — strconv,
// time.Parse and the stdlib regex / unicode packages are a better fit.
package frnorm
