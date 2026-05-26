package gazettestest_test

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/gazetteer/gazettestest"
)

type stubPayload struct{ S string }

func (*stubPayload) IsEmpty() bool { return false }

func TestStubSource_OK(t *testing.T) {
	want := &stubPayload{S: "hello"}
	s := gazettestest.NewStubSource("stub", 7, want, nil)
	if s.Name() != "stub" {
		t.Errorf("Name = %q", s.Name())
	}
	if s.Version() != 7 {
		t.Errorf("Version = %d", s.Version())
	}
	got, err := s.Query(context.Background(), gazetteer.Listing{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if got != any(want) {
		t.Errorf("payload = %v, want %v", got, want)
	}
}

func TestStubSource_Error(t *testing.T) {
	wantErr := errors.New("boom")
	s := gazettestest.NewStubSource("stub", 1, nil, wantErr)
	_, err := s.Query(context.Background(), gazetteer.Listing{})
	if !errors.Is(err, wantErr) {
		t.Errorf("err = %v, want %v", err, wantErr)
	}
}

func TestStubSource_DrivesBuilderCollect(t *testing.T) {
	stub := gazettestest.NewStubSource("dvf", 1, &stubPayload{S: "ok"}, nil)
	client, err := gazetteer.NewBuilder().With(stub).Build()
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	d := client.Collect(context.Background(), gazetteer.Listing{})
	r, ok := d.Results["dvf"]
	if !ok {
		t.Fatalf("Results[dvf] missing")
	}
	if r.Status != gazetteer.StatusOK {
		t.Errorf("Status = %v, want StatusOK", r.Status)
	}
}

func TestStubSource_SentinelClassifies(t *testing.T) {
	cases := []struct {
		err  error
		want gazetteer.Status
	}{
		{gazetteer.ErrInsufficientInputs, gazetteer.StatusSkippedPrereq},
		{gazetteer.ErrUnsupportedPropertyType, gazetteer.StatusSkippedPrereq},
		{gazetteer.ErrAntiBot, gazetteer.StatusFailedAntiBot},
		{gazetteer.ErrUpstreamSchemaChanged, gazetteer.StatusFailedOutdated},
		{gazetteer.ErrUpstreamPermanent, gazetteer.StatusFailedPermanent},
	}
	for _, tc := range cases {
		stub := gazettestest.NewStubSource("s", 1, nil, tc.err)
		client, _ := gazetteer.NewBuilder().With(stub).Build()
		d := client.Collect(context.Background(), gazetteer.Listing{})
		if d.Results["s"].Status != tc.want {
			t.Errorf("err=%v: Status=%v want %v", tc.err, d.Results["s"].Status, tc.want)
		}
	}
}
