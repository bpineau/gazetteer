// Package safejson ships tiny wrappers around encoding/json's
// Marshal for callers that operate on data they have already
// established is marshalable — plain maps, slices and structs with
// `json:` tags only.
//
// The error returned by json.Marshal is unreachable in those cases,
// so production code routinely writes `b, _ := json.Marshal(v)`.
// That pattern silently produces an empty byte slice if a programmer
// ever breaks the invariant (e.g. embeds a chan or a func, or
// implements a faulty json.Marshaler). MustMarshal makes the broken
// contract loud instead — a panic that surfaces in tests.
//
// Example:
//
//	body := safejson.MustMarshal(req)  // panics on chan/func; never empty-on-error
package safejson
