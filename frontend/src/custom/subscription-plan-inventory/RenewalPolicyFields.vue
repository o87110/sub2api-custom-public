<template>
  <section class="rounded-xl border border-gray-200 p-4 dark:border-dark-600">
    <div class="flex items-start justify-between gap-4">
      <div>
        <label :id="`${inputId}-label`" class="text-sm font-medium text-gray-700 dark:text-gray-300">
          {{ t('payment.admin.allowExistingUserRenewal') }}
        </label>
        <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
          {{ t('payment.admin.allowExistingUserRenewalHint') }}
        </p>
      </div>
      <button
        type="button"
        role="switch"
        :aria-checked="enabled"
        :aria-labelledby="`${inputId}-label`"
        :class="[
          'relative inline-flex h-6 w-11 shrink-0 rounded-full border-2 border-transparent transition-colors focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2',
          enabled ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600',
        ]"
        data-test="renewal-policy-toggle"
        @click="emit('update:enabled', !enabled)"
      >
        <span
          :class="[
            'pointer-events-none inline-block h-5 w-5 rounded-full bg-white shadow transition-transform',
            enabled ? 'translate-x-5' : 'translate-x-0',
          ]"
        />
      </button>
    </div>

    <div v-if="enabled" class="mt-4">
      <label :for="inputId" class="input-label">{{ t('payment.admin.renewalGraceDays') }}</label>
      <input
        :id="inputId"
        :value="graceDays"
        type="number"
        min="0"
        max="30"
        step="1"
        class="input"
        :aria-invalid="!!error"
        :aria-describedby="`${inputId}-hint${error ? ` ${inputId}-error` : ''}`"
        data-test="renewal-grace-days"
        @input="onInput"
      />
      <p :id="`${inputId}-hint`" class="mt-1 text-xs text-gray-500 dark:text-gray-400">
        {{ t('payment.admin.renewalGraceDaysHint') }}
      </p>
      <p v-if="error" :id="`${inputId}-error`" class="mt-1 text-xs text-red-600 dark:text-red-400">
        {{ error }}
      </p>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{
  enabled: boolean
  graceDays: number
  error?: string
}>()

const emit = defineEmits<{
  'update:enabled': [value: boolean]
  'update:graceDays': [value: number]
}>()

const { t } = useI18n()
const inputId = 'subscription-renewal-grace-days'

function onInput(event: Event) {
  const value = Number((event.target as HTMLInputElement).value)
  emit('update:graceDays', value)
}
</script>
