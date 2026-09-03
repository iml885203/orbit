<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useData, useRoute } from 'vitepress'

type ShowcaseStage = {
  signal: string
  title: string
  details: string
  link: string
  linkText: string
}

type ShowcaseContent = {
  label: string
  title: string
  description: string
  persistence: string
  stages: ShowcaseStage[]
}

const { frontmatter } = useData()
const route = useRoute()
const root = ref<HTMLElement>()
const activeStage = ref(0)
const visible = ref(false)
const pageVisible = ref(true)
const reducedMotion = ref(false)

const showcase = computed(() => {
  if (route.path !== '/' && route.path !== '/zh-TW/') return undefined
  return frontmatter.value.showcase as ShowcaseContent | undefined
})
const motion = computed(() => {
  if (reducedMotion.value) return 'reduced'
  return visible.value && pageVisible.value && showcase.value ? 'running' : 'paused'
})

let stageTimer: ReturnType<typeof setInterval> | undefined
let intersectionObserver: IntersectionObserver | undefined
let motionQuery: MediaQueryList | undefined

function stopStageTimer() {
  if (!stageTimer) return
  clearInterval(stageTimer)
  stageTimer = undefined
}

function syncStageTimer() {
  stopStageTimer()
  if (motion.value !== 'running' || !showcase.value) return
  stageTimer = setInterval(() => {
    activeStage.value = (activeStage.value + 1) % showcase.value!.stages.length
  }, 1800)
}

function onVisibilityChange() {
  pageVisible.value = !document.hidden
}

function onMotionChange(event: MediaQueryListEvent | MediaQueryList) {
  reducedMotion.value = event.matches
}

watch(motion, syncStageTimer)
watch(root, (current, previous) => {
  visible.value = false
  if (previous) intersectionObserver?.unobserve(previous)
  if (current) intersectionObserver?.observe(current)
})

onMounted(() => {
  pageVisible.value = !document.hidden
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  reducedMotion.value = motionQuery.matches
  motionQuery.addEventListener('change', onMotionChange)
  intersectionObserver = new IntersectionObserver(([entry]) => {
    if (entry.target === root.value) visible.value = entry.isIntersecting
  }, { threshold: 0.05 })
  if (root.value) intersectionObserver.observe(root.value)
  document.addEventListener('visibilitychange', onVisibilityChange)
  syncStageTimer()
})

onUnmounted(() => {
  stopStageTimer()
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
    :data-motion="motion"
    aria-labelledby="homepage-showcase-title"
  >
    <div class="homepage-showcase-heading">
      <p class="homepage-showcase-label">{{ showcase.label }}</p>
      <h2 id="homepage-showcase-title">{{ showcase.title }}</h2>
      <p>{{ showcase.description }}</p>
    </div>

    <ol class="homepage-showcase-flow">
      <li
        v-for="(stage, index) in showcase.stages"
        :key="stage.title"
        class="homepage-showcase-stage"
        :class="{
          'is-active': index === activeStage,
          'is-complete': index === activeStage && index === showcase.stages.length - 1,
        }"
        :data-stage="index + 1"
      >
        <span class="homepage-showcase-rail" aria-hidden="true"><span /></span>
        <div class="homepage-showcase-stage-number" aria-hidden="true">{{ String(index + 1).padStart(2, '0') }}</div>
        <div class="homepage-showcase-stage-content">
          <code>{{ stage.signal }}</code>
          <h3>{{ stage.title }}</h3>
          <p>{{ stage.details }}</p>
          <a :href="stage.link">{{ stage.linkText }} <span aria-hidden="true">→</span></a>
        </div>
      </li>
    </ol>

    <p class="homepage-showcase-persistence">
      <code>orbit.yaml</code>
      <span>{{ showcase.persistence }}</span>
    </p>
  </section>
</template>
