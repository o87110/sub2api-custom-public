<template>
  <div :class="toneClass" class="rounded-lg border px-4 py-3">
    <div class="text-xs font-medium">{{ label }}</div>
    <div class="mt-1 text-2xl font-semibold tabular-nums">{{ value }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{
  label: string
  value: number
  tone: 'neutral' | 'success' | 'warning' | 'danger'
}>()

const toneClass = computed(() => ({
  neutral: 'border-gray-200 bg-gray-50 text-gray-700 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200',
  success: 'border-green-200 bg-green-50 text-green-700 dark:border-green-900/50 dark:bg-green-900/20 dark:text-green-300',
  warning: 'border-amber-200 bg-amber-50 text-amber-700 dark:border-amber-900/50 dark:bg-amber-900/20 dark:text-amber-300',
  danger: 'border-red-200 bg-red-50 text-red-700 dark:border-red-900/50 dark:bg-red-900/20 dark:text-red-300'
})[props.tone])
</script>
