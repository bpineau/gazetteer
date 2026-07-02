package main

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/carteloyers"
	"github.com/bpineau/gazetteer/sources/dvf"
)

// TestRenderersRenderFilledPayloads is the happy-path complement to
// TestRenderersNilAndZeroSafe: every registered renderer receives its
// source's payload with every field set to a non-zero value, so the
// formatting branches actually run. Catches nil-pointer derefs on
// optional fields and renderers that return nothing for real data.
func TestRenderersRenderFilledPayloads(t *testing.T) {
	for name, rdr := range sourceRenderers {
		t.Run(name, func(t *testing.T) {
			factory := gazetteer.Lookup(name)
			if factory == nil {
				t.Fatalf("no payload registered for %q", name)
			}
			payload := factory()
			fillValue(t, reflect.ValueOf(payload).Elem(), 6)

			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("renderer panicked on filled payload: %v", p)
				}
			}()
			headline, _ := rdr(payload)
			if headline == "" {
				t.Errorf("renderer returned an empty headline for a fully-populated payload")
			}
		})
	}
}

// fillValue recursively sets every settable field of v to a non-zero
// value. depth bounds recursion on self-referential shapes.
func fillValue(t *testing.T, v reflect.Value, depth int) {
	t.Helper()
	if depth == 0 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x")
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(2)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(2)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fillValue(t, v.Elem(), depth-1)
	case reflect.Slice:
		elem := reflect.New(v.Type().Elem()).Elem()
		fillValue(t, elem, depth-1)
		v.Set(reflect.Append(reflect.MakeSlice(v.Type(), 0, 1), elem))
	case reflect.Map:
		key := reflect.New(v.Type().Key()).Elem()
		fillValue(t, key, depth-1)
		val := reflect.New(v.Type().Elem()).Elem()
		fillValue(t, val, depth-1)
		m := reflect.MakeMapWithSize(v.Type(), 1)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Struct:
		if v.Type() == reflect.TypeFor[time.Time]() {
			v.Set(reflect.ValueOf(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)))
			return
		}
		for _, f := range v.Fields() {
			fillValue(t, f, depth-1)
		}
	default:
		// interfaces, chans, funcs: leave zero — no renderer reads them.
	}
}

// TestRenderDVF pins the cents-to-euros conversion, the one place a
// unit mistake would silently misprice everything the CLI shows.
func TestRenderDVF(t *testing.T) {
	cents := int64(834900) // 8 349.00 €/m²
	r := &dvf.Result{ValueEURPerM2Cents: &cents, SampleSize: 42, Confidence: "high"}
	headline, _ := renderDVF(r)
	for _, want := range []string{"8349 €/m²", "42 sales", "conf=high"} {
		if !strings.Contains(headline, want) {
			t.Errorf("headline %q does not contain %q", headline, want)
		}
	}
}

// TestRenderCarteloyers pins the CC (charges comprises) labelling: the
// rent-basis distinction the docs insist on must survive rendering.
func TestRenderCarteloyers(t *testing.T) {
	r := &carteloyers.Result{
		LoyerMedEURPerM2CC:  18.5,
		LoyerLowEURPerM2CC:  16.0,
		LoyerHighEURPerM2CC: 21.0,
		NbObservations:      120,
		Confidence:          "A",
	}
	headline, _ := renderCarteloyers(r)
	if !strings.Contains(headline, "18.50 €/m²/mois CC") {
		t.Errorf("headline %q must show the median rent tagged CC", headline)
	}
	if !strings.Contains(headline, "16.00-21.00") {
		t.Errorf("headline %q must show the confidence interval", headline)
	}
}

func TestSummariseResult(t *testing.T) {
	cases := []struct {
		name string
		r    gazetteer.Result
		want string
	}{
		{"unknown_ok", gazetteer.Result{Status: gazetteer.StatusOK}, "ok"},
		{"unknown_empty", gazetteer.Result{Status: gazetteer.StatusOKEmpty}, "no data"},
		{"failure", gazetteer.Result{
			Status: gazetteer.StatusFailedTransient,
			Err:    errors.New("dvf: upstream unavailable: 503"),
		}, "upstream unavailable: 503"},
		{"failure_no_err", gazetteer.Result{Status: gazetteer.StatusFailedPermanent}, "failed_permanent"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, _ := summariseResult("no_such_source", c.r)
			if got != c.want {
				t.Errorf("summariseResult = %q, want %q", got, c.want)
			}
		})
	}
}

func TestAbbreviateStatus(t *testing.T) {
	cases := map[gazetteer.Status]string{
		gazetteer.StatusOK:              "ok",
		"":                              "ok",
		gazetteer.StatusOKEmpty:         "empty",
		gazetteer.StatusSkippedPrereq:   "skipped",
		gazetteer.StatusFailedTransient: "transient",
		gazetteer.StatusFailedAntiBot:   "antibot",
		gazetteer.StatusFailedOutdated:  "outdated",
		gazetteer.StatusFailedPermanent: "permanent",
		"exotic":                        "exotic",
	}
	for in, want := range cases {
		if got := abbreviateStatus(in); got != want {
			t.Errorf("abbreviateStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Errorf("truncate(short) = %q", got)
	}
	long := strings.Repeat("a", 20)
	got := truncate(long, 10)
	if len(got) > 10+len("…")-1 || !strings.HasSuffix(got, "…") {
		t.Errorf("truncate(long, 10) = %q, want 9 bytes + ellipsis", got)
	}
}

func TestUnwrap(t *testing.T) {
	if got := unwrap("dvf: kaboom"); got != "kaboom" {
		t.Errorf("unwrap = %q, want %q", got, "kaboom")
	}
	long := strings.Repeat("a", 50) + ": detail"
	if got := unwrap(long); got != long {
		t.Errorf("unwrap should not strip a prefix longer than 40 bytes")
	}
}
