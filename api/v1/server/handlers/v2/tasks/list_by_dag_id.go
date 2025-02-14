package tasks

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/labstack/echo/v4"

	"github.com/hatchet-dev/hatchet/api/v1/server/oas/gen"
	"github.com/hatchet-dev/hatchet/api/v1/server/oas/transformers/v2"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/db"
	"github.com/hatchet-dev/hatchet/pkg/repository/prisma/sqlchelpers"
)

func (t *TasksService) V2DagListTasks(ctx echo.Context, request gen.V2DagListTasksRequestObject) (gen.V2DagListTasksResponseObject, error) {
	tenant := ctx.Get("tenant").(*db.TenantModel)
	dagIds := request.Params.DagIds

	pguuids := make([]pgtype.UUID, 0)
	for _, dagId := range dagIds {
		pguuids = append(pguuids, sqlchelpers.UUIDFromStr(dagId.String()))
	}

	tasks, err := t.config.EngineRepository.OLAP().ListTasksByDAGId(
		ctx.Request().Context(),
		tenant.ID,
		pguuids,
	)

	if err != nil {
		return nil, err
	}

	result := transformers.ToDagChildren(tasks)

	// Search for api errors to see how we handle errors in other cases
	return gen.V2DagListTasks200JSONResponse(
		result,
	), nil
}
