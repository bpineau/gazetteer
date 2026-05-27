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
