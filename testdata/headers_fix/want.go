package example

import "errors"

type input struct {
	Name string
}

var errFailed = errors.New("failed")

func check(value input) (int, error) { return 0, nil }

func moves() error {
	_, err := check(
		input{
			Name: "a",
		},
	)
	if err != nil {
		return err
	}
	_, err = check(
		input{
			Name: "b",
		},
	)
	if err != nil {
		return err
	}
	got := input{
		Name: "c",
	}
	want := input{
		Name: "c",
	}
	if got != want {
		return errFailed
	}
	return nil
}

func laterDeclaration() error {
	if _, err := check(
		input{
			Name: "a",
		},
	); err != nil {
		return err
	}
	_, err := check(
		input{
			Name: "b",
		},
	)
	return err
}

func elseIf(flag bool) error {
	if flag {
		return nil
	} else if _, err := check(
		input{
			Name: "a",
		},
	); err != nil {
		return err
	}
	return nil
}

func shadows() error {
	_, err := check(
		input{
			Name: "outer",
		},
	)
	for range 2 {
		if _, err := check(
			input{
				Name: "inner",
			},
		); err != nil {
			return err
		}
	}
	return err
}

func shadowedLater() error {
	if _, err := check(
		input{
			Name: "first",
		},
	); err != nil {
		return err
	}
	for range 2 {
		if _, err := check(input{}); err != nil {
			return err
		}
	}
	return nil
}
