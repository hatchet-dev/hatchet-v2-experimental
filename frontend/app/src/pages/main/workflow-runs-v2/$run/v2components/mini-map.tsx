import { useMemo } from 'react';
import { V2TaskSummary } from '@/lib/api';
import { TabOption } from './step-run-detail/step-run-detail';
import StepRunNode from './step-run-node';
import { useWorkflowDetails } from '../../hooks';

interface JobMiniMapProps {
  onClick: (stepRunId?: string, defaultOpenTab?: TabOption) => void;
}

type NodeRelationship = {
  node: string;
  children: string[];
  parents: string[];
};

export const JobMiniMap = ({ onClick }: JobMiniMapProps) => {
  const { shape, taskRuns: tasks, isLoading, isError } = useWorkflowDetails();

  const taskRunRelationships: NodeRelationship[] = useMemo(
    () =>
      tasks.map((item) => {
        const node = item.taskExternalId;
        const children = shape.find((i) => i.parent === node)?.children || [];
        const parents = shape
          .filter((i) => i.children.includes(node))
          .map((i) => i.parent);

        return {
          node,
          children,
          parents,
        };
      }) || [],
    [shape, tasks],
  );

  const columns = useMemo(() => {
    const columns: V2TaskSummary[][] = [];
    const processed = new Set<string>();

    const addToColumn = (taskRun: V2TaskSummary, columnIndex: number) => {
      if (!columns[columnIndex]) {
        columns[columnIndex] = [];
      }

      columns[columnIndex].push(taskRun);
      processed.add(taskRun.taskExternalId);
    };

    const processTaskRun = (taskRun: V2TaskSummary) => {
      if (processed.has(taskRun.taskExternalId)) {
        return;
      }

      const relationship = taskRunRelationships.find(
        (r) => r.node == taskRun.taskExternalId,
      );

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

        if (maxParentColumn > -1) {
          addToColumn(taskRun, maxParentColumn + 1);
        }
      }
    };

    while (processed.size < tasks.length) {
      tasks.forEach(processTaskRun);
    }

    return columns;
  }, [taskRunRelationships, tasks]);

  if (isLoading || isError) {
    return null;
  }

  return (
    <div className="flex flex-1 flex-row p-4 rounded-sm relative gap-1">
      {columns.map((column, colIndex) => (
        <div
          key={colIndex}
          className="flex flex-col justify-start h-full min-w-fit grow"
        >
          {column.map((taskRun) => {
            return (
              <StepRunNode
                key={taskRun.taskExternalId}
                data={{
                  task: taskRun,
                  graphVariant: 'none',
                  onClick: () => onClick(taskRun.metadata.id),
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
