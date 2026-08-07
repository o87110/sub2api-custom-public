<template>
  <div>
    <label class="input-label">{{ t('payment.admin.soldOutAction') }}</label>
    <Select
      data-testid="sold-out-action-select"
      :model-value="modelValue"
      :options="options"
      class="w-full"
      @update:model-value="emit('update:modelValue', $event as SoldOutAction)"
    />
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
      {{ t('payment.admin.soldOutActionHint') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/components/common/Select.vue'
import {
  SOLD_OUT_ACTION_DELIST,
  SOLD_OUT_ACTION_DISABLE_PURCHASE,
  type SoldOutAction,
} from './inventory'

defineProps<{ modelValue: SoldOutAction }>()
const emit = defineEmits<{ 'update:modelValue': [value: SoldOutAction] }>()
const { t } = useI18n()

const options = computed(() => [
  { value: SOLD_OUT_ACTION_DELIST, label: t('payment.admin.soldOutActionDelist') },
  { value: SOLD_OUT_ACTION_DISABLE_PURCHASE, label: t('payment.admin.soldOutActionDisablePurchase') },
])
</script>
