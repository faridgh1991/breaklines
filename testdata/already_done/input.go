package example

import "log/slog"

type point struct {
	X int
	Y int
}

func done(logger *slog.Logger) point {
	logger.Info(
		"done",
		"x", 1,
	)
	return point{
		X: 1,
		Y: 2,
	}
}
