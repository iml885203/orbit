<script setup lang="ts">
import { computed } from 'vue'
import { useData } from 'vitepress'
import { homepageCopyFor } from '../homepage-copy'

const { lang } = useData()
const copy = computed(() => homepageCopyFor(lang.value))
</script>

<template>
  <section class="homepage-columns" :aria-label="copy.columnsLabel">
    <div class="homepage-columns__inner">
      <div v-for="column in copy.columns" :key="column.eyebrow" class="homepage-columns__item">
        <p class="homepage-columns__eyebrow">{{ column.eyebrow }}</p>
        <h2>{{ column.title }}</h2>
        <p>{{ column.body }}</p>
      </div>
    </div>
  </section>
</template>
