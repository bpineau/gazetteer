package gazetteer

import (
	"context"
	"testing"
)

func TestStatus_String(t *testing.T) {
	cases := []struct {
		s    Status
		want string
	}{
		{StatusOK, "ok"},
		{StatusOKEmpty, "ok_empty"},
		{StatusSkippedPrereq, "skipped_prereq"},
		{StatusFailedTransient, "failed_transient"},
		{StatusFailedAntiBot, "failed_antibot"},
		{StatusFailedOutdated, "failed_outdated"},
		{StatusFailedPermanent, "failed_permanent"},
	}
	for _, c := range cases {
		if got := c.s.String(); got != c.want {
			t.Errorf("Status(%q).String() = %q, want %q", string(c.s), got, c.want)
		}
	}
}

func TestStatus_StringEmpty(t *testing.T) {
	if got := Status("").String(); got == "" {
		t.Errorf("zero-value Status.String() returned empty; want a sentinel")
	}
}

type fakeSource struct {
	name string
	ver  int
	out  any
	err  error
}

func (f *fakeSource) Name() string { return f.name }
func (f *fakeSource) Version() int { return f.ver }
func (f *fakeSource) Query(ctx context.Context, l Listing) (any, error) {
	return f.out, f.err
}

// Compile-time check the fake satisfies Source.
var _ Source = (*fakeSource)(nil)

type fakeEmpty struct{ empty bool }

func (f *fakeEmpty) IsEmpty() bool { return f.empty }

func TestEmptyReporter_TypeAssertion(t *testing.T) {
	var data any = &fakeEmpty{empty: true}
	er, ok := data.(EmptyReporter)
	if !ok {
		t.Fatal("fakeEmpty should satisfy EmptyReporter")
	}
	if !er.IsEmpty() {
		t.Error("IsEmpty() = false, want true")
	}

	var nonEmpty any = struct{ X int }{X: 5}
	if _, ok := nonEmpty.(EmptyReporter); ok {
		t.Error("plain struct should not satisfy EmptyReporter")
	}
}

func TestSource_QueryReturnsAnyError(t *testing.T) {
	s := &fakeSource{name: "fake", ver: 1, out: &fakeEmpty{empty: true}}
	got, err := s.Query(context.Background(), Listing{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	er, ok := got.(EmptyReporter)
	if !ok || !er.IsEmpty() {
		t.Errorf("expected EmptyReporter with IsEmpty()=true; got %v", got)
	}
}
