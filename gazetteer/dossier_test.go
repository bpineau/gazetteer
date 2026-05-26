package gazetteer

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type dossierTestPayload struct{ Median int }

func newOKDossier() Dossier {
	return Dossier{
		Listing: Listing{Address: "10 rue de la Paix"},
		Results: map[string]Result{
			"dvf": {
				Name:    "dvf",
				Version: 1,
				Status:  StatusOK,
				Data:    &dossierTestPayload{Median: 9500},
			},
			"osm": {
				Name:    "osm",
				Version: 2,
				Status:  StatusFailedTransient,
				Err:     errors.New("timeout"),
			},
		},
		StartedAt:  time.Now(),
		FinishedAt: time.Now(),
	}
}

func TestDossier_OK(t *testing.T) {
	d := newOKDossier()
	if !d.OK("dvf") {
		t.Errorf("d.OK(dvf) = false, want true")
	}
	if d.OK("osm") {
		t.Errorf("d.OK(osm) = true, want false (failed)")
	}
	if d.OK("absent") {
		t.Errorf("d.OK(absent) = true, want false")
	}
}

func TestDossier_Failed(t *testing.T) {
	d := newOKDossier()
	failed := d.Failed()
	if _, ok := failed["osm"]; !ok {
		t.Errorf("Failed() missing osm")
	}
	if _, ok := failed["dvf"]; ok {
		t.Errorf("Failed() should not include OK source dvf")
	}
}

func TestGet_OKSource(t *testing.T) {
	d := newOKDossier()
	v, ok := Get[*dossierTestPayload](d, "dvf")
	if !ok {
		t.Fatal("Get should return ok=true on OK source")
	}
	if v.Median != 9500 {
		t.Errorf("Median = %d, want 9500", v.Median)
	}
}

func TestGet_FailedSource(t *testing.T) {
	d := newOKDossier()
	_, ok := Get[*dossierTestPayload](d, "osm")
	if ok {
		t.Errorf("Get should return ok=false on failed source")
	}
}

func TestGet_TypeMismatch(t *testing.T) {
	d := newOKDossier()
	_, ok := Get[*Listing](d, "dvf")
	if ok {
		t.Errorf("Get should return ok=false on type mismatch")
	}
}

func TestGet_AbsentSource(t *testing.T) {
	d := newOKDossier()
	_, ok := Get[*dossierTestPayload](d, "nope")
	if ok {
		t.Errorf("Get should return ok=false on absent source")
	}
}

func TestGet_OKEmptyAlsoReturnsData(t *testing.T) {
	d := Dossier{
		Results: map[string]Result{
			"dvf": {
				Name:   "dvf",
				Status: StatusOKEmpty,
				Data:   &dossierTestPayload{Median: 0},
			},
		},
	}
	v, ok := Get[*dossierTestPayload](d, "dvf")
	if !ok {
		t.Fatal("Get should return data for StatusOKEmpty (caller decides via IsEmpty)")
	}
	if v.Median != 0 {
		t.Errorf("Median = %d, want 0", v.Median)
	}
}

func TestDossier_JSONRoundtrip(t *testing.T) {
	// Use a unique name to avoid registry collision across tests.
	const name = "test:dossier:roundtrip"
	registerForTest(t, name, func() any { return &dossierTestPayload{} })

	orig := Dossier{
		Listing: Listing{Address: "10 rue X"},
		Results: map[string]Result{
			name: {
				Name:    name,
				Version: 3,
				Status:  StatusOK,
				Data:    &dossierTestPayload{Median: 4242},
			},
		},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got Dossier
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	v, ok := Get[*dossierTestPayload](got, name)
	if !ok {
		t.Fatalf("Get after unmarshal: not found / wrong type")
	}
	if v.Median != 4242 {
		t.Errorf("Median = %d, want 4242", v.Median)
	}
}

func TestDossier_JSONRoundtrip_UnknownNamePreservesEnvelope(t *testing.T) {
	// A Dossier with an unregistered source name should unmarshal cleanly
	// but leave Data == nil (the raw bytes are dropped — registry-less
	// unmarshal is best-effort).
	js := `{"results":{"unknown_source":{"name":"unknown_source","version":1,"status":"ok","data":{"x":1}}}}`
	var d Dossier
	if err := json.Unmarshal([]byte(js), &d); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	r := d.Results["unknown_source"]
	if r.Data != nil {
		t.Errorf("Data for unknown source should be nil, got %v", r.Data)
	}
	// Envelope fields should still be populated.
	if r.Name != "unknown_source" {
		t.Errorf("Name = %q, want %q", r.Name, "unknown_source")
	}
	if r.Status != StatusOK {
		t.Errorf("Status = %v, want StatusOK", r.Status)
	}
}
