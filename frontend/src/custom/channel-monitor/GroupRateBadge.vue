<template>
  <span
    v-if="displayValue"
    data-testid="channel-monitor-group-rate"
    class="inline-flex max-w-32 flex-shrink-0 items-center truncate rounded-md bg-gray-100 px-1.5 py-0.5 font-mono text-xs font-medium tabular-nums text-gray-700 dark:bg-dark-700 dark:text-gray-200"
    :title="displayValue"
    :aria-label="accessibleLabel"
  >
    {{ displayValue }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { renderGroupRateDisplay } from './groupRate'

const props = defineProps<{
  rate: number
  template?: string
}>()

const { locale } = useI18n()

const displayValue = computed(() => renderGroupRateDisplay(props.rate, props.template))

const accessibleLabel = computed(() => {
  return locale.value.toLowerCase().startsWith('zh')
    ? `分组倍率：${displayValue.value}`
    : `Group rate: ${displayValue.value}`
})
</script>
