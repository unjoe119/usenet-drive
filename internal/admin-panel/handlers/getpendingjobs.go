package handlers

import (
	"net/http"
	"strconv"

	uploadqueue "github.com/javi11/usenet-drive/internal/upload-queue"
	"github.com/labstack/echo/v4"
)

func BuildGetPendingJobsHandler(queue uploadqueue.UploadQueue) echo.HandlerFunc {
	return func(c echo.Context) error {
		limit := 10
		offset := 0
		if limitStr := c.QueryParam("limit"); limitStr != "" {
			limit, _ = strconv.Atoi(limitStr)
		}
		if offsetStr := c.QueryParam("offset"); offsetStr != "" {
			offset, _ = strconv.Atoi(offsetStr)
		}

		result, err := queue.GetPendingJobs(c.Request().Context(), limit, offset)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}

		return c.JSON(http.StatusOK, result)
	}
}