<template>
  <BaseDialog
    :show="show"
    :title="t('payment.alipayWarning.title')"
    width="wide"
    @close="emit('cancel')"
  >
    <div class="space-y-5">
      <div
        role="alert"
        class="flex items-start gap-4 rounded-xl border-2 border-red-500/80 bg-red-950/30 px-4 py-4 text-red-200 dark:bg-red-950/40 sm:px-5"
      >
        <span class="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-red-600/30 text-red-300">
          <Icon name="exclamationTriangle" size="lg" :stroke-width="2" aria-hidden="true" />
        </span>
        <p class="text-lg font-bold leading-relaxed sm:text-xl">
          {{ t('payment.alipayWarning.alert') }}
        </p>
      </div>

      <ol class="space-y-4 text-sm leading-relaxed text-gray-700 dark:text-gray-200 sm:text-base">
        <li class="flex items-start gap-3">
          <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-red-100 font-bold text-red-700 dark:bg-red-900/50 dark:text-red-200">1</span>
          <p>{{ amountNotice }}</p>
        </li>
        <li class="flex items-start gap-3">
          <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-red-100 font-bold text-red-700 dark:bg-red-900/50 dark:text-red-200">2</span>
          <p>{{ t('payment.alipayWarning.tailIsMarker') }}</p>
        </li>
        <li class="flex items-start gap-3">
          <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-red-100 font-bold text-red-700 dark:bg-red-900/50 dark:text-red-200">3</span>
          <p class="font-semibold text-red-600 dark:text-red-300">{{ t('payment.alipayWarning.mismatchWillFail') }}</p>
        </li>
      </ol>
    </div>

    <template #footer>
      <div class="flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <button type="button" class="btn btn-secondary min-h-11" @click="emit('cancel')">
          {{ t('payment.alipayWarning.cancel') }}
        </button>
        <button type="button" class="btn btn-primary min-h-11" @click="emit('confirm')">
          {{ t('payment.alipayWarning.confirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { formatPaymentAmount } from '@/components/payment/currency'

const props = withDefaults(defineProps<{
  show: boolean
  amount: number
  currency?: string
  locale?: string
}>(), {
  currency: 'CNY',
  locale: undefined,
})

const emit = defineEmits<{
  cancel: []
  confirm: []
}>()

const { t } = useI18n()

const amountNotice = computed(() => t('payment.alipayWarning.amountNotice', {
  amount: formatPaymentAmount(props.amount, props.currency, props.locale),
  amountPlusOneCent: formatPaymentAmount(props.amount + 0.01, props.currency, props.locale),
  amountPlusTwoCents: formatPaymentAmount(props.amount + 0.02, props.currency, props.locale),
}))
</script>
