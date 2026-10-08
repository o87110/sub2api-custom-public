<template>
  <a
    v-if="visibleText && safeURL"
    data-testid="invitation-code-hint-link"
    :href="safeURL"
    target="_blank"
    rel="noopener noreferrer"
    class="min-w-0 break-words text-sm font-normal text-primary-600 hover:underline dark:text-primary-400"
  >{{ visibleText }}</a>
  <span
    v-else-if="visibleText"
    data-testid="invitation-code-hint-text"
    class="min-w-0 break-words text-sm font-normal text-gray-500 dark:text-gray-400"
  >{{ visibleText }}</span>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ text: string; url: string }>()

const visibleText = computed(() => props.text.trim())
const safeURL = computed(() => {
  const raw = props.url.trim()
  if (!/^https?:\/\//i.test(raw) || [...raw].some(
    (char) => char === '\\' || char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127
  )) return ''
  try {
    const parsed = new URL(raw)
    if (!['http:', 'https:'].includes(parsed.protocol) || !parsed.hostname || parsed.username || parsed.password) {
      return ''
    }
    return raw
  } catch {
    return ''
  }
})
</script>
