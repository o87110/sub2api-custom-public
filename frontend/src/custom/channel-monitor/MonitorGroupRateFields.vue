<template>
  <div class="grid grid-cols-1 gap-4 sm:grid-cols-2">
    <div>
      <label class="input-label">{{ t('admin.channelMonitor.form.groupRateOverride') }}</label>
      <input
        :value="modelValue.override ?? ''"
        data-testid="monitor-group-rate-override"
        type="number"
        min="0.0001"
        step="0.0001"
        class="input"
        :placeholder="t('admin.channelMonitor.form.groupRateOverridePlaceholder')"
        @input="updateOverride"
      />
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.channelMonitor.form.groupRateOverrideHint') }}
      </p>
    </div>

    <div>
      <label class="input-label">{{ t('admin.channelMonitor.form.groupRateDisplayTemplate') }}</label>
      <input
        :value="modelValue.displayTemplate"
        data-testid="monitor-group-rate-template"
        type="text"
        :maxlength="GROUP_RATE_DISPLAY_TEMPLATE_MAX_LENGTH"
        class="input font-mono"
        :placeholder="DEFAULT_GROUP_RATE_DISPLAY_TEMPLATE"
        @input="updateDisplayTemplate"
      />
      <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('admin.channelMonitor.form.groupRateDisplayTemplateHint') }}
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

import {
  DEFAULT_GROUP_RATE_DISPLAY_TEMPLATE,
  GROUP_RATE_DISPLAY_TEMPLATE_MAX_LENGTH,
  type MonitorGroupRateFormState,
} from './groupRate'

const props = defineProps<{
  modelValue: MonitorGroupRateFormState
}>()

const emit = defineEmits<{
  (event: 'update:modelValue', value: MonitorGroupRateFormState): void
}>()

const { t } = useI18n()

function updateOverride(event: Event) {
  const raw = (event.target as HTMLInputElement).value
  emit('update:modelValue', {
    ...props.modelValue,
    override: raw === '' ? '' : Number(raw),
  })
}

function updateDisplayTemplate(event: Event) {
  emit('update:modelValue', {
    ...props.modelValue,
    displayTemplate: (event.target as HTMLInputElement).value,
  })
}
</script>
