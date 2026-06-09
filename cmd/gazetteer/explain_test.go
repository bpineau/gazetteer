package main

import (
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestMissingInputs(t *testing.T) {
	t.Parallel()
	withIRIS := gazetteer.Listing{IRIS: "751103806"}
	withINSEE := gazetteer.Listing{INSEE: "75110", Address: "x"}
	cases := []struct {
		name   string
		inputs []inputClause
		l      gazetteer.Listing
		want   []string
	}{
		{"iris missing", []inputClause{need("Listing.IRIS")}, gazetteer.Listing{}, []string{"Listing.IRIS"}},
		{"iris present", []inputClause{need("Listing.IRIS")}, withIRIS, nil},
		{"INSEE-or-address satisfied by INSEE", []inputClause{need("INSEE", "address")}, withINSEE, nil},
		{"rooms missing", []inputClause{need("INSEE"), need("rooms")}, withINSEE, []string{"rooms"}},
		{"all present", []inputClause{need("lat/lon")}, gazetteer.Listing{Lat: new(48.8), Lon: new(2.3)}, nil},
		{"optional clause never gates", []inputClause{need("INSEE"), optional("for €-total", "surface")}, withINSEE, nil},
		{"zip satisfies zip-or-INSEE", []inputClause{need("zip", "INSEE")}, gazetteer.Listing{Zip: "93100"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := missingInputs(c.inputs, c.l)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("missingInputs = %v, want %v", got, c.want)
			}
		})
	}
}

func TestEmptyVerdict(t *testing.T) {
	t.Parallel()

	// filoiris needs Listing.IRIS; a Listing without it → "missing" verdict.
	v := emptyVerdict("filoiris", gazetteer.Listing{INSEE: "13001"})
	if !strings.Contains(v, "missing Listing.IRIS") {
		t.Errorf("filoiris verdict = %q, want a missing-Listing.IRIS cause", v)
	}

	// oll needs INSEE + rooms; with both present the cause is coverage/no-data,
	// and the verdict surfaces oll's Paris exclusion.
	v = emptyVerdict("oll", gazetteer.Listing{INSEE: "75110", Rooms: new(2)})
	if !strings.Contains(v, "no data for this address") || !strings.Contains(v, "Paris") {
		t.Errorf("oll verdict = %q, want a coverage/no-data cause mentioning Paris", v)
	}
}

func TestIsOKWithData(t *testing.T) {
	t.Parallel()
	if !isOKWithData(gazetteer.Result{Status: gazetteer.StatusOK, Data: &filledResult{}}) {
		t.Error("OK + non-empty should be data")
	}
	if isOKWithData(gazetteer.Result{Status: gazetteer.StatusOKEmpty, Data: &emptyResult{}}) {
		t.Error("OKEmpty should not be data")
	}
}

// tiny EmptyReporter doubles for isOKWithData.
type filledResult struct{}

func (filledResult) IsEmpty() bool { return false }

type emptyResult struct{}

func (emptyResult) IsEmpty() bool { return true }
