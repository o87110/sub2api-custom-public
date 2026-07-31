<template>
  <div>
    <label class="input-label">{{ t('admin.groups.form.minimumBalance') }}</label>
    <input
      :value="modelValue"
      type="number"
      step="0.00000001"
      min="0"
      required
      class="input"
      data-testid="group-minimum-balance"
      @input="updateValue"
    />
    <p class="input-hint">{{ t('admin.groups.form.minimumBalanceHint') }}</p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import type { MinimumBalanceFormValue } from './minimumBalance'

defineProps<{
  modelValue: MinimumBalanceFormValue
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: MinimumBalanceFormValue): void
}>()

const { t } = useI18n()

function updateValue(event: Event) {
  const raw = (event.target as HTMLInputElement).value
  emit('update:modelValue', raw === '' ? '' : Number(raw))
}
</script>
