package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// maxPasses bounds the passes of one run; each pass only adds line breaks
// or moves code out of headers, so a few passes always settle.
const maxPasses = 100

// Run is one run of the rules over a module directory.
type Run struct {
	Dir      string
	Patterns []string
	Options  Options
	// Exclude skips files whose path relative to Dir matches it.
	Exclude *regexp.Regexp
	// Tags are build tags, passed to the go command as -tags.
	Tags string
}

// Result is what a run would change and what it cannot fix.
type Result struct {
	// Changed maps each file that changes to its new content.
	Changed     map[string][]byte
	Diagnostics []Diagnostic
	// NewErrors are type errors the changes introduced; a result with any is
	// never written.
	NewErrors []string
}

// Execute applies the rules in memory until nothing changes.
func (run Run) Execute() (Result, error) {
	overlay := map[string][]byte{}
	original := map[string][]byte{}
	initialErrors, _, _, err := run.pass(overlay, original, false)
	if err != nil {
		return Result{}, err
	}
	for range maxPasses {
		_, changed, _, passErr := run.pass(overlay, original, true)
		if passErr != nil {
			return Result{}, passErr
		}
		if !changed {
			break
		}
	}
	finalErrors, _, diagnostics, err := run.pass(overlay, original, false)
	if err != nil {
		return Result{}, err
	}
	result := Result{
		Changed:     map[string][]byte{},
		Diagnostics: diagnostics,
	}
	for name, content := range overlay {
		if !bytes.Equal(content, original[name]) {
			result.Changed[name] = content
		}
	}
	for message := range finalErrors {
		if !initialErrors[message] {
			result.NewErrors = append(result.NewErrors, message)
		}
	}
	sort.Strings(result.NewErrors)
	return result, nil
}

// pass loads the packages with overlay, and with update applies the changes
// the rules find to overlay. It returns the type errors by message, whether
// anything changed, and the diagnostics.
func (run Run) pass(
	overlay map[string][]byte,
	original map[string][]byte,
	update bool,
) (map[string]bool, bool, []Diagnostic, error) {
	fset := token.NewFileSet()
	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo,
		Dir:     run.Dir,
		Fset:    fset,
		Tests:   true,
		Overlay: overlay,
	}
	if run.Tags != "" {
		config.BuildFlags = []string{"-tags=" + run.Tags}
	}
	loaded, err := packages.Load(config, run.Patterns...)
	if err != nil {
		return nil, false, nil, fmt.Errorf("load packages: %w", err)
	}
	typeErrors := map[string]bool{}
	var diagnostics []Diagnostic
	changed := false
	done := map[string]bool{}
	for _, pkg := range loaded {
		for _, loadErr := range pkg.Errors {
			typeErrors[relative(run.Dir, loadErr.Error())] = true
		}
		for _, file := range pkg.Syntax {
			name := fset.File(file.Pos()).Name()
			if done[name] || !run.includes(name, file) {
				continue
			}
			done[name] = true
			src, readErr := source(name, overlay)
			if readErr != nil {
				return nil, false, nil, readErr
			}
			if _, seen := original[name]; !seen {
				original[name] = src
			}
			rules := &fileRules{
				fset:    fset,
				src:     src,
				info:    pkg.TypesInfo,
				pkg:     pkg.Types,
				options: run.Options,
			}
			rules.check(file)
			for _, diagnostic := range rules.diagnostics {
				diagnostic.Position.Filename = relative(run.Dir, diagnostic.Position.Filename)
				diagnostics = append(diagnostics, diagnostic)
			}
			if !update || len(rules.changes) == 0 {
				continue
			}
			formatted, formatErr := format.Source(apply(src, rules.changes))
			if formatErr != nil {
				return nil, false, nil, fmt.Errorf("format %s after changes: %w", name, formatErr)
			}
			if !bytes.Equal(formatted, src) {
				overlay[name] = formatted
				changed = true
			}
		}
	}
	sort.Slice(diagnostics, func(i, j int) bool { return diagnostics[i].String() < diagnostics[j].String() })
	return typeErrors, changed, diagnostics, nil
}

func (run Run) includes(name string, file *ast.File) bool {
	if !strings.HasSuffix(name, ".go") || ast.IsGenerated(file) {
		return false
	}
	// Files the go command generates for tests and cgo live in its cache.
	if !strings.HasPrefix(name, run.Dir+string(filepath.Separator)) {
		return false
	}
	return run.Exclude == nil || !run.Exclude.MatchString(relative(run.Dir, name))
}

func source(name string, overlay map[string][]byte) ([]byte, error) {
	if content, ok := overlay[name]; ok {
		return content, nil
	}
	content, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", name, err)
	}
	return content, nil
}

func relative(dir, text string) string {
	return strings.ReplaceAll(text, dir+string(filepath.Separator), "")
}
