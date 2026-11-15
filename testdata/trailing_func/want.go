package example

func run(name string, body func()) { body() }

func withTx(id int, name string, body func() error) error { return body() }

func trailing() error {
	run("name", func() {
		_ = 1
	})
	err := withTx(1, "x", func() error {
		return nil
	})
	_ = err
	return withTx(
		1,
		"spans",
		func() error {
			return nil
		},
	)
}
