import { useMemo } from 'react';
import ReactFlow, { Position, MarkerType, Node, Edge } from 'reactflow';
import 'reactflow/dist/style.css';
import StepRunNode from './step-run-node';
import { WorkflowRunShapeForWorkflowRunDetails } from '@/lib/api';
import dagre from 'dagre';
import { useTheme } from '@/components/theme-provider';

const connectionLineStyleDark = { stroke: '#fff' };
const connectionLineStyleLight = { stroke: '#000' };

function HatchetNode({ id }: { id: string }) {
  return <div>{id}</div>;
}

const nodeTypes = {
  stepNode: HatchetNode,
};

const WorkflowRunVisualizer = ({
  shape,
}: {
  shape: WorkflowRunShapeForWorkflowRunDetails;
  selectedStepRunId?: string;
  setSelectedStepRunId: (stepRunId: string) => void;
}) => {
  const { theme } = useTheme();

  const edges: Edge[] = shape
    .map((task) =>
      task.children.map((child) => ({
        id: task.parent,
        source: task.parent,
        target: child,
        // TODO: Change this
        animated: false,
        markerEnd: {
          type: MarkerType.ArrowClosed,
        },
      })),
    )
    .flat();

  const nodes: Node[] = shape.map((task) => ({
    id: task.parent,
    selectable: false,
    type: 'stepNode',
    position: { x: 0, y: 0 },
    data: task,
  }));

  const nodeWidth = 230;
  const nodeHeight = 70;
  const dagreGraph = new dagre.graphlib.Graph();
  dagreGraph.setDefaultEdgeLabel(() => ({}));

  const getLayoutedElements = (
    nodes: Node[],
    edges: Edge[],
    direction = 'LR',
  ) => {
    const isHorizontal = direction === 'LR';
    dagreGraph.setGraph({ rankdir: direction });

    nodes.forEach((node) => {
      dagreGraph.setNode(node.id, { width: nodeWidth, height: nodeHeight });
    });

    edges.forEach((edge) => {
      dagreGraph.setEdge(edge.source, edge.target);
    });

    dagre.layout(dagreGraph);

    nodes.forEach((node) => {
      const nodeWithPosition = dagreGraph.node(node.id);
      node.targetPosition = isHorizontal ? Position.Left : Position.Top;
      node.sourcePosition = isHorizontal ? Position.Right : Position.Bottom;

      // We are shifting the dagre node position (anchor=center center) to the top left
      // so it matches the React Flow node anchor point (top left).
      node.position = {
        x: nodeWithPosition.x - nodeWidth / 2,
        y: nodeWithPosition.y - nodeHeight / 2,
      };

      return node;
    });

    return { nodes, edges };
  };

  const dagrLayout = getLayoutedElements(nodes, edges);

  const dagrNodes = dagrLayout.nodes;
  const dagrEdges = dagrLayout.edges;

  const connectionLineStyle = useMemo(() => {
    return theme === 'dark'
      ? connectionLineStyleDark
      : connectionLineStyleLight;
  }, [theme]);

  console.log(edges, nodes);

  return (
    <div className="w-full h-[300px]">
      <ReactFlow
        nodes={dagrNodes}
        edges={dagrEdges}
        nodeTypes={nodeTypes}
        connectionLineStyle={connectionLineStyle}
        snapToGrid={true}
        fitView
        proOptions={{
          hideAttribution: true,
        }}
        className="border-1 border-gray-800 rounded-lg"
        maxZoom={1}
      />
    </div>
  );
};

export default WorkflowRunVisualizer;
