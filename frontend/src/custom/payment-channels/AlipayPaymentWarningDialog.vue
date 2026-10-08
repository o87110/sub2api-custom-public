<template>
  <BaseDialog
    :show="show"
    :title="t('payment.alipayWarning.title')"
    width="wide"
    @close="emit('cancel')"
  >
    <div class="alipay-payment-warning-content space-y-5">
      <div
        role="alert"
        class="flex items-center gap-3 rounded-xl border-2 border-red-500 bg-red-50 px-4 py-4 text-red-700 dark:bg-red-950/40 dark:text-red-300 sm:gap-4 sm:px-5"
      >
        <span class="flex h-12 w-12 shrink-0 items-center justify-center rounded-full bg-red-200 dark:bg-red-900/70">
          <span class="flex h-10 w-10 items-center justify-center rounded-full bg-red-600 text-white">
            <Icon name="exclamationTriangle" size="lg" :stroke-width="2" aria-hidden="true" />
          </span>
        </span>
        <i18n-t keypath="payment.alipayWarning.alert" tag="p" scope="global" class="min-w-0 text-base font-bold leading-relaxed sm:text-lg">
          <template #exactMatch>
            <span class="mt-1 block text-xl sm:text-2xl">{{ t('payment.alipayWarning.exactMatch') }}</span>
          </template>
        </i18n-t>
      </div>

      <ol class="space-y-4 text-sm leading-relaxed text-gray-700 dark:text-gray-200 sm:text-base">
        <li class="flex items-start gap-3">
          <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-red-100 font-bold text-red-700 dark:bg-red-900/50 dark:text-red-200">1</span>
          <i18n-t keypath="payment.alipayWarning.amountNotice" tag="p" scope="global" class="min-w-0">
            <template #tailAmount>
              <strong class="font-bold text-red-600 dark:text-red-400">{{ t('payment.alipayWarning.tailAmount') }}</strong>
            </template>
            <template #amount>
              <strong class="font-bold text-gray-900 dark:text-white">{{ formattedAmount }}</strong>
            </template>
            <template #tailAmountExamples>
              <strong class="font-bold text-red-600 dark:text-red-400">{{ tailAmountExamples }}</strong>
            </template>
            <template #paymentPageAmount>
              <strong class="font-bold text-red-600 dark:text-red-400">{{ t('payment.alipayWarning.paymentPageAmount') }}</strong>
            </template>
          </i18n-t>
        </li>
        <li class="flex items-start gap-3">
          <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-red-100 font-bold text-red-700 dark:bg-red-900/50 dark:text-red-200">2</span>
          <i18n-t keypath="payment.alipayWarning.tailIsMarker" tag="p" scope="global" class="min-w-0">
            <template #notAServiceFee>
              <strong class="font-bold text-red-600 dark:text-red-400">{{ t('payment.alipayWarning.notAServiceFee') }}</strong>
            </template>
          </i18n-t>
        </li>
        <li class="flex items-start gap-3">
          <span class="flex h-7 w-7 shrink-0 items-center justify-center rounded-full bg-red-100 font-bold text-red-700 dark:bg-red-900/50 dark:text-red-200">3</span>
          <p class="font-bold text-red-600 dark:text-red-400">{{ t('payment.alipayWarning.mismatchWillFail') }}</p>
        </li>
      </ol>
    </div>

    <template #footer>
      <div class="flex flex-col-reverse gap-3 sm:flex-row sm:justify-end">
        <button type="button" class="btn btn-secondary min-h-11" @click="emit('cancel')">
          {{ t('payment.alipayWarning.cancel') }}
        </button>
        <button type="button" class="btn alipay-confirm-button min-h-11" @click="emit('confirm')">
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

const formattedAmount = computed(() => formatPaymentAmount(props.amount, props.currency, props.locale))
const tailAmountExamples = computed(() => [0.01, 0.02]
  .map(tail => formatPaymentAmount(props.amount + tail, props.currency, props.locale))
  .join(' / '))
</script>

<style scoped>
/* 通过内容标记定位当前弹窗，兼容 BaseDialog 的 Teleport。 */
:global(.modal-content:has(.alipay-payment-warning-content)) {
  max-width: 40rem;
}

.alipay-confirm-button {
  background-color: #00b5e8;
  color: white;
  box-shadow: 0 4px 12px rgb(0 181 232 / 25%);
}

.alipay-confirm-button:hover {
  background-color: #009dcc;
  box-shadow: 0 6px 16px rgb(0 181 232 / 30%);
}

.alipay-confirm-button:focus-visible {
  --tw-ring-color: rgb(0 181 232 / 50%);
}
</style>
