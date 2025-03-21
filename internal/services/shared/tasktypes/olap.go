package tasktypes

import (
	"fmt"
	"time"

	"github.com/hatchet-dev/hatchet/internal/msgqueue"
	"github.com/hatchet-dev/hatchet/internal/services/dispatcher/contracts"
	v2 "github.com/hatchet-dev/hatchet/pkg/repository/v2"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/olapv2"
	"github.com/hatchet-dev/hatchet/pkg/repository/v2/sqlcv2"
)

type CreatedTaskPayload struct {
	*sqlcv2.V2Task
}

func CreatedTaskMessage(tenantId string, task *sqlcv2.V2Task) (*msgqueue.Message, error) {
	return msgqueue.NewTenantMessage(
		tenantId,
		"created-task",
		false,
		true,
		CreatedTaskPayload{
			V2Task: task,
		},
	)
}

type CreatedDAGPayload struct {
	*v2.DAGWithData
}

func CreatedDAGMessage(tenantId string, dag *v2.DAGWithData) (*msgqueue.Message, error) {
	return msgqueue.NewTenantMessage(
		tenantId,
		"created-dag",
		false,
		true,
		CreatedDAGPayload{
			DAGWithData: dag,
		},
	)
}

type CreateMonitoringEventPayload struct {
	TaskId int64 `json:"task_id"`

	RetryCount int32 `json:"retry_count"`

	WorkerId *string `json:"worker_id,omitempty"`

	EventType olapv2.V2EventTypeOlap `json:"event_type"`

	EventTimestamp time.Time `json:"event_timestamp" validate:"required"`
	EventPayload   string    `json:"event_payload" validate:"required"`
	EventMessage   string    `json:"event_message,omitempty"`
}

func MonitoringEventMessageFromActionEvent(tenantId string, taskId int64, retryCount int32, request *contracts.StepActionEvent) (*msgqueue.Message, error) {
	payload := CreateMonitoringEventPayload{
		TaskId:         taskId,
		RetryCount:     retryCount,
		WorkerId:       &request.WorkerId,
		EventTimestamp: request.EventTimestamp.AsTime(),
		EventPayload:   request.EventPayload,
	}

	switch request.EventType {
	case contracts.StepActionEventType_STEP_EVENT_TYPE_COMPLETED:
		payload.EventType = olapv2.V2EventTypeOlapFINISHED
	case contracts.StepActionEventType_STEP_EVENT_TYPE_FAILED:
		payload.EventType = olapv2.V2EventTypeOlapFAILED
	case contracts.StepActionEventType_STEP_EVENT_TYPE_STARTED:
		payload.EventType = olapv2.V2EventTypeOlapSTARTED
	default:
		return nil, fmt.Errorf("unknown event type: %s", request.EventType.String())
	}

	return msgqueue.NewTenantMessage(
		tenantId,
		"create-monitoring-event",
		false,
		false,
		payload,
	)
}

func MonitoringEventMessageFromInternal(tenantId string, payload CreateMonitoringEventPayload) (*msgqueue.Message, error) {
	return msgqueue.NewTenantMessage(
		tenantId,
		"create-monitoring-event",
		false,
		false,
		payload,
	)
}
