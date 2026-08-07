<template>
  <div class="flex min-w-max items-center gap-2 text-sm">
    <span
      :class="soldOut ? 'font-medium text-red-600 dark:text-red-400' : 'text-gray-700 dark:text-gray-300'"
    >
      {{ quantityLabel }}
    </span>
    <span
      v-if="autoDelisted"
      class="badge badge-warning whitespace-nowrap"
    >
      {{ t('payment.admin.inventoryAutoDelisted') }}
    </span>
    <span
      v-else-if="soldOut && soldOutAction === SOLD_OUT_ACTION_DISABLE_PURCHASE"
      class="badge badge-warning whitespace-nowrap"
    >
      {{ t('payment.admin.inventoryPurchaseDisabled') }}
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  SOLD_OUT_ACTION_DELIST,
  SOLD_OUT_ACTION_DISABLE_PURCHASE,
  type SoldOutAction,
} from './inventory'

const props = withDefaults(defineProps<{
  quantity: number | null
  autoDelisted?: boolean
  soldOutAction?: SoldOutAction
}>(), {
  autoDelisted: false,
  soldOutAction: SOLD_OUT_ACTION_DELIST,
})

const { t } = useI18n()
const soldOut = computed(() => props.quantity === 0)
const quantityLabel = computed(() => {
  if (props.quantity == null) return t('payment.admin.inventoryUnlimited')
  if (soldOut.value) return t('payment.admin.inventorySoldOut')
  return String(props.quantity)
})
</script>
