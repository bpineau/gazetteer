package gazetteer

import "testing"

// registerForTest registers (name, factory) in the package-global
// registry and schedules removal via t.Cleanup so the registry survives
// `go test -count=N` and parallel re-runs. Use this in tests instead of
// calling Register directly.
func registerForTest(t *testing.T, name string, factory func() any) {
	t.Helper()
	Register(name, factory)
	t.Cleanup(func() {
		regMu.Lock()
		delete(registry, name)
		regMu.Unlock()
	})
}
