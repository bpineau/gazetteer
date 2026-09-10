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
	}, discardStreams())
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
	q, err := parseQueryFlags("query", []string{"1 rue de Rivoli, Paris", "--rooms", "2"}, discardStreams())
	if err != nil {
		t.Fatalf("interleaved: %v", err)
	}
	if q.rooms != 2 || q.addr != "1 rue de Rivoli, Paris" {
		t.Errorf("interleaved parse: %+v", q)
	}

	if _, err := parseQueryFlags("query", nil, discardStreams()); err == nil {
		t.Error("missing <addr> should error")
	}
	if _, err := parseQueryFlags("query", []string{"--rooms", "NaN", "x"}, discardStreams()); !errors.Is(err, errUsage) {
		t.Errorf("bad flag value should map to errUsage, got %v", err)
	}
}

func TestParseCompareFlags(t *testing.T) {
	q, addrs, err := parseCompareFlags([]string{
		"--profile", "balanced", "addr one", "addr two",
	}, discardStreams())
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

// TestProfileFlagIsAppraiseOnly pins B18's fix: the shared flag set backs both
// `query` and `appraise`, but only `appraise` computes a ZoneScore. On `query`
// the flag used to be advertised by -h while runQuery never read it, so an
// invalid preset passed silently; it must now be unknown there (a loud usage
// error) and still work on `appraise`. `compare` keeps its own registration.
func TestProfileFlagIsAppraiseOnly(t *testing.T) {
	q, err := parseQueryFlags("appraise", []string{"--profile", "patrimoine", "12 rue X, Paris"}, discardStreams())
	if err != nil {
		t.Fatalf("appraise --profile: %v", err)
	}
	if q.profile != "patrimoine" {
		t.Errorf("appraise profile = %q, want %q", q.profile, "patrimoine")
	}

	if _, err := parseQueryFlags("query", []string{"--profile", "patrimoine", "12 rue X, Paris"}, discardStreams()); !errors.Is(err, errUsage) {
		t.Errorf("query --profile err = %v, want errUsage (the flag must not exist there)", err)
	}

	// And the -h transcript each sub-command prints must agree.
	if got := usageOf(t, "query"); strings.Contains(got, "-profile") {
		t.Errorf("`query -h` offers --profile:\n%s", got)
	}
	if got := usageOf(t, "appraise"); !strings.Contains(got, "-profile") {
		t.Errorf("`appraise -h` does not offer --profile:\n%s", got)
	}
}

// usageOf captures what `gazetteer <cmd> -h` prints. The flag set writes its
// Usage banner to the streams it was handed, so a buffer suffices.
func usageOf(t *testing.T, cmd string) string {
	t.Helper()
	var c capture
	_, _ = parseQueryFlags(cmd, []string{"-h"}, c.streams())
	return c.stderr.String()
}

// TestExplainFlagFeedsBothSubcommands is the mirror of
// TestProfileFlagIsAppraiseOnly: --explain sits on the SAME shared flag set,
// but here both users of that set collect a Dossier, so the honest fix was to
// wire it rather than to unregister it. runAppraise used to ignore the flag it
// advertised, printing the plain summary; `appraise --explain` must now print
// the diagnosis and keep the synthesis. Both sub-commands are asserted on the
// three surfaces that can drift apart: parsing, the -h transcript, and the
// renderer they share.
func TestExplainFlagFeedsBothSubcommands(t *testing.T) {
	for _, cmd := range []string{"query", cmdAppraise} {
		q, err := parseQueryFlags(cmd, []string{"--explain", "12 rue X, Paris"}, discardStreams())
		if err != nil {
			t.Fatalf("%s --explain: %v", cmd, err)
		}
		if !q.explain {
			t.Errorf("%s --explain not captured: %+v", cmd, q)
		}
		if got := usageOf(t, cmd); !strings.Contains(got, "-explain") {
			t.Errorf("`%s -h` does not offer --explain:\n%s", cmd, got)
		}
	}

	// oll carries INSEE + rooms, so its emptiness is a coverage verdict, not a
	// missing input: the diagnosis says so, the summary table cannot.
	rooms := 2
	d := gazetteer.Dossier{
		Listing: gazetteer.Listing{Address: "12 rue X", INSEE: "75110", Rooms: &rooms},
		Results: map[string]gazetteer.Result{
			"dvf": {Name: "dvf", Version: 3, Status: gazetteer.StatusOK, Data: &filledResult{}},
			"oll": {Name: "oll", Version: 1, Status: gazetteer.StatusOKEmpty, Data: &emptyResult{}},
		},
	}

	var buf bytes.Buffer
	printSourceBlock(&buf, d, true)
	explained := buf.String()
	for _, want := range []string{
		"Listing (after normalisation)", "Per-source diagnosis",
		"no data for this address", "1 source(s) returned data, 1 empty, 0 failed",
	} {
		if !strings.Contains(explained, want) {
			t.Errorf("explain output missing %q:\n%s", want, explained)
		}
	}

	buf.Reset()
	printSourceBlock(&buf, d, false)
	summary := buf.String()
	if !strings.Contains(summary, "results:") {
		t.Errorf("summary output is not the per-source table:\n%s", summary)
	}
	if strings.Contains(summary, "Per-source diagnosis") {
		t.Errorf("summary output carries the diagnosis without --explain:\n%s", summary)
	}
}
