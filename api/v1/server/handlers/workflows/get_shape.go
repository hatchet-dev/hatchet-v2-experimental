package workflows

import (
	"context"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/hatchet-dev/hatchet/api/v1/server/oas/gen"
	"github.com/hatchet-dev/hatchet/api/v1/server/oas/transformers"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/dbsqlc"
)

func (t *WorkflowService) WorkflowRunGetShape(ctx echo.Context, request gen.WorkflowRunGetShapeRequestObject) (gen.WorkflowRunGetShapeResponseObject, error) {
	reqCtx, cancel := context.WithTimeout(ctx.Request().Context(), 5*time.Second)
	defer cancel()

	rows := make([]dbsqlc.GetWorkflowRunShapeRow, 0)

	shape, err := t.config.APIRepository.WorkflowRun().GetWorkflowRunShape(
		reqCtx, request.WorkflowVersionId,
	)

	if err != nil {
		panic(err)
	}

	for _, row := range shape {
		shapeRow := *row
		rows = append(rows, shapeRow)
	}

	return gen.WorkflowRunGetShape200JSONResponse(
		transformers.ToWorkflowRunShape(rows),
	), nil
}
