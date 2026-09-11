<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch, nextTick } from 'vue'
import { useRoute } from 'vitepress'

const route = useRoute()
const isHome = computed(() => route.path === '/' || route.path === '/zh-TW/')
const chinese = computed(() => route.path.startsWith('/zh-TW/'))
const root = ref<HTMLElement>()
const video = ref<HTMLVideoElement>()
const failed = ref(false)
const format = ref('webm')
let observer: IntersectionObserver | undefined
let motionQuery: MediaQueryList | undefined
let visible = false
let userPaused = false

async function play() {
  if (!video.value || failed.value) return
  const requestedFormat = format.value
  try { await video.value.play() } catch (error) {
    if (requestedFormat !== format.value) return
    if (error instanceof DOMException && ['NotAllowedError', 'AbortError'].includes(error.name)) return
    if (error instanceof DOMException && error.name === 'NotSupportedError') {
      await onMediaError()
      return
    }
    failed.value = true
  }
}
function syncPlayback() {
  if (!video.value) return
  if (!visible || document.hidden || motionQuery?.matches) video.value.pause()
  else if (!userPaused && !video.value.ended) void play()
}
function onPause() {
  if (visible && !document.hidden && !motionQuery?.matches && !video.value?.ended) userPaused = true
}
function replay() {
  if (!video.value) return
  userPaused = false
  if (failed.value) { failed.value = false; video.value.load() }
  video.value.currentTime = 0
  void play()
}
async function onMediaError() {
  if (format.value === 'webm' && video.value?.canPlayType('video/mp4; codecs="avc1.640028, mp4a.40.2"')) {
    format.value = 'mp4'
    await nextTick()
    video.value?.load()
    syncPlayback()
  } else failed.value = true
}
async function observeDemo() {
  observer?.disconnect()
  visible = false
  userPaused = false
  failed.value = false
  await nextTick()
  if (root.value) observer?.observe(root.value)
}
watch(() => route.path, observeDemo)
onMounted(() => {
  motionQuery = window.matchMedia('(prefers-reduced-motion: reduce)')
  motionQuery.addEventListener('change', syncPlayback)
  document.addEventListener('visibilitychange', syncPlayback)
  observer = new IntersectionObserver(([entry]) => {
    visible = entry.isIntersecting
    syncPlayback()
  }, { threshold: 0.35 })
  void observeDemo()
})
onUnmounted(() => {
  observer?.disconnect()
  motionQuery?.removeEventListener('change', syncPlayback)
  document.removeEventListener('visibilitychange', syncPlayback)
})
</script>

<template>
  <section v-if="isHome" id="demo" ref="root" class="homepage-showcase" aria-labelledby="demo-title">
    <header>
      <h2 id="demo-title">{{ chinese ? '問一次，看見整個環境依序啟動。' : 'Ask once. See the whole environment come alive.' }}</h2>
      <p>{{ chinese ? '從 Agent 對話、執行指令，到依序啟動服務與檢查問題。' : 'From an agent conversation to commands, ordered startup, and diagnosing a failing dependency.' }}</p>
    </header>
    <video ref="video" controls muted playsinline preload="metadata" width="1920" height="1080"
      :poster="'/media/orbit-showcase.png'" :src="`/media/orbit-launch.${format}`"
      :aria-label="chinese ? 'Orbit 產品展示影片（英文），34 秒' : 'Orbit product demo, 34 seconds'"
      aria-describedby="demo-description" @pause="onPause" @play="userPaused = false" @error="onMediaError">
    </video>
    <div class="demo-footer">
      <p id="demo-description">{{ chinese ? '34 秒操作示意 · 英文畫面，無旁白。Agent 檢查並啟動環境、修正設定，再由 Orbit dashboard 呈現狀態。' : '34-second illustrative workflow · Music only. The agent inspects and starts the environment, fixes configuration, and checks state in the Orbit dashboard.' }}</p>
      <button type="button" @click="replay">{{ failed ? (chinese ? '重新載入影片' : 'Retry video') : (chinese ? '從頭播放' : 'Play from start') }}</button>
    </div>
    <p v-if="failed" role="alert">{{ chinese ? '影片無法載入，請重試。你也可以先閱讀操作說明。' : 'The video could not load. Please retry, or read the workflow guide.' }} <a :href="chinese ? '/zh-TW/docs/local-first' : '/docs/local-first'">{{ chinese ? '操作說明' : 'Workflow guide' }}</a></p>
  </section>
</template>
