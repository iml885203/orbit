<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useData } from 'vitepress'
import HeroOrbit from './HeroOrbit.vue'
import { homepageCopyFor, license, platforms } from '../homepage-copy'

const { lang, frontmatter } = useData()
const copy = computed(() => homepageCopyFor(lang.value))
const release = computed(() => frontmatter.value.latestRelease as string | undefined)
const section = ref<HTMLElement>()
const promptElement = ref<HTMLElement>()
const copyState = ref<'idle' | 'copied' | 'failed'>('idle')
let resetTimer: number | undefined

function selectPrompt() {
  const node = promptElement.value
  if (!node) return
  const range = document.createRange()
  range.selectNodeContents(node)
  const selection = window.getSelection()
  selection?.removeAllRanges()
  selection?.addRange(range)
}

async function copyPrompt() {
  try {
    await navigator.clipboard.writeText(copy.value.prompt)
    copyState.value = 'copied'
  } catch (error) {
    // Clipboard access can be denied or absent; selecting the text keeps the
    // action completable and the button label says what happened.
    if (import.meta.env.DEV) console.error('copyPrompt:', error)
    selectPrompt()
    copyState.value = 'failed'
  }
  window.clearTimeout(resetTimer)
  resetTimer = window.setTimeout(() => { copyState.value = 'idle' }, 2400)
}

onUnmounted(() => window.clearTimeout(resetTimer))
</script>

<template>
  <section ref="section" class="homepage-hero" aria-labelledby="homepage-headline">
    <div class="homepage-hero__text">
      <h1 id="homepage-headline">{{ copy.headline[0] }}<br>{{ copy.headline[1] }}</h1>
      <p class="homepage-hero__subhead">{{ copy.subhead }}</p>
    </div>
    <div class="homepage-hero__field">
      <HeroOrbit :parallax-area="section" />
    </div>
    <div class="homepage-hero__actions">
      <div :id="copy.getStartedId" class="prompt-card">
        <div class="prompt-card__bar">
          <span>{{ copy.promptLabel }}</span>
          <button type="button" :data-state="copyState" @click="copyPrompt">{{ copy.copyButton[copyState] }}</button>
          <span class="visually-hidden" role="status">{{ copyState === 'idle' ? '' : copy.copyButton[copyState] }}</span>
        </div>
        <p ref="promptElement" class="prompt-card__text">{{ copy.prompt }}</p>
      </div>
      <p class="homepage-hero__install">
        {{ copy.install.prefix }} <code>brew install iml885203/tap/orbit</code> ·
        <a :href="copy.install.href">{{ copy.install.others }}</a>
      </p>
      <p class="homepage-hero__trust">
        <b>{{ platforms }}</b> <i aria-hidden="true">·</i> {{ license }}
        <template v-if="release"> <i aria-hidden="true">·</i> <span data-release>{{ release }}</span></template>
      </p>
    </div>
  </section>
</template>
