package georisques

import (
	"reflect"
	"testing"
)

func TestExtractRedFlags(t *testing.T) {
	result := map[string]any{
		"summary": map[string]any{
			"red_flags": []any{"seisme", "RETRAIT_ARGILE", "seisme", "  ", "radon"},
		},
	}
	got, ok := ExtractRedFlags(result)
	if !ok {
		t.Fatalf("ExtractRedFlags ok=false, want true")
	}
	want := []string{"seisme", "retrait_argile", "radon"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractRedFlags = %v, want %v", got, want)
	}
}

func TestExtractRedFlags_Absent(t *testing.T) {
	if _, ok := ExtractRedFlags(map[string]any{}); ok {
		t.Errorf("missing summary → ok should be false")
	}
	if _, ok := ExtractRedFlags(map[string]any{"summary": map[string]any{}}); ok {
		t.Errorf("missing red_flags → ok should be false")
	}
	if _, ok := ExtractRedFlags(map[string]any{"summary": map[string]any{"red_flags": []any{}}}); ok {
		t.Errorf("empty red_flags → ok should be false")
	}
}

func TestExtractActiveNaturalRisks(t *testing.T) {
	result := map[string]any{
		"naturels": map[string]any{
			"inondation":     map[string]any{"present": true},
			"seisme":         map[string]any{"present": true},
			"radon":          map[string]any{"present": false},
			"retrait_argile": "not a map",
		},
	}
	got, ok := ExtractActiveNaturalRisks(result)
	if !ok {
		t.Fatalf("ExtractActiveNaturalRisks ok=false, want true")
	}
	// expect deterministic alphabetical order
	want := []string{"inondation", "seisme"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractActiveNaturalRisks = %v, want %v", got, want)
	}
}

func TestExtractActiveTechnoRisks(t *testing.T) {
	result := map[string]any{
		"technos": map[string]any{
			"icpe":          map[string]any{"present": true},
			"sites_pollues": map[string]any{"present": false},
			"transport_md":  map[string]any{"present": true},
		},
	}
	got, ok := ExtractActiveTechnoRisks(result)
	if !ok {
		t.Fatalf("ExtractActiveTechnoRisks ok=false, want true")
	}
	want := []string{"icpe", "transport_md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractActiveTechnoRisks = %v, want %v", got, want)
	}
}

func TestExtractActiveRisks_Empty(t *testing.T) {
	if _, ok := ExtractActiveNaturalRisks(map[string]any{}); ok {
		t.Errorf("empty input → ok should be false")
	}
	if _, ok := ExtractActiveTechnoRisks(map[string]any{}); ok {
		t.Errorf("empty input → ok should be false")
	}
}
