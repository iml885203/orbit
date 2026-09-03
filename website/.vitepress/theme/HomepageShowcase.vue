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
  dashboardLabel: string
  environment: string
  starting: string
  healthy: string
  scenes: string[]
  relationships: string[]
  link: string
  linkText: string
}

const nodes = [
  { id: 'web', name: 'web', kind: 'frontend', readyAt: 5 },
  { id: 'api', name: 'api', kind: 'backend', readyAt: 4 },
  { id: 'worker', name: 'worker', kind: 'backend', readyAt: 4 },
  { id: 'postgres', name: 'postgresql', kind: 'infra', readyAt: 3 },
  { id: 'redis', name: 'redis', kind: 'infra', readyAt: 3 },
  { id: 'kafka', name: 'kafka', kind: 'infra', readyAt: 3 },
]
const edges = [
  { id: 'web-api', readyAt: 5 },
  { id: 'api-postgres', readyAt: 4 },
  { id: 'api-redis', readyAt: 4 },
  { id: 'worker-postgres', readyAt: 4 },
  { id: 'worker-kafka', readyAt: 4 },
]
const finalScene = 5

const { frontmatter } = useData()
const route = useRoute()
const root = ref<HTMLElement>()
const scene = ref(finalScene)
const visible = ref(false)
const pageVisible = ref(true)
const reducedMotion = ref(false)

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

let sceneTimer: ReturnType<typeof setInterval> | undefined
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
    if (scene.value < finalScene) scene.value += 1
    if (scene.value === finalScene) stopSceneTimer()
  }, 1200)
}

function resetScene() {
  stopSceneTimer()
  scene.value = reducedMotion.value ? finalScene : 0
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
    aria-labelledby="homepage-showcase-title"
  >
    <div class="homepage-showcase-heading">
      <p class="homepage-showcase-label">{{ showcase.label }}</p>
      <h2 id="homepage-showcase-title">{{ showcase.title }}</h2>
      <p>{{ showcase.description }}</p>
    </div>

    <div class="showcase-demo">
      <div class="showcase-conversation">
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
        <div class="showcase-dashboard-bar">
          <div><strong>{{ showcase.dashboardLabel }}</strong><span>{{ showcase.environment }}</span></div>
          <span v-if="scene === finalScene" class="showcase-dashboard-health">{{ showcase.healthy }}</span>
        </div>
        <div class="showcase-graph">
          <div class="showcase-edges" aria-hidden="true">
            <span
              v-for="edge in edges"
              :key="edge.id"
              class="showcase-edge"
              :class="[`edge-${edge.id}`, { 'is-active': scene >= edge.readyAt }]"
            />
          </div>
          <article
            v-for="node in nodes"
            :key="node.id"
            class="showcase-node"
            :class="[`node-${node.id}`, `kind-${node.kind}`, { 'is-healthy': scene >= node.readyAt }]"
          >
            <span class="showcase-node-kind">{{ node.kind }}</span>
            <strong>{{ node.name }}</strong>
            <span class="showcase-node-status">
              <i aria-hidden="true" />{{ scene >= node.readyAt ? showcase.healthy : showcase.starting }}
            </span>
          </article>
        </div>
        <ul class="showcase-relationships">
          <li v-for="relationship in showcase.relationships" :key="relationship">{{ relationship }}</li>
        </ul>
      </div>
    </div>

    <div class="showcase-outcome">
      <p role="status" aria-live="polite">{{ status }}</p>
      <a :href="showcase.link">{{ showcase.linkText }} <span aria-hidden="true">→</span></a>
    </div>
  </section>
</template>
