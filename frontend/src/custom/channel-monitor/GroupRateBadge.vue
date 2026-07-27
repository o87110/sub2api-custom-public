<template>
  <span
    v-if="formattedRate"
    data-testid="channel-monitor-group-rate"
    class="inline-flex flex-shrink-0 items-center rounded-md bg-gray-100 px-1.5 py-0.5 font-mono text-xs font-medium tabular-nums text-gray-700 dark:bg-dark-700 dark:text-gray-200"
    :title="accessibleLabel"
    :aria-label="accessibleLabel"
  >
    {{ formattedRate }}x
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatGroupRateMultiplier } from './groupRate'

const props = defineProps<{
  rate: number
}>()

const { locale } = useI18n()

const formattedRate = computed(() => formatGroupRateMultiplier(props.rate))

const accessibleLabel = computed(() => {
  const value = `${formattedRate.value}x`
  return locale.value.toLowerCase().startsWith('zh')
    ? `分组默认倍率：${value}`
    : `Default group rate: ${value}`
})
</script>
