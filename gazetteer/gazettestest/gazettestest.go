// Package gazettestest provides reusable test doubles for code that
// consumes the gazetteer API.
//
// StubSource lets callers build a gazetteer.Source from a fixed
// (payload, error) pair without re-declaring a fakeSource type in
// every test file. The stub honours the same contract as a real
// Source — Name / Version / Query — so it can be passed straight to
// gazetteer.NewBuilder().With(...).
//
// Typical usage:
//
//	src := gazettestest.NewStubSource("dvf", 1, &dvf.Result{...}, nil)
//	client, _ := gazetteer.NewBuilder().With(src).Build()
//	d := client.Collect(ctx, listing)
//	r, _ := gazetteer.Get[*dvf.Result](d, "dvf")
//
// For error-path tests, pass a non-nil err and the framework will
// translate it through classifyErr exactly as it does for a real
// Source — letting consumers exercise the full Status taxonomy
// (StatusFailedAntiBot, StatusFailedPermanent, etc.) deterministically.
package gazettestest

import (
	"context"

	"github.com/bpineau/gazetteer/gazetteer"
)

// StubSource is a deterministic gazetteer.Source returning a fixed
// (payload, error) pair on every Query call. Safe for concurrent use
// (the fields are written once at construction and read-only after).
type StubSource struct {
	name    string
	version int
	payload any
	err     error
}

// NewStubSource builds a stub that returns (payload, err) for every
// Query. Pass payload=nil + err=nil to model a Source that returns
// no error and no data (the framework records StatusOK with nil Data).
// Pass any gazetteer sentinel (or a wrap thereof) as err to exercise
// the classifyErr Status mapping in consumer tests.
func NewStubSource(name string, version int, payload any, err error) *StubSource {
	return &StubSource{
		name:    name,
		version: version,
		payload: payload,
		err:     err,
	}
}

// Name implements gazetteer.Source.
func (s *StubSource) Name() string { return s.name }

// Version implements gazetteer.Source.
func (s *StubSource) Version() int { return s.version }

// Query implements gazetteer.Source. The ctx and Listing arguments are
// ignored — the stub returns the (payload, err) configured at
// construction time. Tests that need to assert on the call (e.g.
// "Query was invoked with X") should use a hand-rolled mock instead.
func (s *StubSource) Query(_ context.Context, _ gazetteer.Listing) (any, error) {
	return s.payload, s.err
}

var _ gazetteer.Source = (*StubSource)(nil)
