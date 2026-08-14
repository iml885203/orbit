<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useData } from 'vitepress'

type Star = {
  x: number
  y: number
  radius: number
  alpha: number
  speed: number
  drift: number
}

const root = ref<HTMLDivElement>()
const canvas = ref<HTMLCanvasElement>()
const { isDark } = useData()

let context: CanvasRenderingContext2D | null = null
let stars: Star[] = []
let width = 0
let height = 0
let dpr = 1
let frame = 0
let previousFrame = 0
let visible = true
let pageVisible = true
let reducedMotion = false
let pointerX = 0
let pointerY = 0
let resizeObserver: ResizeObserver | undefined
let intersectionObserver: IntersectionObserver | undefined
let motionQuery: MediaQueryList | undefined

function seededRandom(seed: number) {
  let state = seed
  return () => {
    state = Math.imul(48271, state) | 0
    return ((state >>> 0) % 2147483647) / 2147483647
  }
}

function rebuildStars() {
  const random = seededRandom(9473)
  const count = width < 360 ? 24 : width < 440 ? 36 : 72
  stars = Array.from({ length: count }, () => ({
    x: random(),
    y: random(),
    radius: 0.45 + random() * 1.35,
    alpha: 0.16 + random() * 0.68,
    speed: 0.8 + random() * 2.2,
    drift: random() * Math.PI * 2,
  }))
}

function syncCanvasSize() {
  if (!root.value || !canvas.value) return
  const bounds = root.value.getBoundingClientRect()
  const nextWidth = Math.max(1, Math.round(bounds.width))
  const nextHeight = Math.max(1, Math.round(bounds.height))
  const nextDpr = Math.min(window.devicePixelRatio || 1, 2)
  if (nextWidth === width && nextHeight === height && nextDpr === dpr) return

  width = nextWidth
  height = nextHeight
  dpr = nextDpr
  canvas.value.width = Math.round(width * dpr)
  canvas.value.height = Math.round(height * dpr)
  canvas.value.style.width = `${width}px`
  canvas.value.style.height = `${height}px`
  context = canvas.value.getContext('2d', { alpha: true })
  context?.setTransform(dpr, 0, 0, dpr, 0, 0)
  root.value.dataset.dpr = String(dpr)
  rebuildStars()
  draw(performance.now())
}

function drawOrbit(ctx: CanvasRenderingContext2D, centerX: number, centerY: number, radiusX: number, radiusY: number, rotation: number, alpha: number) {
  ctx.save()
  ctx.translate(centerX, centerY)
  ctx.rotate(rotation)
  ctx.beginPath()
  ctx.ellipse(0, 0, radiusX, radiusY, 0, 0, Math.PI * 2)
  ctx.strokeStyle = `rgba(121, 192, 255, ${alpha})`
  ctx.lineWidth = 1
  ctx.stroke()
  ctx.restore()
}

function orbitPoint(centerX: number, centerY: number, radiusX: number, radiusY: number, rotation: number, angle: number) {
  const x = Math.cos(angle) * radiusX
  const y = Math.sin(angle) * radiusY
  return {
    x: centerX + x * Math.cos(rotation) - y * Math.sin(rotation),
    y: centerY + x * Math.sin(rotation) + y * Math.cos(rotation),
  }
}

function drawBody(ctx: CanvasRenderingContext2D, x: number, y: number, radius: number, color: string, glow: number) {
  ctx.save()
  ctx.shadowColor = color
  ctx.shadowBlur = glow
  ctx.fillStyle = color
  ctx.beginPath()
  ctx.arc(x, y, radius, 0, Math.PI * 2)
  ctx.fill()
  ctx.restore()
}

function draw(time: number) {
  const ctx = context
  if (!ctx || !width || !height || !root.value) return

  const dark = isDark.value
  const seconds = reducedMotion ? 7.5 : time / 1000
  const compact = width < 400
  const centerX = width * 0.51 + pointerX * (compact ? 0 : 8)
  const centerY = height * 0.49 + pointerY * (compact ? 0 : 6)
  const scale = Math.min(width / 500, height / 430)
  const pulse = reducedMotion ? 0.5 : (Math.sin(seconds * 1.45) + 1) / 2

  ctx.clearRect(0, 0, width, height)

  const field = ctx.createRadialGradient(centerX, centerY, 12, centerX, centerY, Math.max(width, height) * 0.66)
  field.addColorStop(0, dark ? 'rgba(48, 70, 160, 0.28)' : 'rgba(84, 174, 255, 0.22)')
  field.addColorStop(0.42, dark ? 'rgba(73, 43, 128, 0.13)' : 'rgba(137, 87, 229, 0.10)')
  field.addColorStop(1, 'rgba(13, 17, 23, 0)')
  ctx.fillStyle = field
  ctx.fillRect(0, 0, width, height)

  for (const star of stars) {
    const travel = reducedMotion ? 0 : seconds * star.speed
    const x = (star.x * width + travel + pointerX * 5 + width) % width
    const y = star.y * height + Math.sin(seconds * 0.24 + star.drift) * (compact ? 1.5 : 3) + pointerY * 4
    ctx.globalAlpha = star.alpha * (dark ? 1 : 0.72)
    ctx.fillStyle = star.radius > 1.2 ? '#b8dcff' : '#79c0ff'
    ctx.beginPath()
    ctx.arc(x, y, star.radius * scale, 0, Math.PI * 2)
    ctx.fill()
  }
  ctx.globalAlpha = 1

  const orbitCount = compact ? 2 : 3
  const orbitSpecs = [
    { rx: 132, ry: 50, rotation: -0.34, speed: 0.26, phase: 0.7, color: '#79c0ff' },
    { rx: 174, ry: 72, rotation: 0.34, speed: -0.18, phase: 2.4, color: '#a371f7' },
    { rx: 205, ry: 98, rotation: -0.08, speed: 0.12, phase: 4.1, color: '#39c5cf' },
  ]

  for (let index = 0; index < orbitCount; index += 1) {
    const orbit = orbitSpecs[index]
    const radiusX = orbit.rx * scale
    const radiusY = orbit.ry * scale
    drawOrbit(ctx, centerX, centerY, radiusX, radiusY, orbit.rotation, dark ? 0.27 : 0.34)
    const angle = seconds * orbit.speed + orbit.phase
    const body = orbitPoint(centerX, centerY, radiusX, radiusY, orbit.rotation, angle)
    drawBody(ctx, body.x, body.y, (index === 0 ? 3.8 : 3) * scale, orbit.color, compact ? 7 : 11)
  }

  if (!reducedMotion) {
    const signalCycle = seconds % 5.2
    if (signalCycle < 2.1) {
      const orbit = orbitSpecs[0]
      const radiusX = orbit.rx * scale
      const radiusY = orbit.ry * scale
      const head = orbit.phase + signalCycle * 1.9
      for (let trail = 8; trail >= 0; trail -= 1) {
        const point = orbitPoint(centerX, centerY, radiusX, radiusY, orbit.rotation, head - trail * 0.045)
        ctx.globalAlpha = (1 - trail / 9) * 0.8
        drawBody(ctx, point.x, point.y, Math.max(0.8, 2.7 - trail * 0.22) * scale, '#58a6ff', trail < 2 ? 9 : 0)
      }
      ctx.globalAlpha = 1
    }
  }

  const haloRadius = (82 + pulse * 9) * scale
  const halo = ctx.createRadialGradient(centerX, centerY, 0, centerX, centerY, haloRadius)
  halo.addColorStop(0, dark ? 'rgba(88, 166, 255, 0.34)' : 'rgba(9, 105, 218, 0.26)')
  halo.addColorStop(0.48, dark ? 'rgba(123, 83, 214, 0.16)' : 'rgba(137, 87, 229, 0.12)')
  halo.addColorStop(1, 'rgba(88, 166, 255, 0)')
  ctx.fillStyle = halo
  ctx.beginPath()
  ctx.arc(centerX, centerY, haloRadius, 0, Math.PI * 2)
  ctx.fill()

  const coreRadius = 43 * scale
  const core = ctx.createRadialGradient(centerX - coreRadius * 0.32, centerY - coreRadius * 0.36, coreRadius * 0.08, centerX, centerY, coreRadius)
  core.addColorStop(0, dark ? '#b8dcff' : '#d9efff')
  core.addColorStop(0.2, '#58a6ff')
  core.addColorStop(0.66, '#5e45ad')
  core.addColorStop(1, dark ? '#151b3c' : '#263a78')
  ctx.save()
  ctx.shadowColor = '#58a6ff'
  ctx.shadowBlur = (18 + pulse * 10) * scale
  ctx.fillStyle = core
  ctx.beginPath()
  ctx.arc(centerX, centerY, coreRadius, 0, Math.PI * 2)
  ctx.fill()
  ctx.restore()

  ctx.save()
  ctx.strokeStyle = 'rgba(255, 255, 255, 0.86)'
  ctx.lineWidth = Math.max(2, 3 * scale)
  ctx.beginPath()
  ctx.arc(centerX, centerY, coreRadius * 0.45, -Math.PI * 0.72, Math.PI * 0.72)
  ctx.stroke()
  ctx.restore()

  root.value.dataset.theme = dark ? 'dark' : 'light'
}

function animate(time: number) {
  frame = 0
  if (!visible || !pageVisible || reducedMotion) return
  if (time - previousFrame >= 1000 / 30) {
    draw(time)
    previousFrame = time
  }
  frame = requestAnimationFrame(animate)
}

function restartAnimation() {
  cancelAnimationFrame(frame)
  frame = 0
  if (visible && pageVisible && !reducedMotion) {
    root.value?.setAttribute('data-motion', 'running')
    frame = requestAnimationFrame(animate)
  } else {
    root.value?.setAttribute('data-motion', reducedMotion ? 'reduced' : 'paused')
    draw(performance.now())
  }
}

function onVisibilityChange() {
  pageVisible = !document.hidden
  restartAnimation()
}

function onMotionChange(event: MediaQueryListEvent | MediaQueryList) {
  reducedMotion = event.matches
  restartAnimation()
}

function onPointerMove(event: PointerEvent) {
  if (!root.value || width < 640) return
  const bounds = root.value.getBoundingClientRect()
  pointerX = (event.clientX - bounds.left) / bounds.width - 0.5
  pointerY = (event.clientY - bounds.top) / bounds.height - 0.5
}

function onPointerLeave() {
  pointerX = 0
  pointerY = 0
}

onMounted(async () => {
  await nextTick()
  if (!root.value || !canvas.value) return
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  reducedMotion = motionQuery.matches
  motionQuery.addEventListener('change', onMotionChange)
  resizeObserver = new ResizeObserver(syncCanvasSize)
  resizeObserver.observe(root.value)
  intersectionObserver = new IntersectionObserver(([entry]) => {
    visible = entry.isIntersecting
    restartAnimation()
  }, { threshold: 0.05 })
  intersectionObserver.observe(root.value)
  root.value.addEventListener('pointermove', onPointerMove)
  root.value.addEventListener('pointerleave', onPointerLeave)
  document.addEventListener('visibilitychange', onVisibilityChange)
  syncCanvasSize()
  restartAnimation()
})

watch(isDark, async () => {
  await nextTick()
  draw(performance.now())
})

onUnmounted(() => {
  cancelAnimationFrame(frame)
  resizeObserver?.disconnect()
  intersectionObserver?.disconnect()
  motionQuery?.removeEventListener('change', onMotionChange)
  root.value?.removeEventListener('pointermove', onPointerMove)
  root.value?.removeEventListener('pointerleave', onPointerLeave)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <div ref="root" class="hero-orbit" data-motion="paused" data-theme="dark">
    <canvas ref="canvas" aria-hidden="true" />
    <span class="visually-hidden">An animated orbital field centered on Orbit.</span>
  </div>
</template>
