<template>
  <span class="badge whitespace-nowrap" :class="action === SOLD_OUT_ACTION_DISABLE_PURCHASE ? 'badge-info' : 'badge-secondary'">
    {{ label }}
  </span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  SOLD_OUT_ACTION_DELIST,
  SOLD_OUT_ACTION_DISABLE_PURCHASE,
  type SoldOutAction,
} from './inventory'

const props = withDefaults(defineProps<{ action?: SoldOutAction }>(), {
  action: SOLD_OUT_ACTION_DELIST,
})
const { t } = useI18n()
const label = computed(() => props.action === SOLD_OUT_ACTION_DISABLE_PURCHASE
  ? t('payment.admin.soldOutActionDisablePurchase')
  : t('payment.admin.soldOutActionDelist'))
</script>
