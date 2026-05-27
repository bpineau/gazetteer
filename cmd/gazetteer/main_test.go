package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestSmoke_BuildAndVersion compiles the binary into a tempdir and
// exercises a handful of safe (network-free) sub-commands : version,
// sources list, sources doc <name>, refresh stub, normalize (which
// would hit BAN but we skip the actual address arg to validate the
// usage-error path).
//
// E2E tests that hit BAN / live HTTP backends are deliberately out of
// scope here — they would need stub servers and pull the cmd target
// further out of "smoke" territory.
func TestSmoke_BuildAndVersion(t *testing.T) {
	// CI / sandboxed environments without a usable Go toolchain on
	// PATH would falsely fail this test; check first.
	if _, err := exec.LookPath("go"); err != nil {
		t.Skipf("go toolchain not on PATH: %v", err)
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "gazetteer")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	pkg := "github.com/bpineau/gazetteer/cmd/gazetteer"
	build := exec.Command("go", "build", "-o", bin, pkg)
	build.Env = append(os.Environ(), "CGO_ENABLED=1")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	cases := []struct {
		name      string
		args      []string
		wantExit  int
		stdoutHas string
		stderrHas string
	}{
		{
			name:      "version_prints_version_string",
			args:      []string{"version"},
			wantExit:  0,
			stdoutHas: "gazetteer ",
		},
		{
			name:      "no_args_prints_usage",
			args:      []string{},
			wantExit:  1,
			stderrHas: "Usage: gazetteer <command>",
		},
		{
			name:      "sources_list_includes_dvf",
			args:      []string{"sources", "list"},
			wantExit:  0,
			stdoutHas: "dvf",
		},
		{
			name:      "sources_doc_carteloyers_emits_json",
			args:      []string{"sources", "doc", "carteloyers"},
			wantExit:  0,
			stdoutHas: "loyer_med_eur_per_m2_cc",
		},
		{
			name:      "refresh_stub_returns_not_implemented",
			args:      []string{"refresh", "dvf"},
			wantExit:  0,
			stdoutHas: "not implemented",
		},
		{
			name:      "normalize_without_addr_prints_usage",
			args:      []string{"normalize"},
			wantExit:  1,
			stderrHas: "missing <addr>",
		},
		{
			name:      "unknown_command_returns_1",
			args:      []string{"banana"},
			wantExit:  1,
			stderrHas: `unknown command "banana"`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command(bin, tc.args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err := cmd.Run()
			gotExit := 0
			if err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					gotExit = ee.ExitCode()
				} else {
					t.Fatalf("run %v: %v (stderr=%q)", tc.args, err, stderr.String())
				}
			}
			if gotExit != tc.wantExit {
				t.Errorf("exit=%d want %d\nstdout=%q\nstderr=%q", gotExit, tc.wantExit, stdout.String(), stderr.String())
			}
			if tc.stdoutHas != "" && !strings.Contains(stdout.String(), tc.stdoutHas) {
				t.Errorf("stdout missing %q; got %q", tc.stdoutHas, stdout.String())
			}
			if tc.stderrHas != "" && !strings.Contains(stderr.String(), tc.stderrHas) {
				t.Errorf("stderr missing %q; got %q", tc.stderrHas, stderr.String())
			}
		})
	}
}
