<template>
  <p
    v-if="manualQuotaResetCount > 0"
    class="text-xs text-gray-500 dark:text-dark-400"
    data-testid="subscription-cycle-stats"
  >
    {{
      t('subscriptionProgress.cycleStats', {
        usage: normalizedUsage.toFixed(2),
        count: manualQuotaResetCount
      })
    }}
  </p>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'

const props = defineProps<{
  cycleUsageUsd?: number
  manualQuotaResetCount?: number
}>()

const { t } = useI18n()
const normalizedUsage = computed(() =>
  Number.isFinite(props.cycleUsageUsd) ? Math.max(props.cycleUsageUsd ?? 0, 0) : 0
)
const manualQuotaResetCount = computed(() =>
  Math.max(Math.trunc(props.manualQuotaResetCount ?? 0), 0)
)
</script>
