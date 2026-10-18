<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useData, useRoute } from 'vitepress'
import { filmDuration, homepageCopyFor } from '../homepage-copy'

const route = useRoute()
const { lang } = useData()
const copy = computed(() => homepageCopyFor(lang.value))
const video = ref<HTMLVideoElement>()
const failed = ref(false)
const started = ref(false)
const format = ref('webm')
let playRequested = false

// The film plays only on the visitor's own click, so the first play is a user
// gesture and needs no muting; native controls take over from then on.
function play() {
  playRequested = true
  return attemptPlay()
}

async function attemptPlay() {
  if (!video.value) return
  const requestedFormat = format.value
  try {
    await video.value.play()
    started.value = true
  } catch (error) {
    // The element's error event fires before a pending play() rejects, so by
    // now onMediaError may already have moved on to the mp4 source.
    if (requestedFormat !== format.value) return
    if (error instanceof DOMException && error.name === 'AbortError') return
    if (error instanceof DOMException && error.name === 'NotSupportedError') {
      await onMediaError()
      return
    }
    if (import.meta.env.DEV) console.error('film play:', error)
    failed.value = true
  }
}

async function retry() {
  if (!video.value) return
  failed.value = false
  video.value.load()
  await play()
}

async function onMediaError() {
  if (format.value === 'webm' && video.value?.canPlayType('video/mp4; codecs="avc1.640028, mp4a.40.2"')) {
    format.value = 'mp4'
    await nextTick()
    video.value?.load()
    if (playRequested) await attemptPlay()
    return
  }
  if (import.meta.env.DEV) console.error('film source failed:', video.value?.error)
  failed.value = true
}

// The poster's metadata request can fail before Vue attaches the error
// listener, so an element that is already in error on mount is handled too.
onMounted(() => {
  if (video.value?.error) void onMediaError()
})

// The layout keeps this component alive across the two homepages, so a
// locale switch must not carry over a started, failed or still-playing film.
watch(() => route.path, () => {
  failed.value = false
  started.value = false
  playRequested = false
  if (!video.value) return
  video.value.pause()
  video.value.currentTime = 0
})
</script>

<template>
  <section id="demo" class="homepage-showcase" aria-labelledby="demo-title">
    <h2 id="demo-title">{{ copy.film.heading }}</h2>
    <div class="film-frame">
      <video ref="video" playsinline preload="metadata" width="1920" height="1080" :controls="started"
        :poster="'/media/orbit-showcase.png'" :src="`/media/orbit-launch.${format}`"
        :aria-label="copy.film.label" aria-describedby="demo-description" @error="onMediaError">
      </video>
      <button v-if="!started && !failed" type="button" class="film-play" :aria-label="copy.film.play" @click="play">
        <svg aria-hidden="true" width="26" height="26" viewBox="0 0 24 24" fill="currentColor"><path d="M8 5v14l11-7z" /></svg>
      </button>
      <span v-if="!started" class="film-duration" aria-hidden="true">{{ filmDuration }}</span>
    </div>
    <p id="demo-description" class="film-caption">{{ copy.film.caption }}</p>
    <p v-if="failed" class="film-alert" role="alert">
      {{ copy.film.failed }} <a :href="copy.film.guideHref">{{ copy.film.guide }}</a>
      <button type="button" @click="retry">{{ copy.film.retry }}</button>
    </p>
  </section>
</template>
