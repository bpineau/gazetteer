package main

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
)

func TestParseQueryFlags(t *testing.T) {
	q, err := parseQueryFlags("query", []string{
		"--property-type", "house", "--surface", "80", "--rooms", "4",
		"--source", "dvf,carteloyers", "--json", "12 rue X, 93100 Montreuil",
	})
	if err != nil {
		t.Fatalf("parseQueryFlags: %v", err)
	}
	if q.addr != "12 rue X, 93100 Montreuil" {
		t.Errorf("addr = %q", q.addr)
	}
	if q.propertyType != "house" || q.surface != 80 || q.rooms != 4 || !q.jsonOut {
		t.Errorf("flags not captured: %+v", q)
	}
	if got := splitCSV(q.sources); !reflect.DeepEqual(got, []string{"dvf", "carteloyers"}) {
		t.Errorf("sources = %v", got)
	}
}

func TestParseQueryFlags_InterleavedAndErrors(t *testing.T) {
	// Flags may come after the positional address.
	q, err := parseQueryFlags("query", []string{"1 rue de Rivoli, Paris", "--rooms", "2"})
	if err != nil {
		t.Fatalf("interleaved: %v", err)
	}
	if q.rooms != 2 || q.addr != "1 rue de Rivoli, Paris" {
		t.Errorf("interleaved parse: %+v", q)
	}

	if _, err := parseQueryFlags("query", nil); err == nil {
		t.Error("missing <addr> should error")
	}
	if _, err := parseQueryFlags("query", []string{"--rooms", "NaN", "x"}); !errors.Is(err, errUsage) {
		t.Errorf("bad flag value should map to errUsage, got %v", err)
	}
}

func TestParseCompareFlags(t *testing.T) {
	q, addrs, err := parseCompareFlags([]string{
		"--profile", "balanced", "addr one", "addr two",
	})
	if err != nil {
		t.Fatalf("parseCompareFlags: %v", err)
	}
	if q.profile != "balanced" {
		t.Errorf("profile = %q", q.profile)
	}
	if !reflect.DeepEqual(addrs, []string{"addr one", "addr two"}) {
		t.Errorf("addrs = %v", addrs)
	}
	if q.timeout != 30*time.Second {
		t.Errorf("default timeout = %v, want 30s", q.timeout)
	}
}

func TestParsePropertyType(t *testing.T) {
	cases := []struct {
		in   string
		want gazetteer.PropertyType
		ok   bool
	}{
		{"", gazetteer.PropertyApartment, true},
		{"apartment", gazetteer.PropertyApartment, true},
		{"Appartement", gazetteer.PropertyApartment, true},
		{"maison", gazetteer.PropertyHouse, true},
		{"castle", "", false},
	}
	for _, c := range cases {
		got, err := parsePropertyType(c.in)
		if (err == nil) != c.ok {
			t.Errorf("parsePropertyType(%q) err = %v, want ok=%v", c.in, err, c.ok)
			continue
		}
		if c.ok && got != c.want {
			t.Errorf("parsePropertyType(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSplitCSV(t *testing.T) {
	if got := splitCSV("  "); got != nil {
		t.Errorf("splitCSV(blank) = %v, want nil", got)
	}
	if got := splitCSV("a, b ,,c"); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Errorf("splitCSV = %v", got)
	}
}

func TestPrintDossierSummary(t *testing.T) {
	surface := 45.0
	lat, lon := 48.8566, 2.3522
	d := gazetteer.Dossier{
		Listing: gazetteer.Listing{
			Address: "12 rue X", City: "Montreuil", Zip: "93100",
			INSEE: "93048", Lat: &lat, Lon: &lon,
			PropertyType: gazetteer.PropertyApartment, SurfaceM2: &surface,
		},
		Results: map[string]gazetteer.Result{
			"dvf": {Name: "dvf", Version: 3, Status: gazetteer.StatusOKEmpty},
			"oll": {Name: "oll", Version: 1, Status: gazetteer.StatusFailedTransient,
				Err: errors.New("oll: upstream unavailable")},
		},
	}
	var buf bytes.Buffer
	printDossierSummary(&buf, d)
	out := buf.String()

	for _, want := range []string{
		"12 rue X", "93048", "48.856600,2.352200", "45 m²",
		"dvf", "oll", "transient", "upstream unavailable",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("summary output missing %q:\n%s", want, out)
		}
	}
	// Sorted, diff-friendly source order: dvf before oll.
	if strings.Index(out, "dvf") > strings.Index(out, "oll") {
		t.Errorf("sources not in sorted order:\n%s", out)
	}
}

func TestAddrOf(t *testing.T) {
	if got := addrOf(gazetteer.Listing{Address: "a", INSEE: "75056"}); got != "a" {
		t.Errorf("addrOf = %q, want the address", got)
	}
	if got := addrOf(gazetteer.Listing{INSEE: "75056"}); got != "75056" {
		t.Errorf("addrOf = %q, want the INSEE fallback", got)
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:                "512B",
		2048:               "2.0KiB",
		5 * 1024 * 1024:    "5.0MiB",
		3 << 30:            "3.0GiB",
		1536 * 1024 * 1024: "1.5GiB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestOrDashHelpers(t *testing.T) {
	if orDash(" ") != "—" || orDash("x") != "x" {
		t.Error("orDash")
	}
	f := 1.5
	if orDashF(nil) != "—" || orDashF(&f) != "1.5" {
		t.Error("orDashF")
	}
	i := 3
	if orDashI(nil) != "—" || orDashI(&i) != "3" {
		t.Error("orDashI")
	}
}

func TestUsageListsEverySubcommand(t *testing.T) {
	var buf bytes.Buffer
	usage(&buf)
	out := buf.String()
	for _, cmd := range []string{"query", "appraise", "compare", "normalize", "sources", "refresh", "version"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("usage output does not mention %q", cmd)
		}
	}
}
