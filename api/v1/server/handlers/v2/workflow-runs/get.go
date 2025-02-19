package workflowruns

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/hatchet-dev/hatchet/api/v1/server/oas/gen"
	"github.com/hatchet-dev/hatchet/api/v1/server/oas/transformers/v2"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/db"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/sqlchelpers"
)

func (t *V2WorkflowRunsService) V2WorkflowRunGet(ctx echo.Context, request gen.V2WorkflowRunGetRequestObject) (gen.V2WorkflowRunGetResponseObject, error) {
	tenant := ctx.Get("tenant").(*db.TenantModel)
	workflowRunId := request.WorkflowRunId
	requestContext := ctx.Request().Context()

	taskRunEvents, err := t.config.EngineRepository.OLAP().ListTaskRunEventsByWorkflowRunId(
		requestContext,
		tenant.ID,
		workflowRunId,
	)

	if err != nil {
		return nil, err
	}

	workflowRunPtr, taskMetadata, err := t.config.OLAPRepository.ReadWorkflowRun(requestContext, sqlchelpers.UUIDFromStr(tenant.ID), sqlchelpers.UUIDFromStr(workflowRunId.String()))

	if err != nil {
		return nil, err
	}

	tasks, err := t.config.OLAPRepository.ListTasksByIdAndInsertedAt(
		requestContext,
		tenant.ID,
		taskMetadata,
	)

	stepIdToTaskExternalId := make(map[pgtype.UUID]pgtype.UUID)
	for _, task := range tasks {
		stepIdToTaskExternalId[task.StepID] = task.ExternalID
	}

	workflowRun := *workflowRunPtr
	workflowVersionId := uuid.MustParse(sqlchelpers.UUIDToStr(workflowRun.WorkflowVersionId))

	shape, err := t.config.APIRepository.WorkflowRun().GetWorkflowRunShape(
		requestContext, workflowVersionId,
	)

	if err != nil {
		return nil, err
	}

	result, err := transformers.ToWorkflowRunDetails(taskRunEvents, &workflowRun, shape, tasks, stepIdToTaskExternalId)

	if err != nil {
		return nil, err
	}

	// Search for api errors to see how we handle errors in other cases
	return gen.V2WorkflowRunGet200JSONResponse(
		result,
	), nil
}
