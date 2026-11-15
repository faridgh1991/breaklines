package example

import "context"

func packed(
	ctx context.Context,
	first string,
	second int,
	third bool,
) (string, error) {
	return "", nil
}

func short(a, b int) int { return a + b }

type store interface {
	Save(
		ctx context.Context,
		name string,
		size int,
	) error
}

var literal = func(
	first string,
	second int,
) {
}
