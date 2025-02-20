import { queries } from '@/lib/api';
import { TenantContextType } from '@/lib/outlet';
import { useQuery } from '@tanstack/react-query';
import { useOutletContext, useParams } from 'react-router-dom';
import invariant from 'tiny-invariant';

export const useWorkflowDetails = () => {
  const { tenant } = useOutletContext<TenantContextType>();
  const params = useParams();

  invariant(tenant);
  invariant(params.run);

  const { data, isLoading, isError } = useQuery({
    ...queries.v2WorkflowRuns.details(tenant.metadata.id, params.run),
  });

  const shape = data?.shape || [];
  const taskRuns = data?.tasks || [];
  const taskEvents = data?.taskEvents || [];
  const workflowRun = data?.run;

  return {
    shape,
    taskRuns,
    taskEvents,
    workflowRun,
    isLoading,
    isError,
  };
};
