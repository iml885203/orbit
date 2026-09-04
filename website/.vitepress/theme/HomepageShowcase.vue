<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useData, useRoute } from 'vitepress'

type ShowcaseContent = {
  label: string
  title: string
  description: string
  requestLabel: string
  request: string
  agentLabel: string
  agentResponse: string
  connected: string
  services: string
  graph: string
  table: string
  live: string
  environment: string
  starting: string
  healthy: string
  scenes: string[]
  relationships: string[]
  link: string
  linkText: string
}

const nodes = [
  { id: 'web', name: 'web', kind: 'frontend', detail: 'process', port: ':5173', readyAt: 7 },
  { id: 'api', name: 'api', kind: 'backend', detail: 'process', port: ':8080', readyAt: 6 },
  { id: 'worker', name: 'worker', kind: 'backend', detail: 'process', port: '', readyAt: 6 },
  { id: 'postgres', name: 'postgresql', kind: 'infra', detail: 'container', port: ':5432', readyAt: 5 },
  { id: 'redis', name: 'redis', kind: 'infra', detail: 'container', port: ':6379', readyAt: 5 },
  { id: 'kafka', name: 'kafka', kind: 'infra', detail: 'container', port: ':9092', readyAt: 5 },
]
const edges = [
  { id: 'web-api', path: 'M140 108 C140 120 140 120 140 136', readyAt: 7 },
  { id: 'api-postgres', path: 'M140 228 C140 248 140 248 140 272', readyAt: 6 },
  { id: 'api-redis', path: 'M140 228 C140 252 420 248 420 272', readyAt: 6 },
  { id: 'worker-postgres', path: 'M480 228 C480 252 140 248 140 272', readyAt: 6 },
  { id: 'worker-kafka', path: 'M480 228 C480 288 480 350 480 400', readyAt: 6 },
]
const finalScene = 7

const { frontmatter } = useData()
const route = useRoute()
const root = ref<HTMLElement>()
const scene = ref(finalScene)
const visible = ref(false)
const pageVisible = ref(true)
const reducedMotion = ref(false)
const typedCharacters = ref(1)

const showcase = computed(() => {
  if (route.path !== '/' && route.path !== '/zh-TW/') return undefined
  return frontmatter.value.showcase as ShowcaseContent | undefined
})
const motion = computed(() => {
  if (reducedMotion.value) return 'reduced'
  if (!visible.value || !pageVisible.value || !showcase.value) return 'paused'
  return scene.value === finalScene ? 'complete' : 'running'
})
const status = computed(() => showcase.value?.scenes[scene.value] ?? '')
const typedRequest = computed(() => {
  const request = showcase.value?.request ?? ''
  if (scene.value > 0 || reducedMotion.value) return request
  return Array.from(request).slice(0, typedCharacters.value).join('')
})

let sceneTimer: ReturnType<typeof setInterval> | undefined
let sceneTicks = 0
let intersectionObserver: IntersectionObserver | undefined
let motionQuery: MediaQueryList | undefined

function stopSceneTimer() {
  if (!sceneTimer) return
  clearInterval(sceneTimer)
  sceneTimer = undefined
}

function syncSceneTimer() {
  stopSceneTimer()
  if (motion.value !== 'running') return
  sceneTimer = setInterval(() => {
    if (scene.value === 0) {
      const requestLength = Array.from(showcase.value?.request ?? '').length
      if (typedCharacters.value < requestLength) {
        typedCharacters.value = Math.min(requestLength, typedCharacters.value + 2)
        return
      }
      sceneTicks += 1
      if (sceneTicks < 5) return
    } else {
      sceneTicks += 1
      if (sceneTicks < 18) return
    }
    sceneTicks = 0
    if (scene.value < finalScene) scene.value += 1
    if (scene.value === finalScene) stopSceneTimer()
  }, 60)
}

function resetScene() {
  stopSceneTimer()
  sceneTicks = 0
  scene.value = reducedMotion.value ? finalScene : 0
  typedCharacters.value = reducedMotion.value ? Array.from(showcase.value?.request ?? '').length : 1
}

function onVisibilityChange() {
  pageVisible.value = !document.hidden
}

function onMotionChange(event: MediaQueryListEvent | MediaQueryList) {
  reducedMotion.value = event.matches
  resetScene()
}

watch(motion, syncSceneTimer)
watch(() => route.path, (current, previous) => {
  const homepage = (path: string) => path === '/' || path === '/zh-TW/'
  if (current !== previous && (homepage(current) || homepage(previous))) resetScene()
})
watch(root, (current, previous) => {
  visible.value = false
  if (previous) intersectionObserver?.unobserve(previous)
  if (current) intersectionObserver?.observe(current)
})

onMounted(() => {
  pageVisible.value = !document.hidden
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  reducedMotion.value = motionQuery.matches
  resetScene()
  motionQuery.addEventListener('change', onMotionChange)
  intersectionObserver = new IntersectionObserver(([entry]) => {
    if (entry.target === root.value) visible.value = entry.isIntersecting
  }, { threshold: 0.05 })
  if (root.value) intersectionObserver.observe(root.value)
  document.addEventListener('visibilitychange', onVisibilityChange)
  syncSceneTimer()
})

onUnmounted(() => {
  stopSceneTimer()
  intersectionObserver?.disconnect()
  motionQuery?.removeEventListener('change', onMotionChange)
  document.removeEventListener('visibilitychange', onVisibilityChange)
})
</script>

<template>
  <section
    v-if="showcase"
    ref="root"
    class="homepage-showcase"
    :class="`scene-${scene}`"
    :data-motion="motion"
    :data-scene="scene"
    :data-typed-request="typedRequest"
    aria-labelledby="homepage-showcase-title"
  >
    <div class="homepage-showcase-heading">
      <p class="homepage-showcase-label">{{ showcase.label }}</p>
      <h2 id="homepage-showcase-title">{{ showcase.title }}</h2>
      <p>{{ showcase.description }}</p>
    </div>

    <div class="showcase-demo">
      <div class="showcase-conversation">
        <div class="showcase-composer" aria-hidden="true">
          <span>{{ typedRequest }}</span><i class="showcase-caret" />
          <i class="showcase-send-indicator">↑</i>
        </div>
        <div class="showcase-message showcase-message-user">
          <span>{{ showcase.requestLabel }}</span>
          <p>{{ showcase.request }}</p>
        </div>
        <div class="showcase-message showcase-message-agent">
          <span>{{ showcase.agentLabel }}</span>
          <p>{{ showcase.agentResponse }}</p>
        </div>
      </div>

      <div class="showcase-dashboard">
        <div class="showcase-app-bar" aria-hidden="true">
          <div class="showcase-brand">
            <svg viewBox="0 0 96 96">
              <path d="M75.6 36.3A30 30 0 0 1 38.2 76.4" />
              <path d="M20.4 59.7A30 30 0 0 1 57.8 19.6" />
              <circle class="showcase-logo-orbit" cx="27.5" cy="70" r="6.5" />
              <circle class="showcase-logo-orbit" cx="68.5" cy="26" r="6.5" />
              <circle class="showcase-logo-core" cx="48" cy="48" r="6" />
            </svg>
            <strong>Orbit</strong>
          </div>
          <span class="showcase-instance">local</span>
          <span class="showcase-connected"><i />{{ showcase.connected }}</span>
          <span class="showcase-nav-active">{{ showcase.services }}</span>
          <span class="showcase-environment">{{ showcase.environment }}</span>
        </div>
        <div class="showcase-services-bar" aria-hidden="true">
          <div><strong>{{ showcase.services }}</strong><span>6 resources</span></div>
          <div>
            <span v-if="scene === finalScene" class="showcase-dashboard-health">{{ showcase.healthy }}</span>
            <div class="showcase-view-switch">
              <span class="is-selected">{{ showcase.graph }}</span><span>{{ showcase.table }}</span>
            </div>
          </div>
        </div>
        <div class="showcase-graph">
          <span class="showcase-live" aria-hidden="true"><i />{{ showcase.live }}</span>
          <svg class="showcase-edges" viewBox="0 0 680 512" preserveAspectRatio="xMidYMid meet" aria-hidden="true">
            <defs>
              <marker id="showcase-edge-arrow" viewBox="0 0 8 8" refX="7" refY="4" markerWidth="5" markerHeight="5" orient="auto">
                <path d="M0 0 L8 4 L0 8 Z" />
              </marker>
            </defs>
            <g
              v-for="edge in edges"
              :key="edge.id"
              class="showcase-edge"
              :class="[`edge-${edge.id}`, { 'is-active': scene >= edge.readyAt }]"
            >
              <path :d="edge.path" marker-end="url(#showcase-edge-arrow)" />
              <circle
                v-if="scene >= edge.readyAt"
                class="showcase-flow-dot"
                r="3"
                :style="{ offsetPath: `path('${edge.path}')` }"
              />
            </g>
          </svg>
          <article
            v-for="node in nodes"
            :key="node.id"
            class="showcase-node"
            :class="[`node-${node.id}`, `kind-${node.kind}`, { 'is-healthy': scene >= node.readyAt }]"
          >
            <div class="showcase-node-row">
              <span class="showcase-node-status"><i aria-hidden="true" />{{ scene >= node.readyAt ? showcase.healthy : showcase.starting }}</span>
              <strong>{{ node.name }}</strong>
              <span class="showcase-node-kind">{{ node.detail }}</span>
            </div>
            <div class="showcase-node-row showcase-node-meta">
              <span aria-hidden="true">↻</span><span aria-hidden="true">■</span><span aria-hidden="true">▤</span>
              <span class="showcase-node-port">{{ node.port }}</span>
            </div>
          </article>
        </div>
        <ul class="showcase-relationships">
          <li v-for="relationship in showcase.relationships" :key="relationship">
            <i v-if="scene === finalScene" class="showcase-relationship-marker" aria-hidden="true" />{{ relationship }}
          </li>
        </ul>
      </div>
    </div>

    <div class="showcase-outcome">
      <p role="status" aria-live="polite">{{ status }}</p>
      <a :href="showcase.link">{{ showcase.linkText }} <span aria-hidden="true">→</span></a>
    </div>
  </section>
</template>
