package gazetteer

import (
	"testing"
)

type regTestPayload struct{ X int }

func TestRegister_Lookup(t *testing.T) {
	const name = "test:register:lookup"
	registerForTest(t, name, func() any { return &regTestPayload{X: 7} })
	f := Lookup(name)
	if f == nil {
		t.Fatal("Lookup returned nil for registered name")
	}
	v, ok := f().(*regTestPayload)
	if !ok || v.X != 7 {
		t.Errorf("factory yielded %v, want &regTestPayload{X:7}", f())
	}
}

func TestLookup_Unknown(t *testing.T) {
	if f := Lookup("absolutely-not-registered"); f != nil {
		t.Errorf("Lookup of unknown name returned non-nil")
	}
}

func TestRegisteredNames(t *testing.T) {
	// Register two names; assert RegisteredNames includes them in
	// sorted order. The registry is process-wide (real sources
	// auto-register in init), so we only check the two we control.
	a := "test:registerednames:zzz"
	b := "test:registerednames:aaa"
	registerForTest(t, a, func() any { return &regTestPayload{} })
	registerForTest(t, b, func() any { return &regTestPayload{} })

	names := RegisteredNames()
	var posA, posB = -1, -1
	for i, n := range names {
		if n == a {
			posA = i
		}
		if n == b {
			posB = i
		}
	}
	if posA == -1 || posB == -1 {
		t.Fatalf("RegisteredNames missing %q or %q: got %v", a, b, names)
	}
	if posB >= posA {
		t.Errorf("RegisteredNames not sorted: %q at %d, %q at %d", b, posB, a, posA)
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	const name = "test:register:duplicate"
	registerForTest(t, name, func() any { return &regTestPayload{} })
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("duplicate Register should panic")
		}
	}()
	// Intentionally call the bare Register here (NOT registerForTest)
	// because we expect this call to panic before t.Cleanup would be
	// registered. The first registerForTest above handles cleanup.
	Register(name, func() any { return &regTestPayload{} })
}
