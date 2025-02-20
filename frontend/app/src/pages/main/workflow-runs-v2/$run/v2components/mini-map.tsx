import { useMemo } from 'react';
import { queries, V2TaskSummary } from '@/lib/api';
import { TabOption } from './step-run-detail/step-run-detail';
import StepRunNode from './step-run-node';
import invariant from 'tiny-invariant';
import { useQuery } from '@tanstack/react-query';
import { useOutletContext, useParams } from 'react-router-dom';
import { TenantContextType } from '@/lib/outlet';

interface JobMiniMapProps {
  onClick: (stepRunId?: string, defaultOpenTab?: TabOption) => void;
}

type NodeRelationship = {
  node: string;
  children: string[];
  parents: string[];
};

export const JobMiniMap = ({ onClick }: JobMiniMapProps) => {
  const { tenant } = useOutletContext<TenantContextType>();
  const params = useParams();

  invariant(tenant);
  invariant(params.run);

  const { data, isLoading, isError } = useQuery({
    ...queries.v2WorkflowRuns.details(tenant.metadata.id, params.run),
  });

  if (isLoading || isError) {
    return null;
  }

  const shape = data?.shape || [];
  const tasks = data?.tasks || [];

  const taskRunRelationships: NodeRelationship[] =
    shape?.map((item) => {
      const node = item.parent;
      const children = item.children;
      const parents =
        shape.filter((i) => i.children.includes(node)).map((i) => i.parent) ||
        [];

      return {
        node,
        children,
        parents,
      };
    }) || [];

  const columns = useMemo(() => {
    const columns: V2TaskSummary[][] = [];
    const processed = new Set<string>();

    const addToColumn = (taskRun: V2TaskSummary, columnIndex: number) => {
      if (!taskRun.taskExternalId) {
        return;
      }

      if (!columns[columnIndex]) {
        columns[columnIndex] = [];
      }
      columns[columnIndex].push(taskRun);
      processed.add(taskRun.taskExternalId);
    };

    const processTaskRun = (taskRun: V2TaskSummary) => {
      const relationship = taskRunRelationships.find(
        (r) => r.node == taskRun.taskExternalId,
      );

      if (!taskRun.taskExternalId || processed.has(taskRun.taskExternalId)) {
        return;
      }

      if (!relationship || relationship.parents.length === 0) {
        addToColumn(taskRun, 0);
      } else {
        const maxParentColumn = Math.max(
          ...relationship.parents.map((parentId) => {
            const parentStep = tasks.find((r) => r.taskExternalId === parentId);

            return parentStep
              ? columns.findIndex((col) => col.includes(parentStep))
              : -1;
          }),
        );

        addToColumn(taskRun, maxParentColumn + 1);
      }
    };

    while (processed.size < tasks.length) {
      tasks.forEach(processTaskRun);
    }

    return columns;
  }, [taskRunRelationships]);

  const normalizedStepRunsByStepId = useMemo(() => {
    return taskRunRelationships.reduce(
      (acc, step) => {
        const taskRun = tasks?.find((tr) => tr.taskExternalId === step.node);
        if (taskRun) {
          acc[step.node] = taskRun;
        }
        return acc;
      },
      {} as Record<string, V2TaskSummary>,
    );
  }, [tasks, taskRunRelationships]);

  return (
    <div className="flex flex-row p-4 rounded-sm relative gap-1">
      {columns.map((column, colIndex) => (
        <div
          key={colIndex}
          className="flex flex-col justify-start h-full min-w-fit grow"
        >
          {column.map((taskRun) => {
            if (!taskRun.taskExternalId) {
              return null;
            }

            const task = normalizedStepRunsByStepId[taskRun.taskExternalId];

            if (!task) {
              return null;
            }

            return (
              <StepRunNode
                key={task.taskExternalId}
                data={{
                  task,
                  graphVariant: 'none',
                  onClick: () => onClick(task?.metadata.id),
                  childWorkflowsCount: 0,
                }}
              />
            );
          })}
        </div>
      ))}
    </div>
  );
};
