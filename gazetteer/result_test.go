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

type evTestPayload struct{ V int }

func (p *evTestPayload) IsEmpty() bool { return false }

func TestResult_EvidenceJSONRoundTrip(t *testing.T) {
	type evidence struct {
		Tier string `json:"tier"`
		N    int    `json:"n"`
	}
	Register("evroundtrip", func() any { return &evTestPayload{} })
	d := Dossier{Results: map[string]Result{
		"evroundtrip": {
			Name:     "evroundtrip",
			Status:   StatusOK,
			Data:     &evTestPayload{V: 1},
			Evidence: &evidence{Tier: "commune", N: 42},
		},
	}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !contains(string(b), `"evidence":{"tier":"commune","n":42}`) {
		t.Fatalf("wire form lacks evidence: %s", b)
	}
	var back Dossier
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	raw, ok := back.Results["evroundtrip"].Evidence.(json.RawMessage)
	if !ok {
		t.Fatalf("round-tripped Evidence = %T, want json.RawMessage", back.Results["evroundtrip"].Evidence)
	}
	var ev evidence
	if err := json.Unmarshal(raw, &ev); err != nil || ev.Tier != "commune" || ev.N != 42 {
		t.Errorf("Evidence payload = %s (%v), want tier=commune n=42", raw, err)
	}
}
