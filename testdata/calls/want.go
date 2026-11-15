package example

import "fmt"

func join(parts ...string) string { return fmt.Sprint(parts) }

func add(a, b, c int) int { return a + b + c }

type options struct {
	Name string
	Size int
}

func configure(name string, value options) string { return name + value.Name }

func calls(parts []string) string {
	short := add(1, 2, 3)
	_ = short
	nested := add(
		add(1, 2, 3),
		add(4, 5, 6),
		7,
	)
	_ = nested
	text := fmt.Sprintf(
		"%s and %s and %s and %d",
		"first",
		"second",
		"third",
		nested,
	)
	_ = text
	spread := append(
		[]string{"a"},
		parts...,
	)
	_ = spread
	trailing := add(
		1,
		2,
		3,
	)
	_ = trailing
	return configure(
		"x",
		options{
			Name: "y",
			Size: 1,
		},
	)
}
