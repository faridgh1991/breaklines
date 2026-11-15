// Command breaklines breaks Go code into lines by a few strict rules:
//
//   - When a function's parameters or a call's arguments do not fit on one
//     line, each goes on its own line, with the closing parenthesis on its
//     own line. A nested call that fits on one line is one argument.
//   - A struct literal puts each field on its own line, even when it fits on
//     one. A slice, array, or map literal does so once an element spans lines.
//   - Exceptions: a log/slog call keeps each key and value on one line, and a
//     call whose last argument is a function literal keeps the arguments
//     before it on its first line.
//
// It reports, as file:line:col diagnostics, what the rules break and it does
// not fix: a call or struct literal split inside an if or for header, and a
// multi-value assignment over several lines. With -fix-headers it moves a
// multi-line if init before the if and a struct literal out of an if
// condition.
//
// Usage:
//
//	breaklines [flags] [packages]
//
// Packages default to ./... in the current module. Without -w, breaklines
// lists the files it would change and exits 1 when there are any, for a
// pre-commit hook or CI. It never writes a result that adds type errors.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"sort"
)

func main() {
	os.Exit(run())
}

func run() int {
	write := flag.Bool("w", false, "write the changes to the files")
	verbose := flag.Bool("v", false, "print each file's new content instead of its name (without -w)")
	fixHeaders := flag.Bool(
		"fix-headers",
		false,
		"move a multi-line if init before the if and a struct literal out of an if condition",
	)
	slogPairs := flag.Bool("slog-pairs", true, "keep each key and value of a log/slog call on one line")
	trailingFunc := flag.Bool(
		"trailing-func",
		true,
		"keep the arguments before a trailing function literal on the call's first line",
	)
	exclude := flag.String("exclude", "", "skip files whose path, relative to the module, matches this regexp")
	tags := flag.String("tags", "", "build tags, as for go build -tags")
	dir := flag.String("C", ".", "run in this directory")
	force := flag.Bool("force", false, "write even when the changes add type errors")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: breaklines [flags] [packages]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println(buildVersion())
		return 0
	}

	absolute, err := filepath.Abs(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "breaklines:", err)
		return 2
	}
	patterns := flag.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	runner := Run{
		Dir:      absolute,
		Patterns: patterns,
		Tags:     *tags,
		Options: Options{
			SlogPairs:       *slogPairs,
			TrailingFuncLit: *trailingFunc,
			FixHeaders:      *fixHeaders,
		},
	}
	if *exclude != "" {
		if runner.Exclude, err = regexp.Compile(*exclude); err != nil {
			fmt.Fprintln(os.Stderr, "breaklines: -exclude:", err)
			return 2
		}
	}
	result, err := runner.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, "breaklines:", err)
		return 2
	}
	if len(result.NewErrors) > 0 {
		fmt.Fprintln(os.Stderr, "breaklines: the changes add type errors:")
		for _, message := range result.NewErrors {
			fmt.Fprintln(os.Stderr, "  "+message)
		}
		if *write && !*force {
			fmt.Fprintln(os.Stderr, "breaklines: nothing written; fix by hand or rerun with -force")
			return 2
		}
	}
	names := make([]string, 0, len(result.Changed))
	for name := range result.Changed {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		rel, relErr := filepath.Rel(absolute, name)
		if relErr != nil {
			rel = name
		}
		switch {
		case *write:
			if writeErr := writeFile(name, result.Changed[name]); writeErr != nil {
				fmt.Fprintln(os.Stderr, "breaklines:", writeErr)
				return 2
			}
			fmt.Println("wrote", rel)
		case *verbose:
			fmt.Printf("=== %s\n%s", rel, result.Changed[name])
		default:
			fmt.Println(rel)
		}
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Println(diagnostic)
	}
	if !*write && len(result.Changed) > 0 || len(result.Diagnostics) > 0 {
		return 1
	}
	return 0
}

func writeFile(name string, content []byte) error {
	info, err := os.Stat(name)
	if err != nil {
		return err
	}
	return os.WriteFile(name, content, info.Mode().Perm())
}

func buildVersion() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "devel"
}
