// Package maaddr provides the small, generic address-normalization
// helpers an autocomplete-shaped consumer layers on top of `banx`'s
// raw BAN client. Pure string + Geocoder calls — no HTTP, no DB, no
// upstream-specific sentinels.
//
// Helpers:
//
//   - [CanonicalizeAddress] : ask BAN for the canonical form of a raw
//     address and return the street-only portion (zip+city stripped).
//   - [StripTrailingZipCity] : trim a trailing "<zip> <city>" tail
//     from a BAN label so the result is just the street + house
//     number.
package maaddr
