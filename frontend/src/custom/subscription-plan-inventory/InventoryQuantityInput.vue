<template>
  <div>
    <label class="input-label" for="subscription-plan-remaining-quantity">
      {{ t('payment.admin.remainingQuantity') }}
    </label>
    <input
      id="subscription-plan-remaining-quantity"
      :value="modelValue"
      type="number"
      min="1"
      step="1"
      inputmode="numeric"
      class="input"
      :class="error ? 'border-red-500 focus:border-red-500 focus:ring-red-500' : ''"
      :aria-invalid="Boolean(error)"
      :aria-describedby="error ? 'subscription-plan-remaining-quantity-error' : 'subscription-plan-remaining-quantity-hint'"
      @input="onInput"
    />
    <p
      v-if="error"
      id="subscription-plan-remaining-quantity-error"
      class="mt-1 text-xs text-red-600 dark:text-red-400"
    >
      {{ error }}
    </p>
    <p
      v-else
      id="subscription-plan-remaining-quantity-hint"
      class="mt-1 text-xs text-gray-500 dark:text-gray-400"
    >
      {{ soldOut ? t('payment.admin.remainingQuantitySoldOutHint') : t('payment.admin.remainingQuantityHint') }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  modelValue: string
  error?: string
  soldOut?: boolean
}>()

const emit = defineEmits<{
  'update:modelValue': [value: string]
}>()

const { t } = useI18n()

function onInput(event: Event) {
  emit('update:modelValue', (event.target as HTMLInputElement).value)
}
</script>
