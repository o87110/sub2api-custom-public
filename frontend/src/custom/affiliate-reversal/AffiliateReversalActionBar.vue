<template>
  <div
    v-if="selectedRecords.length > 0"
    class="flex flex-col gap-3 border-b border-red-100 bg-red-50/70 px-4 py-3 sm:flex-row sm:items-center sm:justify-between dark:border-red-900/40 dark:bg-red-950/20"
    role="region"
    :aria-label="t('admin.affiliates.reversal.action')"
  >
    <div class="flex flex-wrap items-center gap-2 text-sm">
      <span class="font-semibold text-red-800 dark:text-red-200">
        {{ t('admin.affiliates.reversal.selected', { count: selectedRecords.length }) }}
      </span>
      <span class="text-red-300 dark:text-red-800" aria-hidden="true">•</span>
      <span class="text-red-700 dark:text-red-300">
        {{ t('admin.affiliates.reversal.selectedAmount', { amount: formatAmount(selectedAmount) }) }}
      </span>
      <button
        type="button"
        class="rounded px-1.5 py-1 text-xs font-medium text-red-700 hover:bg-red-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500 dark:text-red-300 dark:hover:bg-red-900/30"
        :disabled="busy"
        @click="emit('clear')"
      >
        {{ t('admin.affiliates.reversal.clearSelection') }}
      </button>
    </div>
    <button
      type="button"
      class="btn btn-danger min-h-11"
      :disabled="busy"
      @click="showDialog = true"
    >
      <Icon name="ban" size="sm" class="mr-2" aria-hidden="true" />
      {{ t('admin.affiliates.reversal.action') }}
    </button>
  </div>

  <AffiliateReversalDialog
    :show="showDialog"
    :order-ids="selectedRecords.map(record => record.order_id)"
    @close="showDialog = false"
    @busy-change="busy = $event"
    @completed="handleCompleted"
  />
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import type { AffiliateRebateRecord, AffiliateReversalResult } from '@/api/admin/affiliates'
import AffiliateReversalDialog from './AffiliateReversalDialog.vue'

const props = defineProps<{
  selectedRecords: AffiliateRebateRecord[]
}>()

const emit = defineEmits<{
  clear: []
  completed: [result: AffiliateReversalResult]
}>()

const { t } = useI18n()
const showDialog = ref(false)
const busy = ref(false)
const selectedAmount = computed(() =>
  props.selectedRecords.reduce((sum, record) => sum + Number(record.rebate_amount || 0), 0),
)

function formatAmount(value: number): string {
  return Number(value || 0).toFixed(2)
}

function handleCompleted(result: AffiliateReversalResult) {
  showDialog.value = false
  emit('completed', result)
}
</script>
