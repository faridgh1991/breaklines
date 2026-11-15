package example

type point struct {
	X, Y int
}

type line struct {
	From, To point
	Label    string
}

var origin = point{
	X: 0,
	Y: 0,
}

var empty = point{}

var unkeyed = point{
	1,
	2,
}

var nested = line{
	From: point{
		X: 1,
		Y: 2,
	},
	To: point{
		X: 3,
		Y: 4,
	},
	Label: "a",
}

var pointer = &point{
	X: 5,
	Y: 6,
}

var single = point{
	X: 7,
}

var points = []point{
	{
		X: 1,
		Y: 2,
	},
	{
		X: 3,
		Y: 4,
	},
}

var byName = map[string]point{
	"a": {
		X: 1,
		Y: 2,
	},
}

var numbers = []int{1, 2, 3}

var table = []struct {
	name string
	want int
}{
	{
		"one",
		1,
	},
	{
		"two",
		2,
	},
}
