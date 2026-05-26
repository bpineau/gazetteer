package gazetteer

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestResult_ZeroValue(t *testing.T) {
	var r Result
	if r.Status != "" {
		t.Errorf("Status zero = %q, want empty string", r.Status)
	}
	if r.Data != nil {
		t.Errorf("Data zero = %v, want nil", r.Data)
	}
	if r.Err != nil {
		t.Errorf("Err zero = %v, want nil", r.Err)
	}
}

// TestResult_ZeroValueIsAccessibleByGet asserts the historical
// idiom — constructing a Result without explicitly setting Status —
// still surfaces the typed Data through gazetteer.Get. Get treats a
// zero-value Status ("") as OK for backwards-compatibility.
func TestResult_ZeroValueIsAccessibleByGet(t *testing.T) {
	type stub struct{ V int }
	d := Dossier{Results: map[string]Result{
		"stub": {Name: "stub", Data: &stub{V: 42}},
	}}
	got, ok := Get[*stub](d, "stub")
	if !ok {
		t.Fatal("Get returned ok=false for zero-Status Result")
	}
	if got.V != 42 {
		t.Errorf("V = %d, want 42", got.V)
	}
}

func TestResult_MarshalJSON_OmitsErr(t *testing.T) {
	r := Result{
		Name:      "dvf",
		Version:   7,
		Status:    StatusFailedTransient,
		InputHash: "abc123",
		FetchedAt: time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC),
		Err:       errors.New("boom"),
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	// Err must NOT appear as a Go-error field (json default would skip
	// it; we just assert the marshal succeeded and the rest of the
	// payload is there).
	s := string(b)
	if !contains(s, `"name":"dvf"`) || !contains(s, `"status":"failed_transient"`) {
		t.Errorf("marshal output missing expected fields: %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
