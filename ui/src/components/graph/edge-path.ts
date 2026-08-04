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
  sourceOffset?: number
  targetOffset?: number
}

type DependencyPath = {
  path: string
  labelX: number
  labelY: number
}

const SYNC_CURVATURE = 0.25
const ASYNC_CURVATURE = 0.6
const ASYNC_BOW = 44

export function buildDependencyPath(input: DependencyPathInput): DependencyPath {
  const routedInput = {
    ...input,
    sourceX: input.sourceX + (input.sourceOffset ?? 0),
    targetX: input.targetX + (input.targetOffset ?? 0),
  }
  if (!input.async) {
    const [path, labelX, labelY] = getBezierPath({
      sourceX: routedInput.sourceX,
      sourceY: routedInput.sourceY,
      sourcePosition: routedInput.sourcePosition,
      targetX: routedInput.targetX,
      targetY: routedInput.targetY,
      targetPosition: routedInput.targetPosition,
      curvature: SYNC_CURVATURE,
    })
    return { path, labelX, labelY }
  }

  return buildAsyncPath(routedInput)
}

function buildAsyncPath(input: DependencyPathInput): DependencyPath {
  const sourcePosition = input.sourcePosition ?? Position.Bottom
  const targetPosition = input.targetPosition ?? Position.Top
  const [sourceControlX, sourceControlY] = controlPoint({
    pos: sourcePosition,
    x1: input.sourceX,
    y1: input.sourceY,
    x2: input.targetX,
    y2: input.targetY,
    curvature: ASYNC_CURVATURE,
  })
  const [targetControlX, targetControlY] = controlPoint({
    pos: targetPosition,
    x1: input.targetX,
    y1: input.targetY,
    x2: input.sourceX,
    y2: input.sourceY,
    curvature: ASYNC_CURVATURE,
  })
  const bow = asyncBow(input)
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
