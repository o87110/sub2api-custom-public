<template>
  <div>
    <label class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
      {{ t('payment.paymentMethod') }}
    </label>
    <div class="grid grid-cols-1 gap-3 min-[375px]:grid-cols-2 sm:flex sm:flex-wrap" role="group" :aria-label="t('payment.paymentMethod')">
      <button
        v-for="channel in methods"
        :key="channel.id"
        type="button"
        :disabled="!channel.available"
        :aria-pressed="selected === channel.id"
        :aria-label="paymentChannelLabel(channel, t)"
        :class="[
          'relative flex min-h-[64px] min-w-0 flex-col items-center justify-center rounded-lg border px-3 py-2 text-left transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary-500 focus-visible:ring-offset-2 dark:focus-visible:ring-offset-dark-900 sm:min-w-[148px] sm:flex-1',
          !channel.available
            ? 'cursor-not-allowed border-gray-200 bg-gray-50 text-gray-400 opacity-60 dark:border-dark-700 dark:bg-dark-800/50 dark:text-dark-500'
            : selected === channel.id
              ? selectedClass(channel.payment_type)
              : 'border-gray-300 bg-white text-gray-700 hover:border-gray-400 dark:border-dark-600 dark:bg-dark-800 dark:text-gray-200 dark:hover:border-dark-500',
        ]"
        @click="channel.available && emit('select', channel.id)"
      >
        <span class="flex min-w-0 items-center gap-2">
          <img :src="methodIcon(channel.payment_type)" alt="" class="h-7 w-7 shrink-0 object-contain" />
          <span class="flex min-w-0 flex-col items-start gap-0.5 leading-tight">
            <span class="text-sm font-semibold sm:text-base">{{ paymentChannelLabel(channel, t) }}</span>
            <span
              v-if="!channel.available"
              class="text-xs font-medium"
            >
              {{ t('payment.channels.unavailable') }}
            </span>
            <span
              v-else-if="channel.fee_rate > 0"
              class="text-xs tracking-wide text-gray-500 dark:text-dark-400"
            >
              {{ t('payment.fee') }} {{ channel.fee_rate }}%
            </span>
          </span>
        </span>
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { isBuiltInAlipayMethod, isBuiltInWxpayMethod } from '@/components/payment/providerConfig'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import stripeIcon from '@/assets/icons/stripe.svg'
import airwallexIcon from '@/assets/icons/airwallex.svg'
import paymentIcon from '@/assets/icons/payment.svg'
import { paymentChannelLabel, type PaymentChannelOption } from './paymentChannels'

defineProps<{
  methods: PaymentChannelOption[]
  selected: string
}>()

const emit = defineEmits<{
  select: [id: string]
}>()

const { t } = useI18n()

function methodIcon(paymentType: string): string {
  if (isBuiltInAlipayMethod(paymentType)) return alipayIcon
  if (isBuiltInWxpayMethod(paymentType)) return wxpayIcon
  if (paymentType === 'stripe') return stripeIcon
  if (paymentType === 'airwallex') return airwallexIcon
  return paymentIcon
}

function selectedClass(paymentType: string): string {
  if (isBuiltInAlipayMethod(paymentType)) return 'border-[#02A9F1] bg-blue-50 text-gray-900 shadow-sm dark:bg-blue-950 dark:text-gray-100'
  if (isBuiltInWxpayMethod(paymentType)) return 'border-[#09BB07] bg-green-50 text-gray-900 shadow-sm dark:bg-green-950 dark:text-gray-100'
  if (paymentType === 'stripe') return 'border-[#676BE5] bg-indigo-50 text-gray-900 shadow-sm dark:bg-indigo-950 dark:text-gray-100'
  if (paymentType === 'airwallex') return 'border-[#FF6B3D] bg-orange-50 text-gray-900 shadow-sm dark:border-[#FF8E3C] dark:bg-orange-950 dark:text-gray-100'
  return 'border-primary-500 bg-primary-50 text-gray-900 shadow-sm dark:bg-primary-950 dark:text-gray-100'
}
</script>
