package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files from the current output")

// Each case is testdata/<name>/input.go, run as package "example" in a module
// of its own. want.go is the expected file, and diagnostics.txt the expected
// diagnostics, one per line.
var cases = map[string]Options{
	"struct_literals": {},
	"calls":           {},
	"slog": {
		SlogPairs: true,
	},
	"slog_off": {},
	"trailing_func": {
		TrailingFuncLit: true,
	},
	"params":         {},
	"headers_report": {},
	"headers_fix": {
		FixHeaders: true,
	},
	"already_done": {
		SlogPairs:       true,
		TrailingFuncLit: true,
		FixHeaders:      true,
	},
}

func TestGolden(t *testing.T) {
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			caseDir := filepath.Join("testdata", name)
			input, err := os.ReadFile(filepath.Join(caseDir, "input.go"))
			if err != nil {
				t.Fatal(err)
			}
			module := t.TempDir()
			writeTestFile(t, filepath.Join(module, "go.mod"), "module example\n\ngo 1.22\n")
			writeTestFile(t, filepath.Join(module, "input.go"), string(input))

			result, err := Run{
				Dir:      module,
				Patterns: []string{"./..."},
				Options:  options,
			}.Execute()
			if err != nil {
				t.Fatal(err)
			}
			if len(result.NewErrors) > 0 {
				t.Fatalf("the changes add type errors: %v", result.NewErrors)
			}
			got := string(input)
			if changed, ok := result.Changed[filepath.Join(module, "input.go")]; ok {
				got = string(changed)
			}
			var lines []string
			for _, diagnostic := range result.Diagnostics {
				lines = append(lines, diagnostic.String())
			}
			gotDiagnostics := strings.Join(lines, "\n")
			if gotDiagnostics != "" {
				gotDiagnostics += "\n"
			}
			compare(t, filepath.Join(caseDir, "want.go"), got)
			compare(t, filepath.Join(caseDir, "diagnostics.txt"), gotDiagnostics)
		})
	}
}

// TestIdempotent checks that a second run over a case's output changes nothing.
func TestIdempotent(t *testing.T) {
	for name, options := range cases {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", name, "want.go"))
			if err != nil {
				t.Skip("no golden file yet")
			}
			module := t.TempDir()
			writeTestFile(t, filepath.Join(module, "go.mod"), "module example\n\ngo 1.22\n")
			writeTestFile(t, filepath.Join(module, "input.go"), string(want))
			result, err := Run{
				Dir:      module,
				Patterns: []string{"./..."},
				Options:  options,
			}.Execute()
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Changed) != 0 {
				t.Fatalf("a second run changed:\n%s", result.Changed[filepath.Join(module, "input.go")])
			}
		})
	}
}

func TestApplyKeepsGroupsWhole(t *testing.T) {
	src := []byte("abcdefgh")
	got := string(
		apply(
			src,
			[]change{
				{
					start: 1,
					end:   1,
					text:  "X",
					group: 1,
				},
				{
					start: 5,
					end:   7,
					text:  "Y",
					group: 1,
				},
				{
					start: 6,
					end:   6,
					text:  "Z",
					group: 2,
				}, // inside group 1's replacement
			},
		),
	)
	// Group 1 ends last, so it goes first and whole; group 2, inside its
	// replacement, waits for the next pass.
	if got != "aXbcdeYh" {
		t.Fatalf("apply = %q", got)
	}
}

func writeTestFile(t *testing.T, name, content string) {
	t.Helper()
	if err := os.WriteFile(name, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func compare(t *testing.T, golden, got string) {
	t.Helper()
	if *update {
		if got == "" {
			_ = os.Remove(golden)
			return
		}
		writeTestFile(t, golden, got)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("%s differs:\n--- got\n%s\n--- want\n%s", golden, got, want)
	}
}
