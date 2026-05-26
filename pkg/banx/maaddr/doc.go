// Package maaddr packages the small, generic address-normalization helpers
// that an autocomplete-shaped enricher (MeilleursAgents today, any future
// site keyed on a street-level slug tomorrow) layers on top of `banx`'s
// raw BAN client.
//
// Scope deliberately narrow: pure string + Geocoder calls, no HTTP, no
// DB, no enricher-specific sentinels. The MA enricher's own
// `IsAutocompleteSoftErr` sentinel set stays in
// `internal/core/enrich/meilleursagents` because it is keyed on
// MA-autocomplete error shapes.
//
// Helpers:
//
//   - [CanonicalizeAddress] : ask BAN for the canonical form of a raw
//     address and return the street-only portion (zip+city stripped).
//   - [StripTrailingZipCity] : trim a trailing "<zip> <city>" tail from a
//     BAN label so the result is just the street + house number.
//
// Both helpers were promoted from
// `internal/core/enrich/meilleursagents/fallback.go` so the in-tree
// enricher and the handler-side queue resolver share a single
// implementation (memory `matcher_cluster_online_symmetry`).
package maaddr
