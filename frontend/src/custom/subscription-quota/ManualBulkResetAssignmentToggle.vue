<template>
  <label
    for="manual-assignment-bulk-quota-reset"
    class="flex cursor-pointer items-start justify-between gap-4 rounded-lg border border-gray-200 bg-gray-50 px-4 py-3 dark:border-dark-600 dark:bg-dark-800"
  >
    <span class="min-w-0">
      <span class="block text-sm font-medium text-gray-800 dark:text-gray-200">
        {{ t('admin.subscriptions.manualEligibility.assignLabel') }}
      </span>
      <span class="mt-1 block text-xs leading-5 text-gray-500 dark:text-gray-400">
        {{ t('admin.subscriptions.manualEligibility.assignHint') }}
      </span>
    </span>
    <span class="flex min-h-11 flex-shrink-0 items-center">
      <input
        id="manual-assignment-bulk-quota-reset"
        :checked="modelValue"
        type="checkbox"
        class="peer sr-only"
        data-testid="assign-bulk-reset-eligibility"
        @change="emit('update:modelValue', ($event.target as HTMLInputElement).checked)"
      />
      <span
        class="relative inline-flex h-6 w-11 rounded-full transition-colors duration-200 peer-focus-visible:ring-2 peer-focus-visible:ring-primary-500 peer-focus-visible:ring-offset-2 dark:peer-focus-visible:ring-offset-dark-800"
        :class="modelValue ? 'bg-primary-500' : 'bg-gray-300 dark:bg-dark-600'"
        aria-hidden="true"
      >
        <span
          class="inline-block h-5 w-5 translate-y-0.5 rounded-full bg-white shadow transition-transform duration-200"
          :class="modelValue ? 'translate-x-5' : 'translate-x-0.5'"
        />
      </span>
    </span>
  </label>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ modelValue: boolean }>()

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
}>()

const { t } = useI18n()
</script>
