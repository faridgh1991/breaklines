package example

type input struct {
	Name string
}

func check(value input) (int, error) { return 0, nil }

func reports() (int, error) {
	if _, err := check(
		input{
			Name: "a",
		},
	); err != nil {
		return 0, err
	}
	got := input{
		Name: "b",
	}
	if got != (input{
		Name: "b",
	}) {
		return 1, nil
	}
	first, second :=
		1, 2
	return first + second, nil
}
