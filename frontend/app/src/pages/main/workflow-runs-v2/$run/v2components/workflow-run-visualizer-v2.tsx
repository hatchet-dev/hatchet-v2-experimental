import { useMemo } from 'react';
import ReactFlow, {
  Position,
  MarkerType,
  Node,
  Edge,
  SmoothStepEdge,
  Handle,
} from 'reactflow';
import 'reactflow/dist/style.css';
import dagre from 'dagre';
import { useTheme } from '@/components/theme-provider';
import { useQuery } from '@tanstack/react-query';
import { queries } from '@/lib/api';
import { useTenant } from '@/lib/atoms';
import invariant from 'tiny-invariant';
import { useParams } from 'react-router-dom';

const connectionLineStyleDark = { stroke: '#fff' };
const connectionLineStyleLight = { stroke: '#000' };

interface WorkflowRunShapeItemForWorkflowRunDetails {
  parent: string;
  children: string[];
}

type NodeData = {
  task: WorkflowRunShapeItemForWorkflowRunDetails;
  label: string;
};

type WorkflowRunShapeForWorkflowRunDetails =
  WorkflowRunShapeItemForWorkflowRunDetails[];

const HatchetNode = ({ data }: { data: NodeData }) => {
  return (
    <div className="text-updater-node">
      <Handle type="target" position={Position.Top} />
      <div>{data.label}</div>
      <Handle type="source" position={Position.Bottom} />
    </div>
  );
};

const nodeTypes = {
  stepNode: HatchetNode,
};

const edgeTypes = {
  smoothstep: SmoothStepEdge,
};

const WorkflowRunVisualizer = () => {
  const { theme } = useTheme();
  const { tenant } = useTenant();
  const params = useParams();

  invariant(tenant);
  invariant(params.run);

  const { data, isLoading, isError } = useQuery({
    ...queries.v2WorkflowRuns.details(tenant.metadata.id, params.run),
  });

  if (isLoading || isError) {
    return null;
  }

  const shape = data?.shape;

  if (!shape) {
    return null;
  }

  const edges: Edge[] = useMemo(
    () =>
      shape.flatMap((task) =>
        task.children.map((childId) => ({
          id: `${task.parent}-${childId}`,
          source: task.parent,
          target: childId,
          animated: true,
          style:
            theme === 'dark'
              ? connectionLineStyleDark
              : connectionLineStyleLight,
          markerEnd: {
            type: MarkerType.ArrowClosed,
          },
          type: 'smoothstep',
        })),
      ),
    [shape, theme],
  );

  const nodes: Node[] = useMemo(
    () =>
      shape.map((task) => ({
        id: task.parent,
        type: 'stepNode',
        position: { x: 0, y: 0 },
        data: {
          task,
          label: task.parent,
        },
        selectable: true,
      })),
    [shape],
  );

  const nodeWidth = 230;
  const nodeHeight = 70;

  const getLayoutedElements = (
    nodes: Node[],
    edges: Edge[],
    direction = 'LR',
  ) => {
    const dagreGraph = new dagre.graphlib.Graph();
    dagreGraph.setDefaultEdgeLabel(() => ({}));

    const isHorizontal = direction === 'LR';
    dagreGraph.setGraph({ rankdir: direction });

    nodes.forEach((node) => {
      dagreGraph.setNode(node.id, { width: nodeWidth, height: nodeHeight });
    });

    edges.forEach((edge) => {
      dagreGraph.setEdge(edge.source, edge.target);
    });

    dagre.layout(dagreGraph);

    const layoutedNodes = nodes.map((node) => {
      const nodeWithPosition = dagreGraph.node(node.id);
      node.targetPosition = isHorizontal ? Position.Left : Position.Top;
      node.sourcePosition = isHorizontal ? Position.Right : Position.Bottom;

      node.position = {
        x: nodeWithPosition.x - nodeWidth / 2,
        y: nodeWithPosition.y - nodeHeight / 2,
      };

      return { ...node };
    });

    return { nodes: layoutedNodes, edges };
  };

  const { nodes: layoutedNodes, edges: layoutedEdges } = useMemo(
    () => getLayoutedElements(nodes, edges),
    [nodes, edges],
  );

  return (
    <div className="w-full h-[300px]">
      <ReactFlow
        nodes={layoutedNodes}
        edges={layoutedEdges}
        nodeTypes={nodeTypes}
        edgeTypes={edgeTypes}
        fitView
        proOptions={{
          hideAttribution: true,
        }}
        onNodeClick={(_, node) => console.log(node)}
        className="border rounded-lg"
        maxZoom={1}
      />
    </div>
  );
};

export default WorkflowRunVisualizer;
