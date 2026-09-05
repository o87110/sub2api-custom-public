<template>
  <div class="text-xs">
    <span v-if="enabled" class="badge badge-success">{{ t('payment.admin.renewalEnabled') }}</span>
    <span v-else class="text-gray-400 dark:text-gray-500">{{ t('payment.admin.renewalDisabled') }}</span>
    <p v-if="enabled" class="mt-1 text-gray-500 dark:text-gray-400">
      {{ t('payment.admin.renewalGraceDaysValue', { days: graceDays }) }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ enabled: boolean; graceDays: number }>()
const { t } = useI18n()
</script>
