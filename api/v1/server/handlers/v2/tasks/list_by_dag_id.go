package tasks

import (
	"github.com/labstack/echo/v4"

	"github.com/hatchet-dev/hatchet/api/v1/server/oas/gen"
	"github.com/hatchet-dev/hatchet/api/v1/server/oas/transformers/v2"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/db"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/timescalev2"
)

func (t *TasksService) V2DagListTasks(ctx echo.Context, request gen.V2DagListTasksRequestObject) (gen.V2DagListTasksResponseObject, error) {
	tenant := ctx.Get("tenant").(*db.TenantModel)
	dag := ctx.Get("dag").(*timescalev2.V2DagsOlap)

	tasks, err := t.config.EngineRepository.OLAP().ListTasksByDAGId(
		ctx.Request().Context(),
		tenant.ID,
		dag.ID,
		dag.InsertedAt,
	)

	if err != nil {
		return nil, err
	}

	result := transformers.ToTaskSummaryRows(tasks)

	// Search for api errors to see how we handle errors in other cases
	return gen.V2DagListTasks200JSONResponse(
		result,
	), nil
}
