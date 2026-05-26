package safejson

import (
	"strings"
	"testing"
)

func TestMustMarshal_Map(t *testing.T) {
	got := MustMarshal(map[string]int{"a": 1})
	if string(got) != `{"a":1}` {
		t.Errorf("want {\"a\":1}, got %s", got)
	}
}

func TestMustMarshal_Slice(t *testing.T) {
	got := MustMarshal([]string{"x", "y"})
	if string(got) != `["x","y"]` {
		t.Errorf("want [\"x\",\"y\"], got %s", got)
	}
}

func TestMustMarshalIndent(t *testing.T) {
	got := MustMarshalIndent(map[string]int{"a": 1}, "", "  ")
	if !strings.Contains(string(got), "\"a\": 1") {
		t.Errorf("expected indented output, got %s", got)
	}
}

func TestMustMarshal_PanicsOnUnmarshalable(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for unmarshalable input (chan)")
		}
	}()
	MustMarshal(make(chan int))
}
