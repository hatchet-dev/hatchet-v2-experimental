import { createQueryKeyStore } from '@lukemorales/query-key-factory';

import api, { cloudApi } from './api';
import invariant from 'tiny-invariant';
import { WebhookWorkerCreateRequest } from '.';

type ListEventQuery = Parameters<typeof api.eventList>[1];
type ListRateLimitsQuery = Parameters<typeof api.rateLimitList>[1];
type ListLogLineQuery = Parameters<typeof api.logLineList>[1];
type ListWorkflowRunsQuery = Parameters<typeof api.workflowRunList>[1];
type GetTaskMetricsQuery = Parameters<typeof api.v2TaskListStatusMetrics>[1];
type V2ListWorkflowRunsQuery = Parameters<typeof api.v2WorkflowRunList>[1];
type V2ListTaskRunsQuery = Parameters<typeof api.v2TaskList>[1];
type V2TaskGetPointMetricsQuery = Parameters<
  typeof api.v2TaskGetPointMetrics
>[1];
type ListWorkflowsQuery = Parameters<typeof api.workflowList>[1];
export type ListCloudLogsQuery = Parameters<typeof cloudApi.logList>[1];
export type GetCloudMetricsQuery = Parameters<typeof cloudApi.metricsCpuGet>[1];
type WorkflowRunMetrics = Parameters<typeof api.workflowRunGetMetrics>[1];
type WorkflowRunEventsMetrics = Parameters<
  typeof cloudApi.workflowRunEventsGetMetrics
>[1];
type WorkflowScheduledQuery = Parameters<typeof api.workflowScheduledList>[1];
type CronWorkflowsQuery = Parameters<typeof api.cronWorkflowList>[1];

export const queries = createQueryKeyStore({
  cloud: {
    billing: (tenant: string) => ({
      queryKey: ['billing-state:get', tenant],
      queryFn: async () => (await cloudApi.tenantBillingStateGet(tenant)).data,
    }),
    getManagedWorker: (worker: string) => ({
      queryKey: ['managed-worker:get', worker],
      queryFn: async () => (await cloudApi.managedWorkerGet(worker)).data,
    }),
    listManagedWorkers: (tenant: string) => ({
      queryKey: ['managed-worker:list', tenant],
      queryFn: async () => (await cloudApi.managedWorkerList(tenant)).data,
    }),
    listManagedWorkerInstances: (managedWorkerId: string) => ({
      queryKey: ['managed-worker:list:instances', managedWorkerId],
      queryFn: async () =>
        (await cloudApi.managedWorkerInstancesList(managedWorkerId)).data,
    }),
    getManagedWorkerLogs: (
      managedWorkerId: string,
      query: ListCloudLogsQuery,
    ) => ({
      queryKey: ['managed-worker:get:logs', managedWorkerId, query],
      queryFn: async () =>
        (await cloudApi.logList(managedWorkerId, query)).data,
    }),
    getBuild: (buildId: string) => ({
      queryKey: ['build:get', buildId],
      queryFn: async () => (await cloudApi.buildGet(buildId)).data,
    }),
    getBuildLogs: (buildId: string) => ({
      queryKey: ['build-logs:list', buildId],
      queryFn: async () => (await cloudApi.buildLogsList(buildId)).data,
    }),
    getIacLogs: (managedWorkerId: string, deployKey: string) => ({
      queryKey: ['iac-logs:list', managedWorkerId, deployKey],
      queryFn: async () =>
        (
          await cloudApi.iacLogsList(managedWorkerId, {
            deployKey,
          })
        ).data,
    }),
    getManagedWorkerCpuMetrics: (
      managedWorkerId: string,
      query: GetCloudMetricsQuery,
    ) => ({
      queryKey: ['managed-worker:get:cpu-metrics', managedWorkerId, query],
      queryFn: async () =>
        (await cloudApi.metricsCpuGet(managedWorkerId, query)).data,
    }),
    getManagedWorkerMemoryMetrics: (
      managedWorkerId: string,
      query: GetCloudMetricsQuery,
    ) => ({
      queryKey: ['managed-worker:get:memory-metrics', managedWorkerId, query],
      queryFn: async () =>
        (await cloudApi.metricsMemoryGet(managedWorkerId, query)).data,
    }),
    getManagedWorkerDiskMetrics: (
      managedWorkerId: string,
      query: GetCloudMetricsQuery,
    ) => ({
      queryKey: ['managed-worker:get:disk-metrics', managedWorkerId, query],
      queryFn: async () =>
        (await cloudApi.metricsDiskGet(managedWorkerId, query)).data,
    }),
    listManagedWorkerEvents: (managedWorkerId: string) => ({
      queryKey: ['managed-worker:get:events', managedWorkerId],
      queryFn: async () =>
        (await cloudApi.managedWorkerEventsList(managedWorkerId)).data,
    }),
    workflowRunMetrics: (tenant: string, query: WorkflowRunEventsMetrics) => ({
      queryKey: ['workflow-run:metrics', tenant, query],
      queryFn: async () =>
        (await cloudApi.workflowRunEventsGetMetrics(tenant, query)).data,
    }),
  },
  user: {
    current: {
      queryKey: ['user:get'],
      queryFn: async () => (await api.userGetCurrent()).data,
    },
    listTenantMemberships: {
      queryKey: ['tenant-memberships:list'],
      queryFn: async () => (await api.tenantMembershipsList()).data,
    },
    listInvites: {
      queryKey: ['user:list:tenant-invites'],
      queryFn: async () => (await api.userListTenantInvites()).data,
    },
  },
  alertingSettings: {
    get: (tenant: string) => ({
      queryKey: ['tenant-alerting-settings:get', tenant],
      queryFn: async () => (await api.tenantAlertingSettingsGet(tenant)).data,
    }),
  },
  tenantResourcePolicy: {
    get: (tenant: string) => ({
      queryKey: ['tenant-resource-policy:get', tenant],
      queryFn: async () => (await api.tenantResourcePolicyGet(tenant)).data,
    }),
  },
  members: {
    list: (tenant: string) => ({
      queryKey: ['tenant-member:list', tenant],
      queryFn: async () => (await api.tenantMemberList(tenant)).data,
    }),
  },
  tokens: {
    list: (tenant: string) => ({
      queryKey: ['api-token:list', tenant],
      queryFn: async () => (await api.apiTokenList(tenant)).data,
    }),
  },
  emailGroups: {
    list: (tenant: string) => ({
      queryKey: ['email-group:list', tenant],
      queryFn: async () => (await api.alertEmailGroupList(tenant)).data,
    }),
  },
  slackWebhooks: {
    list: (tenant: string) => ({
      queryKey: ['slack-webhook:list', tenant],
      queryFn: async () => (await api.slackWebhookList(tenant)).data,
    }),
  },
  snsIntegrations: {
    list: (tenant: string) => ({
      queryKey: ['sns:list', tenant],
      queryFn: async () => (await api.snsList(tenant)).data,
    }),
  },
  invites: {
    list: (tenant: string) => ({
      queryKey: ['tenant-invite:list', tenant],
      queryFn: async () => (await api.tenantInviteList(tenant)).data,
    }),
  },
  workflows: {
    list: (tenant: string, query?: ListWorkflowsQuery) => ({
      queryKey: ['workflow:list', tenant],
      queryFn: async () => (await api.workflowList(tenant, query)).data,
    }),
    get: (workflow: string) => ({
      queryKey: ['workflow:get', workflow],
      queryFn: async () => (await api.workflowGet(workflow)).data,
    }),
    getMetrics: (workflow: string) => ({
      queryKey: ['workflow:get:metrics', workflow],
      queryFn: async () => (await api.workflowGetMetrics(workflow)).data,
    }),
    getVersion: (workflow: string, version?: string) => ({
      queryKey: ['workflow-version:get', workflow, version],
      queryFn: async () =>
        (
          await api.workflowVersionGet(workflow, {
            version: version,
          })
        ).data,
    }),
    // getDefinition: (workflow: string, version?: string) => ({
    //   queryKey: ['workflow-version:get:definition', workflow, version],
    //   queryFn: async () =>
    //     (
    //       await api.workflowVersionGetDefinition(workflow, {
    //         version: version,
    //       })
    //     ).data,
    // }),
  },
  scheduledRuns: {
    list: (tenant: string, query: WorkflowScheduledQuery) => ({
      queryKey: ['scheduled-run:list', tenant, query],
      queryFn: async () =>
        (await api.workflowScheduledList(tenant, query)).data,
    }),
  },
  cronJobs: {
    list: (tenant: string, query: CronWorkflowsQuery) => ({
      queryKey: ['cron-job:list', tenant, query],
      queryFn: async () => (await api.cronWorkflowList(tenant, query)).data,
    }),
  },
  v2WorkflowRuns: {
    list: (tenant: string, query: V2ListWorkflowRunsQuery) => ({
      queryKey: ['v2:workflow-run:list', tenant, query],
      queryFn: async () => (await api.v2WorkflowRunList(tenant, query)).data,
    }),
    listTaskEvents: (workflowRunId: string) => ({
      queryKey: ['v2:workflow-run:list-tasks', workflowRunId],
      queryFn: async () =>
        (await api.v2WorkflowRunTaskEventsList(workflowRunId)).data,
    }),
    details: (workflowRunId: string) => ({
      queryKey: ['workflow-run-details:get', workflowRunId],
      queryFn: async () =>
        (await api.v2WorkflowRunGet(workflowRunId)).data,
    }),
  },
  v2Tasks: {
    list: (tenant: string, query: V2ListTaskRunsQuery) => ({
      queryKey: ['v2-task:list', tenant, query],
      queryFn: async () => (await api.v2TaskList(tenant, query)).data,
    }),
    get: (task: string) => ({
      queryKey: ['v2-task:get', task],
      queryFn: async () => (await api.v2TaskGet(task)).data,
    }),
    getByDagId: (tenant: string, dagIds: string[]) => ({
      queryKey: ['v2-task:get-by-dag-id', dagIds],
      queryFn: async () =>
        (
          await api.v2DagListTasks({
            dag_ids: dagIds,
            tenant,
          })
        ).data,
    }),
  },
  v2TaskEvents: {
    list: (
      tenant: string,
      query: ListWorkflowRunsQuery,
      taskRunId?: string | undefined,
      workflowRunId?: string | undefined,
    ) => ({
      queryKey: [
        'v2:workflow-run:list',
        tenant,
        taskRunId,
        workflowRunId,
        query,
      ],
      queryFn: async () => {
        if (taskRunId) {
          return (await api.v2TaskEventList(taskRunId, query)).data;
        } else if (workflowRunId) {
          return (await api.v2WorkflowRunTaskEventsList(workflowRunId))
            .data;
        } else {
          throw new Error('Either task or workflowRunId must be set');
        }
      },
    }),
  },
  v2TaskRuns: {
    metrics: (tenant: string, query: GetTaskMetricsQuery) => ({
      queryKey: ['v2:task-run:metrics', tenant, query],
      queryFn: async () =>
        (await api.v2TaskListStatusMetrics(tenant, query)).data,
    }),
    pointMetrics: (tenant: string, query: V2TaskGetPointMetricsQuery) => ({
      queryKey: ['v2-task:metrics', tenant, query],
      queryFn: async () =>
        (await api.v2TaskGetPointMetrics(tenant, query)).data,
    }),
  },
  workflowRuns: {
    list: (tenant: string, query: ListWorkflowRunsQuery) => ({
      queryKey: ['workflow-run:list', tenant, query],
      queryFn: async () => (await api.workflowRunList(tenant, query)).data,
    }),
    shape: (tenant: string, workflowVersionId: string) => ({
      queryKey: ['workflow-run:get:shape', tenant, workflowVersionId],
      queryFn: async () =>
        (await api.workflowRunGetShape(tenant, workflowVersionId)).data,
    }),
    get: (tenant: string, workflowRun: string) => ({
      queryKey: ['workflow-run:get', tenant, workflowRun],
      queryFn: async () => (await api.workflowRunGet(tenant, workflowRun)).data,
    }),
    getInput: (tenant: string, workflowRun: string) => ({
      queryKey: ['workflow-run:get:input', tenant, workflowRun],
      queryFn: async () =>
        (await api.workflowRunGetInput(tenant, workflowRun)).data,
    }),
    metrics: (tenant: string, query: WorkflowRunMetrics) => ({
      queryKey: ['workflow-run:metrics', tenant, query],
      queryFn: async () =>
        (await api.workflowRunGetMetrics(tenant, query)).data,
    }),
    listStepRunEvents: (tenantId: string, workflowRun: string) => ({
      queryKey: ['workflow-run:list:step-run-events', workflowRun],
      queryFn: async () =>
        (await api.workflowRunListStepRunEvents(tenantId, workflowRun)).data,
    }),
  },
  metrics: {
    get: (tenant: string) => ({
      queryKey: ['queue-metrics:get', tenant],
      queryFn: async () => (await api.tenantGetQueueMetrics(tenant)).data,
    }),
    getStepRunQueueMetrics: (tenant: string) => ({
      queryKey: ['queue-metrics:get:step-run', tenant],
      queryFn: async () =>
        (await api.tenantGetStepRunQueueMetrics(tenant)).data,
    }),
  },
  stepRuns: {
    get: (tenant: string, stepRun: string) => ({
      queryKey: ['step-run:get', tenant, stepRun],
      queryFn: async () => (await api.stepRunGet(tenant, stepRun)).data,
    }),
    getLogs: (stepRun: string, query: ListLogLineQuery) => ({
      queryKey: ['log-lines:list', stepRun],
      queryFn: async () => (await api.logLineList(stepRun, query)).data,
    }),
    getSchema: (tenant: string, stepRun: string) => ({
      queryKey: ['step-run:get:schema', stepRun],
      queryFn: async () => (await api.stepRunGetSchema(tenant, stepRun)).data,
    }),
    listEvents: (stepRun: string) => ({
      queryKey: ['step-run:list:events', stepRun],
      queryFn: async () => (await api.stepRunListEvents(stepRun)).data,
    }),
    listArchives: (stepRun: string) => ({
      queryKey: ['step-run:list:archives', stepRun],
      queryFn: async () => (await api.stepRunListArchives(stepRun)).data,
    }),
  },
  events: {
    list: (tenant: string, query: ListEventQuery) => ({
      queryKey: ['event:list', tenant, query],
      queryFn: async () => (await api.eventList(tenant, query)).data,
    }),
    listKeys: (tenant: string) => ({
      queryKey: ['event-keys:list', tenant],
      queryFn: async () => (await api.eventKeyList(tenant)).data,
    }),
    get: (event: string) => ({
      queryKey: ['event:get', event],
      queryFn: async () => (await api.eventGet(event)).data,
    }),
    getData: (event: string) => ({
      queryKey: ['event-data:get', event],
      queryFn: async () => (await api.eventDataGet(event)).data,
    }),
  },
  rate_limits: {
    list: (tenant: string, query: ListRateLimitsQuery) => ({
      queryKey: ['rate-limits:list', tenant, query],
      queryFn: async () => (await api.rateLimitList(tenant, query)).data,
    }),
  },
  workers: {
    list: (tenant: string) => ({
      queryKey: ['worker:list', tenant],
      queryFn: async () => (await api.workerList(tenant)).data,
    }),
    get: (worker: string) => ({
      queryKey: ['worker:get', worker],
      queryFn: async () => (await api.workerGet(worker)).data,
    }),
  },
  github: {
    listInstallations: {
      queryKey: ['github-app:list:installations'],
      queryFn: async () => (await cloudApi.githubAppListInstallations()).data,
    },
    listRepos: (installation?: string) => ({
      queryKey: ['github-app:list:repos', installation],
      queryFn: async () => {
        invariant(installation, 'Installation must be set');
        const res = (await cloudApi.githubAppListRepos(installation)).data;
        return res;
      },
      enabled: !!installation,
    }),
    listBranches: (
      installation?: string,
      repoOwner?: string,
      repoName?: string,
    ) => ({
      queryKey: ['github-app:list:branches', installation, repoOwner, repoName],
      queryFn: async () => {
        invariant(installation, 'Installation must be set');
        invariant(repoOwner, 'Repo owner must be set');
        invariant(repoName, 'Repo name must be set');
        const res = (
          await cloudApi.githubAppListBranches(
            installation,
            repoOwner,
            repoName,
          )
        ).data;
        return res;
      },
      enabled: !!installation && !!repoOwner && !!repoName,
    }),
  },
  webhookWorkers: {
    list: (tenant: string) => ({
      queryKey: ['webhook-worker:list', tenant],
      queryFn: async () => (await api.webhookList(tenant)).data,
    }),
    create: (tenant: string, webhookWorker: WebhookWorkerCreateRequest) => ({
      queryKey: ['webhook-worker:create', tenant, webhookWorker],
      queryFn: async () =>
        (await api.webhookCreate(tenant, webhookWorker)).data,
    }),
    listRequests: (webhookWorkerId: string) => ({
      queryKey: ['webhook-worker:list:requests', webhookWorkerId],
      queryFn: async () =>
        (await api.webhookRequestsList(webhookWorkerId)).data,
    }),
  },

  info: {
    getVersion: {
      queryKey: ['info:version'],
      queryFn: async () => (await api.infoGetVersion()).data,
    },
  },
});
