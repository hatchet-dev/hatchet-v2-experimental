import { queries, V2TaskStatus, WorkflowRunStatus } from '@/lib/api';
import { TenantContextType } from '@/lib/outlet';
import { useQuery } from '@tanstack/react-query';
import { useOutletContext, useParams } from 'react-router-dom';
import invariant from 'tiny-invariant';
import { WorkflowRunInputDialog } from './v2components/workflow-run-input';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { StepRunEvents } from './v2components/step-run-events-for-workflow-run';
import { useCallback, useState } from 'react';
import StepRunDetail, {
  TabOption,
} from './v2components/step-run-detail/step-run-detail';
import { Separator } from '@/components/ui/separator';
import { CodeHighlighter } from '@/components/ui/code-highlighter';
import { Sheet, SheetContent } from '@/components/ui/sheet';
import { V2RunDetailHeader } from './v2components/header';
import { Badge } from '@/components/ui/badge';
import { ViewToggle } from './v2components/view-toggle';
import WorkflowRunVisualizer from './v2components/workflow-run-visualizer-v2';

export const WORKFLOW_RUN_TERMINAL_STATUSES = [
  WorkflowRunStatus.CANCELLED,
  WorkflowRunStatus.FAILED,
  WorkflowRunStatus.SUCCEEDED,
];

function statusToBadgeVariant(status: V2TaskStatus) {
  switch (status) {
    case V2TaskStatus.COMPLETED:
      return 'successful';
    case V2TaskStatus.FAILED:
    case V2TaskStatus.CANCELLED:
      return 'failed';
    default:
      return 'inProgress';
  }
}

export default function ExpandedWorkflowRun() {
  const { tenant } = useOutletContext<TenantContextType>();
  const params = useParams();
  const [selectedTaskRunId, setSelectedTaskRunId] = useState<string>();
  const [isSidebarOpen, setIsSidebarOpen] = useState(false);

  const handleTaskRunExpand = useCallback((taskRunId: string) => {
    setSelectedTaskRunId(taskRunId);
    setIsSidebarOpen(true);
  }, []);

  invariant(tenant);
  invariant(params.run);

  const { data, isLoading, isError } = useQuery({
    ...queries.v2WorkflowRuns.details(tenant.metadata.id, params.run),
  });

  if (isLoading || isError) {
    return null;
  }

  const workflowRun = data?.run;
  const taskEvents = data?.taskEvents;
  const shape = data?.shape;
  const tasks = data?.tasks;

  if (!workflowRun || !taskEvents || !shape || !tasks) {
    return null;
  }

  // FIXME: This is a bug
  const inputData = JSON.stringify(workflowRun.input || {});
  const additionalMetadata = workflowRun.additionalMetadata;

  return (
    <div className="flex-grow h-full w-full">
      <div className="mx-auto max-w-7xl pt-2 px-4 sm:px-6 lg:px-8">
        <V2RunDetailHeader taskRunId={params.run} />
        <Separator className="my-4" />
        <div className="flex flex-row gap-x-4">
          <p className="font-semibold">Status</p>
          <Badge variant={statusToBadgeVariant(workflowRun.status)}>
            {workflowRun.status}
          </Badge>
        </div>
        <div className="w-full h-fit flex overflow-auto relative bg-slate-100 dark:bg-slate-900">
          <WorkflowRunVisualizer setSelectedTaskRunId={handleTaskRunExpand} />
          {shape && <ViewToggle shape={shape} />}
        </div>
        <div className="h-4" />
        <Tabs defaultValue="activity">
          <TabsList layout="underlined">
            <TabsTrigger variant="underlined" value="activity">
              Activity
            </TabsTrigger>
            <TabsTrigger variant="underlined" value="input">
              Input
            </TabsTrigger>
            <TabsTrigger variant="underlined" value="additional-metadata">
              Additional Metadata
            </TabsTrigger>
            {/* <TabsTrigger value="logs">App Logs</TabsTrigger> */}
          </TabsList>
          <TabsContent value="activity">
            <div className="h-4" />
            {
              <StepRunEvents
                workflowRunId={params.run}
                fallbackTaskDisplayName={workflowRun.displayName}
                onClick={handleTaskRunExpand}
              />
            }
          </TabsContent>
          <TabsContent value="input">
            {inputData && (
              <WorkflowRunInputDialog input={JSON.parse(inputData)} />
            )}
          </TabsContent>
          <TabsContent value="additional-metadata">
            <CodeHighlighter
              className="my-4"
              language="json"
              code={JSON.stringify(additionalMetadata, null, 2)}
            />
          </TabsContent>
        </Tabs>
      </div>
      {inputData && (
        <Sheet
          open={isSidebarOpen}
          onOpenChange={(open) => setIsSidebarOpen(open)}
        >
          <SheetContent className="w-fit min-w-[56rem] max-w-4xl sm:max-w-2xl z-[60]">
            {selectedTaskRunId && (
              <StepRunDetail
                taskRunId={selectedTaskRunId}
                defaultOpenTab={TabOption.Output}
              />
            )}
          </SheetContent>
        </Sheet>
      )}
    </div>
  );
}
