package dataset

import (
	"path/filepath"
	"testing"
)

func TestResolveDir_Precedence(t *testing.T) {
	// Not parallel: mutates the process environment.
	t.Setenv(DefaultDirEnv, "/env/dir")

	if got, _ := ResolveDir("/explicit"); got != "/explicit" {
		t.Errorf("explicit should win: got %q", got)
	}
	if got, _ := ResolveDir(""); got != "/env/dir" {
		t.Errorf("env should win over default: got %q", got)
	}
	if got, _ := ResolveDir("  "); got != "/env/dir" {
		t.Errorf("blank explicit falls through to env: got %q", got)
	}
}

func TestDefaultDir_FallsBackToUserCache(t *testing.T) {
	t.Setenv(DefaultDirEnv, "")
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if filepath.Base(got) != dirName {
		t.Errorf("DefaultDir = %q, want it to end in %q", got, dirName)
	}
}
