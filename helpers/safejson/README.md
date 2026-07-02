# safejson — loud Marshal for can't-fail call sites

Tiny wrappers around `encoding/json.Marshal` for callers operating on
data they have already established is marshalable: plain maps, slices
and structs with `json:` tags only.

In those cases `json.Marshal`'s error is unreachable, so production
code routinely writes `b, _ := json.Marshal(v)`. That pattern silently
produces an empty byte slice the day someone breaks the invariant
(embeds a `chan` or a `func`, or ships a faulty `json.Marshaler`).
`MustMarshal` makes the broken contract loud instead: a panic that
surfaces in the first test that exercises the type.

## Quick start

```go
import "github.com/bpineau/gazetteer/helpers/safejson"

body := safejson.MustMarshal(req)             // never empty-on-error
pretty := safejson.MustMarshalIndent(v, "", "  ")
```

Use it only where the value's marshalability is a compile-time-ish
invariant of your own types. For data crossing a trust boundary
(user input, upstream payloads), keep the explicit error path.

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/safejson`:

- `func MustMarshal(v any) []byte`
- `func MustMarshalIndent(v any, prefix, indent string) []byte`

## Status

Stable and intentionally minimal; no further symbols planned.
