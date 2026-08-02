import { Position, getBezierPath } from '@xyflow/svelte'
import { stableHash } from '../../lib/hash'

type DependencyPathInput = {
  id: string
  async: boolean
  sourceX: number
  sourceY: number
  sourcePosition?: Position
  targetX: number
  targetY: number
  targetPosition?: Position
  routeLane?: number
}

type DependencyPath = {
  path: string
  labelX: number
  labelY: number
}

const SYNC_CURVATURE = 0.25
const ASYNC_CURVATURE = 0.6
const ASYNC_BOW = 44
const NODE_HALF_WIDTH = 120
const ROUTE_GUTTER = 12
const ROUTE_LANE_GAP = 28
const ROUTE_STUB = 32
const ROUTE_CORNER = 12

export function buildDependencyPath(input: DependencyPathInput): DependencyPath {
  if (!input.async) {
    if (input.routeLane) return buildRoutedPath(input)
    const [path, labelX, labelY] = getBezierPath({
      sourceX: input.sourceX,
      sourceY: input.sourceY,
      sourcePosition: input.sourcePosition,
      targetX: input.targetX,
      targetY: input.targetY,
      targetPosition: input.targetPosition,
      curvature: SYNC_CURVATURE,
    })
    return { path, labelX, labelY }
  }

  return buildAsyncPath(input)
}

function buildAsyncPath(input: DependencyPathInput): DependencyPath {
  return buildBowedPath(input, asyncBow(input), ASYNC_CURVATURE)
}

function buildRoutedPath(input: DependencyPathInput): DependencyPath {
  const lane = input.routeLane ?? 0
  const side = Math.sign(lane)
  const laneX = input.sourceX + side * (
    NODE_HALF_WIDTH + ROUTE_GUTTER + (Math.abs(lane) - 1) * ROUTE_LANE_GAP
  )
  const sourceRouteY = input.sourceY + ROUTE_STUB
  const targetRouteY = input.targetY - ROUTE_STUB
  const sourceCornerX = laneX - side * ROUTE_CORNER
  const targetCornerX = input.targetX + side * ROUTE_CORNER

  return {
    path: [
      `M${input.sourceX},${input.sourceY}`,
      `L${input.sourceX},${sourceRouteY - ROUTE_CORNER}`,
      `Q${input.sourceX},${sourceRouteY} ${input.sourceX + side * ROUTE_CORNER},${sourceRouteY}`,
      `L${sourceCornerX},${sourceRouteY}`,
      `Q${laneX},${sourceRouteY} ${laneX},${sourceRouteY + ROUTE_CORNER}`,
      `L${laneX},${targetRouteY - ROUTE_CORNER}`,
      `Q${laneX},${targetRouteY} ${sourceCornerX},${targetRouteY}`,
      `L${targetCornerX},${targetRouteY}`,
      `Q${input.targetX},${targetRouteY} ${input.targetX},${targetRouteY + ROUTE_CORNER}`,
      `L${input.targetX},${input.targetY}`,
    ].join(' '),
    labelX: laneX,
    labelY: (sourceRouteY + targetRouteY) / 2,
  }
}

function buildBowedPath(
  input: DependencyPathInput,
  bow: { x: number; y: number },
  curvature: number,
): DependencyPath {
  const sourcePosition = input.sourcePosition ?? Position.Bottom
  const targetPosition = input.targetPosition ?? Position.Top
  const [sourceControlX, sourceControlY] = controlPoint({
    pos: sourcePosition,
    x1: input.sourceX,
    y1: input.sourceY,
    x2: input.targetX,
    y2: input.targetY,
    curvature,
  })
  const [targetControlX, targetControlY] = controlPoint({
    pos: targetPosition,
    x1: input.targetX,
    y1: input.targetY,
    x2: input.sourceX,
    y2: input.sourceY,
    curvature,
  })
  const sourceControl = { x: sourceControlX + bow.x, y: sourceControlY + bow.y }
  const targetControl = { x: targetControlX + bow.x, y: targetControlY + bow.y }
  const labelX = (
    input.sourceX * 0.125 +
    sourceControl.x * 0.375 +
    targetControl.x * 0.375 +
    input.targetX * 0.125
  )
  const labelY = (
    input.sourceY * 0.125 +
    sourceControl.y * 0.375 +
    targetControl.y * 0.375 +
    input.targetY * 0.125
  )

  return {
    path: `M${input.sourceX},${input.sourceY} C${sourceControl.x},${sourceControl.y} ${targetControl.x},${targetControl.y} ${input.targetX},${input.targetY}`,
    labelX,
    labelY,
  }
}

type ControlInput = {
  pos: Position
  x1: number
  y1: number
  x2: number
  y2: number
  curvature: number
}

function controlPoint({ pos, x1, y1, x2, y2, curvature }: ControlInput): [number, number] {
  switch (pos) {
    case Position.Left:
      return [x1 - controlOffset(x1 - x2, curvature), y1]
    case Position.Right:
      return [x1 + controlOffset(x2 - x1, curvature), y1]
    case Position.Top:
      return [x1, y1 - controlOffset(y1 - y2, curvature)]
    case Position.Bottom:
      return [x1, y1 + controlOffset(y2 - y1, curvature)]
  }
}

// Mirrors xyflow/system's calculateControlOffset + getControlWithCurvature.
// Re-implemented because xyflow only exports getBezierPath (a finished SVG
// path string); async edges need raw control points to apply a bow offset
// before composing the path. If xyflow ever exports these helpers, drop
// controlOffset/controlPoint and import them instead.
// Source: @xyflow/system/dist/esm/index.mjs ~L900.
function controlOffset(distance: number, curvature: number): number {
  if (distance >= 0) return 0.5 * distance
  return curvature * 25 * Math.sqrt(-distance)
}

function asyncBow(input: DependencyPathInput): { x: number; y: number } {
  const dx = input.targetX - input.sourceX
  const dy = input.targetY - input.sourceY
  const length = Math.hypot(dx, dy) || 1
  const sign = stableHash(input.id) % 2 === 0 ? 1 : -1

  return {
    x: (-dy / length) * ASYNC_BOW * sign,
    y: (dx / length) * ASYNC_BOW * sign,
  }
}
