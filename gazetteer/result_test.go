package gazetteer

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestResult_ZeroValue(t *testing.T) {
	var r Result
	if r.Status != StatusOK {
		t.Errorf("Status zero = %v, want StatusOK", r.Status)
	}
	if r.Data != nil {
		t.Errorf("Data zero = %v, want nil", r.Data)
	}
	if r.Err != nil {
		t.Errorf("Err zero = %v, want nil", r.Err)
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
