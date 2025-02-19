import { useOutletContext, useParams } from 'react-router-dom';

import { V2TaskStatus, WorkflowRunStatus, queries } from '@/lib/api';
import { useQuery } from '@tanstack/react-query';
import invariant from 'tiny-invariant';
import { TenantContextType } from '@/lib/outlet';
import { Button } from '@/components/ui/button';
import {
  AdjustmentsHorizontalIcon,
  ArrowPathIcon,
  XCircleIcon,
} from '@heroicons/react/24/outline';
import { ArrowTopRightIcon } from '@radix-ui/react-icons';
import {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbSeparator,
  BreadcrumbPage,
} from '@/components/ui/breadcrumb';
import { formatDuration } from '@/lib/utils';
import RelativeDate from '@/components/molecules/relative-date';
import { useTenant } from '@/lib/atoms';

export const WORKFLOW_RUN_TERMINAL_STATUSES = [
  WorkflowRunStatus.CANCELLED,
  WorkflowRunStatus.FAILED,
  WorkflowRunStatus.SUCCEEDED,
];

export const V2RunDetailHeader = () => {
  const { tenant } = useOutletContext<TenantContextType>();
  const params = useParams();

  invariant(tenant);
  invariant(params.run);

  const { isLoading: loading, data } = useQuery({
    ...queries.v2WorkflowRuns.details(tenant.metadata.id, params.run),
  });

  if (loading || !data) {
    return <div>Loading...</div>;
  }

  const workflowRun = data?.run;

  return (
    <div className="flex flex-col gap-4">
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink href="/">Home</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbLink href="/workflow-runs">Workflow Runs</BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>{workflowRun.displayName}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>
      <div className="flex flex-row justify-between items-center">
        <div className="flex flex-row justify-between items-center w-full">
          <div>
            <h2 className="text-2xl font-bold leading-tight text-foreground flex flex-row gap-4 items-center">
              <AdjustmentsHorizontalIcon className="w-5 h-5 mt-1" />
              {workflowRun.displayName}
            </h2>
          </div>
          <div className="flex flex-row gap-2 items-center">
            <a
              href={`/workflows/${workflowRun.workflowId}`}
              target="_blank"
              rel="noreferrer"
            >
              <Button size={'sm'} className="px-2 py-2 gap-2" variant="outline">
                <ArrowTopRightIcon className="w-4 h-4" />
                Workflow Definition
              </Button>
            </a>
            <Button
              size={'sm'}
              className="px-2 py-2 gap-2"
              variant={'outline'}
              disabled
            >
              <ArrowPathIcon className="w-4 h-4" />
              Replay
            </Button>
            <Button
              size={'sm'}
              className="px-2 py-2 gap-2"
              variant={'outline'}
              disabled
            >
              <XCircleIcon className="w-4 h-4" />
              Cancel
            </Button>
          </div>
        </div>
      </div>
      {/* {data.triggeredBy?.parentWorkflowRunId && (
        <TriggeringParentWorkflowRunSection
          tenantId={data.tenantId}
          parentWorkflowRunId={data.triggeredBy.parentWorkflowRunId}
        />
      )} */}
      {/* {data.triggeredBy?.eventId && (
        <TriggeringEventSection eventId={data.triggeredBy.eventId} />
      )} */}
      {/* {data.triggeredBy?.cronSchedule && (
        <TriggeringCronSection cron={data.triggeredBy.cronSchedule} />
      )} */}
      <div className="flex flex-row gap-2 items-center">
        <V2RunSummary />
      </div>
    </div>
  );
};

export const V2RunSummary = () => {
  const { tenant } = useTenant();
  const params = useParams();

  invariant(tenant);
  invariant(params.run);

  const { data } = useQuery({
    ...queries.v2Tasks.get(params.run),
  });

  const timings = [];

  if (!data) {
    return null;
  }

  timings.push(
    <div key="created" className="text-sm text-muted-foreground">
      {'Created '}
      <RelativeDate date={data.metadata.createdAt} />
    </div>,
  );

  if (data.startedAt) {
    timings.push(
      <div key="started" className="text-sm text-muted-foreground">
        {'Started '}
        <RelativeDate date={data.startedAt} />
      </div>,
    );
  } else {
    timings.push(
      <div key="running" className="text-sm text-muted-foreground">
        Running
      </div>,
    );
  }

  if (data.status === V2TaskStatus.CANCELLED && data.finishedAt) {
    timings.push(
      <div key="finished" className="text-sm text-muted-foreground">
        {'Cancelled '}
        <RelativeDate date={data.finishedAt} />
      </div>,
    );
  }

  if (data.status === V2TaskStatus.FAILED && data.finishedAt) {
    timings.push(
      <div key="finished" className="text-sm text-muted-foreground">
        {'Failed '}
        <RelativeDate date={data.finishedAt} />
      </div>,
    );
  }

  if (data.status === V2TaskStatus.COMPLETED && data.finishedAt) {
    timings.push(
      <div key="finished" className="text-sm text-muted-foreground">
        {'Succeeded '}
        <RelativeDate date={data.finishedAt} />
      </div>,
    );
  }

  if (data.duration) {
    timings.push(
      <div key="duration" className="text-sm text-muted-foreground">
        Run took {formatDuration(data.duration)}
      </div>,
    );
  }

  // interleave the timings with a dot
  const interleavedTimings: JSX.Element[] = [];

  timings.forEach((timing, index) => {
    interleavedTimings.push(timing);
    if (index < timings.length - 1) {
      interleavedTimings.push(
        <div key={`dot-${index}`} className="text-sm text-muted-foreground">
          |
        </div>,
      );
    }
  });

  return (
    <div className="flex flex-row gap-4 items-center">{interleavedTimings}</div>
  );
};
