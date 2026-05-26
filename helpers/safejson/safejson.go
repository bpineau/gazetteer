// Package safejson ships tiny wrappers around encoding/json's Marshal
// for callers that operate on data they have already established is
// marshalable — plain maps, slices and structs with `json:` tags only.
//
// The error returned by json.Marshal is unreachable in those cases, so
// production code routinely writes `b, _ := json.Marshal(v)`. That
// pattern silently produces an empty byte slice if a programmer ever
// breaks the invariant (e.g. embeds a chan or a func, or implements a
// faulty json.Marshaler). MustMarshal makes the broken contract loud
// instead — a panic that surfaces in tests.
package safejson

import (
	"encoding/json"
	"fmt"
)

// MustMarshal serialises v to JSON. Panics if json.Marshal reports an
// error. Use only for inputs whose marshalability is a programmer
// invariant — map/struct payloads with json tags, never user-supplied
// types or anything that contains channels, functions or recursive
// graphs.
func MustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// Hitting this branch means a caller passed a type that
		// json.Marshal cannot handle (chan/func/cycle) or a faulty
		// MarshalJSON. Panic so it surfaces in tests rather than
		// silently writing empty JSON downstream.
		panic(fmt.Sprintf("safejson.MustMarshal: %v", err))
	}
	return b
}

// MustMarshalIndent is the same as MustMarshal but produces indented
// output, matching the json.MarshalIndent signature.
func MustMarshalIndent(v any, prefix, indent string) []byte {
	b, err := json.MarshalIndent(v, prefix, indent)
	if err != nil {
		panic(fmt.Sprintf("safejson.MustMarshalIndent: %v", err))
	}
	return b
}
