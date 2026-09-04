import { defineComponent, h, nextTick, onMounted, onUnmounted, watch } from 'vue'
import { useRoute } from 'vitepress'
import DefaultTheme from 'vitepress/theme'
import HeroOrbit from './HeroOrbit.vue'
import HomepageShowcase from './HomepageShowcase.vue'
import './orbit.css'
import './homepage-showcase.css'

function addMobileNavigationEscape() {
  const onKeydown = (event: KeyboardEvent) => {
    if (event.key !== 'Escape') return
    const toggle = document.querySelector<HTMLButtonElement>('.VPNavBarHamburger[aria-expanded="true"]')
    if (!toggle) return
    toggle.click()
    toggle.focus()
  }

  document.addEventListener('keydown', onKeydown)
  return () => document.removeEventListener('keydown', onKeydown)
}

function setHomeLandmark() {
  const content = document.querySelector<HTMLElement>('#VPContent')
  if (!content) return
  if (content.classList.contains('is-home')) content.setAttribute('role', 'main')
  else content.removeAttribute('role')
}

export default {
  extends: DefaultTheme,
  Layout: defineComponent({
    setup() {
      const route = useRoute()
      let cleanup: (() => void) | undefined
      let stopWatching: (() => void) | undefined
      onMounted(() => {
        cleanup = addMobileNavigationEscape()
        stopWatching = watch(() => route.path, async () => {
          await nextTick()
          setHomeLandmark()
        }, { immediate: true })
      })
      onUnmounted(() => cleanup?.())
      onUnmounted(() => stopWatching?.())
      return () => h(DefaultTheme.Layout, null, {
        'home-hero-image': () => h(HeroOrbit),
        'home-features-after': () => h(HomepageShowcase),
      })
    },
  }),
}
