package example

import (
	"context"
	"log/slog"
)

func logs(ctx context.Context, logger *slog.Logger, id int, err error) {
	logger.InfoContext(
		ctx,
		"started",
		"id",
		id,
		"error",
		err,
	)
	logger.Warn("short", "id", id)
	slog.Error(
		"package level",
		"id",
		id,
		"error",
		err,
		"retry",
		true,
	)
	logger.With(
		"id",
		id,
		"name",
		"n",
	).Info("with")
}
