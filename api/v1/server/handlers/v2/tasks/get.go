package tasks

import (
	"github.com/labstack/echo/v4"

	"github.com/hatchet-dev/hatchet/api/v1/server/oas/gen"
	"github.com/hatchet-dev/hatchet/api/v1/server/oas/transformers/v2"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/olapv2"
)

func (t *TasksService) V2TaskGet(ctx echo.Context, request gen.V2TaskGetRequestObject) (gen.V2TaskGetResponseObject, error) {
	task := ctx.Get("task").(*olapv2.V2TasksOlap)

	taskWithData, workflowRunExternalId, err := t.config.EngineRepository.OLAP().ReadTaskRunData(ctx.Request().Context(), task.TenantID, task.ID, task.InsertedAt)

	if err != nil {
		return nil, err
	}

	result := transformers.ToTask(taskWithData, workflowRunExternalId)

	return gen.V2TaskGet200JSONResponse(
		result,
	), nil
}
