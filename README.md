# breaklines

`breaklines` breaks Go code into lines by a few strict rules, which `gofmt` leaves to the author, and formats the
result with `go/format`. It reads the code with full type information, so it tells a struct literal from a slice or
map literal and a `log/slog` call from any other call.

## Rules

**Calls and parameter lists.** When a function's parameters, or a call's arguments, do not fit on one line, each goes
on its own line, with the closing parenthesis on its own line. A nested call that fits on one line is one argument and
stays on its line.

```go
order, err := client.CreateOrder(
	customerID,
	total,
	time.Second,
)
```

**Struct literals.** A struct literal puts each field on its own line, even when it would fit on one. A slice, array,
or map literal does the same once one of its elements spans lines; `[]int{1, 2, 3}` stays as it is.

```go
return Output{
	ID:     entry.ID,
	Amount: entry.Amount,
}
```

A call that takes a struct literal no longer fits on one line, so its arguments go on their own lines too.

**Exceptions.**

- A `log/slog` call keeps each key and its value on one line (`-slog-pairs`, on by default):

  ```go
  logger.InfoContext(
  	ctx,
  	"items processed",
  	"customer_id", customerID,
  	"total", total,
  )
  ```

- A call whose last argument is a function literal keeps the arguments before it on its first line, as Go code
  usually does (`-trailing-func`, on by default): `t.Run(name, func(t *testing.T) {`.

**Headers.** Calls and struct literals in `if` conditions are allowed. A call in a `for` condition and a multi-value
assignment over several lines (`a, b :=` with `x, y` on the next line) are reported. With `-fix-headers`:

- A multi-line `if x := ...; cond {` init moves before the `if`. A `:=` whose variables all exist earlier in the same
  block becomes `=`. The move is refused, and reported, when it would change which variable the code refers to: when
  the block declares one of the variables later, or when one would shadow, or be shadowed by, another of that name.
  An `else if` init is never moved.
- `if got != (T{...}) {` becomes `want := T{...}` before the `if` and `if got != want {`; the name is `want`, or
  `wantT` when the function already uses `want`.

## Install

```sh
go install github.com/faridgh1991/breaklines@latest
```

This puts `breaklines` in `$(go env GOPATH)/bin`. The `-version` flag reports the module version used to build the binary. It needs the Go toolchain the checked code builds with, since it
loads packages through the `go` command.

## Use

```sh
breaklines                    # list the files that would change, and the diagnostics; change nothing
breaklines -w                 # write the changes
breaklines -w -fix-headers    # also move inits and literals out of if headers
breaklines -version           # print the installed version
breaklines -v ./internal/...  # print the new content of the files that would change
breaklines -C ../other-project -exclude '^internal/legacy/'
```

| Flag | Default | Meaning |
|---|---|---|
| `-w` | off | Write the changes. Without it, nothing on disk changes. |
| `-v` | off | Without `-w`, print each changed file's new content instead of its name. |
| `-fix-headers` | off | Move multi-line `if` inits and struct literals out of `if` headers. |
| `-slog-pairs` | on | Keep each key and value of a `log/slog` call on one line. |
| `-trailing-func` | on | Keep the arguments before a trailing function literal on the first line. |
| `-exclude` | none | Skip files whose path, relative to the module, matches this regexp. |
| `-tags` | none | Build tags, as for `go build -tags`. |
| `-C` | `.` | Run in this directory. |
| `-force` | off | Write even when the changes add type errors. Avoid it. |

Packages default to `./...`. Test files are included. Generated files (those with a `// Code generated ... DO NOT
EDIT.` line) are skipped.

**Exit status.** `0`: nothing to change and nothing to report. `1`: files would change (without `-w`), or diagnostics
remain. `2`: an error, or changes that would add type errors (nothing is written).

**Diagnostics** print as `file:line:col: message`, with positions in the reformatted file, as `-w` writes it. Run
with `-w` first, then fix what remains by hand. A summary groups diagnostics by message and prints their total.

## Safety

- It computes every pass in memory and touches disk only with `-w`, after the last pass.
- After the last pass it type-checks the result and refuses to write when it has type errors the original did not.
- It only inserts line breaks, except for the opt-in `-fix-headers`, which moves an `if` init or a struct literal
  without changing which variable any code refers to.
- It repeats passes until nothing changes, so a second run changes nothing.

Still run your tests and linters afterwards, and review the diff before you commit it.

## In a pre-commit hook or CI

```sh
# .githooks/pre-commit
breaklines >/dev/null || { echo "breaklines: run 'breaklines -w' and fix the diagnostics"; exit 1; }
```

```yaml
# .gitlab-ci.yml
breaklines:
  script:
    - go install github.com/faridgh1991/breaklines@latest   # or build it from a copy in the repository
    - breaklines
```

## Limits

- Files excluded by build tags are not seen; pass `-tags` to include them.
- Long boolean conditions split over lines (`if a ||` then `b {`) are not a rule and are left alone.
- An unkeyed struct literal stays unkeyed; only its fields go on their own lines.
- Without `-fix-headers` it only reports header problems; with it, some stay reported for a hand fix.

## Develop

```sh
go test ./...                       # golden tests in testdata/<case>/{input.go,want.go,diagnostics.txt}
go test ./... -run TestGolden -update   # rewrite the golden files after a change; review the diff
```

Each case runs `input.go` as package `example` in a module of its own, with the options listed in `cases` in
`breaklines_test.go`, and `TestIdempotent` checks that a second run over `want.go` changes nothing.
