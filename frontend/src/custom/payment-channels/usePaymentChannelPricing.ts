import { computed, watch, type ComputedRef, type Ref } from 'vue'

import { DEFAULT_PAYMENT_CURRENCY, formatPaymentAmount, normalizePaymentCurrency } from '@/components/payment/currency'
import type { CheckoutInfoResponse, SubscriptionPlan } from '@/types/payment'
import {
  normalizePaymentChannelOptions,
  type PaymentChannelOption,
} from './paymentChannels'
import {
  isPaymentAmountRepresentable,
  multiplyAndRoundPaymentAmount,
  paymentCurrencyFractionDigits,
  paymentFeeAmount,
  roundPaymentAmount,
} from './paymentMoney'

type PaymentTab = 'recharge' | 'subscription'
type Translate = (key: string, params?: string | Record<string, unknown>) => string

export interface PaymentChannelPricingOptions {
  checkout: Ref<CheckoutInfoResponse>
  activeTab: Ref<PaymentTab>
  amount: Ref<number | null>
  selectedPlan: Ref<SubscriptionPlan | null>
  selectedChannelId: Ref<string>
  locale: () => unknown
  t: Translate
}

export interface PaymentChannelPricing {
  channelOptions: ComputedRef<PaymentChannelOption[]>
  enabledChannelIds: ComputedRef<string[]>
  selectedChannel: ComputedRef<PaymentChannelOption | undefined>
  validAmount: ComputedRef<number>
  balanceRechargeMultiplier: ComputedRef<number>
  subscriptionUsdToCnyRate: ComputedRef<number>
  creditedAmount: ComputedRef<number>
  globalMinAmount: ComputedRef<number>
  globalMaxAmount: ComputedRef<number>
  selectedLimit: ComputedRef<PaymentChannelOption | undefined>
  selectedCurrency: ComputedRef<string>
  methodOptions: ComputedRef<PaymentChannelOption[]>
  feeRate: ComputedRef<number>
  feeAmount: ComputedRef<number>
  totalAmount: ComputedRef<number>
  amountError: ComputedRef<string>
  canSubmit: ComputedRef<boolean>
  subPaymentAmount: ComputedRef<number>
  subFeeAmount: ComputedRef<number>
  subTotalAmount: ComputedRef<number>
  subMethodOptions: ComputedRef<PaymentChannelOption[]>
  canSubmitSubscription: ComputedRef<boolean>
  balanceAmountFitsChannel: (channel: PaymentChannelOption, value?: number) => boolean
  subscriptionAmountFitsChannel: (channel: PaymentChannelOption, price?: number) => boolean
  formatSelectedPaymentAmount: (value: number) => string
  formatSelectedSubscriptionPaymentAmount: (value: number) => string
}

export function usePaymentChannelPricing(options: PaymentChannelPricingOptions): PaymentChannelPricing {
  const channelOptions = computed(() => normalizePaymentChannelOptions(options.checkout.value))
  const enabledChannelIds = computed(() =>
    channelOptions.value.filter(option => option.available).map(option => option.id),
  )
  const selectedChannel = computed(() =>
    channelOptions.value.find(option => option.id === options.selectedChannelId.value),
  )
  const validAmount = computed(() => options.amount.value ?? 0)
  const balanceRechargeMultiplier = computed(() => {
    const multiplier = options.checkout.value.balance_recharge_multiplier
    return Number.isFinite(multiplier) && multiplier > 0 ? multiplier : 1
  })
  const subscriptionUsdToCnyRate = computed(() => {
    const rate = options.checkout.value.subscription_usd_to_cny_rate
    return Number.isFinite(rate) && rate > 0 ? rate : 0
  })
  const creditedAmount = computed(() =>
    Math.round((validAmount.value * balanceRechargeMultiplier.value) * 100) / 100,
  )

  function paymentChannelFeeRate(channel: PaymentChannelOption | undefined): number {
    const rate = channel?.fee_rate
    if (Number.isFinite(rate) && rate! >= 0) return rate!
    return options.checkout.value.recharge_fee_rate ?? 0
  }

  function amountFitsChannel(amount: number, channel: PaymentChannelOption): boolean {
    if (amount <= 0) return true
    if (channel.amount_ranges?.length) {
      return channel.amount_ranges.some(range =>
        (range.single_min <= 0 || amount >= range.single_min)
        && (range.single_max <= 0 || amount <= range.single_max),
      )
    }
    if (channel.single_min > 0 && amount < channel.single_min) return false
    if (channel.single_max > 0 && amount > channel.single_max) return false
    return true
  }

  function balanceTotalAmountForChannel(value: number, channel: PaymentChannelOption): number {
    const currency = normalizePaymentCurrency(channel.currency)
    const rate = paymentChannelFeeRate(channel)
    if (rate <= 0 || value <= 0) return value
    const fee = paymentFeeAmount(value, rate, currency)
    return roundPaymentAmount(value + fee, currency)
  }

  function balancePrincipalAmountForGatewayLimit(
    channel: PaymentChannelOption,
    limit: number,
    boundary: 'min' | 'max',
  ): number {
    if (!Number.isFinite(limit) || limit <= 0) return 0
    const rawRate = paymentChannelFeeRate(channel)
    const rate = Number.isFinite(rawRate) && rawRate > 0 ? rawRate : 0
    const estimate = limit / (1 + rate / 100)
    const currency = normalizePaymentCurrency(channel.currency)
    const currencyDigits = paymentCurrencyFractionDigits(currency)
    const inputScale = 10 ** Math.max(2, currencyDigits)
    const stepUnits = 10 ** Math.max(0, Math.max(2, currencyDigits) - currencyDigits)
    const estimatedUnits = estimate * inputScale
    let units = boundary === 'min'
      ? Math.ceil((estimatedUnits - 1e-9) / stepUnits) * stepUnits
      : Math.floor((estimatedUnits + 1e-9) / stepUnits) * stepUnits
    units = Math.max(boundary === 'min' ? stepUnits : 0, units)
    if (!Number.isSafeInteger(units)) return estimate

    const totalAt = (candidateUnits: number) =>
      balanceTotalAmountForChannel(candidateUnits / inputScale, channel)
    const tolerance = Number.EPSILON * Math.max(1, Math.abs(limit))

    if (boundary === 'min') {
      while (units > stepUnits && totalAt(units - stepUnits) + tolerance >= limit) units -= stepUnits
      while (totalAt(units) + tolerance < limit) units += stepUnits
    } else {
      while (units > 0 && totalAt(units) > limit + tolerance) units -= stepUnits
      while (totalAt(units + stepUnits) <= limit + tolerance) units += stepUnits
    }

    return units / inputScale
  }

  const balancePrincipalLimits = computed(() => channelOptions.value.flatMap((channel) => {
    const ranges = channel.amount_ranges?.length
      ? channel.amount_ranges
      : [{ single_min: channel.single_min, single_max: channel.single_max }]
    return ranges.map(range => ({
      min: balancePrincipalAmountForGatewayLimit(channel, range.single_min, 'min'),
      max: balancePrincipalAmountForGatewayLimit(channel, range.single_max, 'max'),
    }))
  }))
  const globalMinAmount = computed(() => {
    const limits = balancePrincipalLimits.value
    if (limits.length === 0 || limits.some(limit => limit.min <= 0)) return 0
    return Math.min(...limits.map(limit => limit.min))
  })
  const globalMaxAmount = computed(() => {
    const limits = balancePrincipalLimits.value
    if (limits.length === 0 || limits.some(limit => limit.max <= 0)) return 0
    return Math.max(...limits.map(limit => limit.max))
  })

  const selectedLimit = computed(() => selectedChannel.value)
  const selectedCurrency = computed(() => normalizePaymentCurrency(selectedLimit.value?.currency))
  const localeCode = computed(() => {
    const raw = options.locale()
    if (typeof raw === 'string') return raw
    if (raw && typeof raw === 'object' && 'value' in raw) {
      return String((raw as { value?: string }).value || '')
    }
    return undefined
  })

  function subscriptionPaymentAmountForCurrency(value: number, currency: string): number {
    const rate = subscriptionUsdToCnyRate.value
    if (rate <= 0 || currency !== DEFAULT_PAYMENT_CURRENCY) return value
    return multiplyAndRoundPaymentAmount(value, rate, currency)
  }

  function formatSelectedPaymentAmount(value: number): string {
    return formatPaymentAmount(value, selectedCurrency.value, localeCode.value)
  }

  function formatSelectedSubscriptionPaymentAmount(value: number): string {
    return formatSelectedPaymentAmount(
      subscriptionPaymentAmountForCurrency(value, selectedCurrency.value),
    )
  }

  function balanceAmountFitsChannel(
    channel: PaymentChannelOption,
    value = validAmount.value,
  ): boolean {
    const currency = normalizePaymentCurrency(channel.currency)
    if (!isPaymentAmountRepresentable(value, currency)) return false
    return amountFitsChannel(balanceTotalAmountForChannel(value, channel), channel)
  }

  const methodOptions = computed<PaymentChannelOption[]>(() =>
    channelOptions.value.map(channel => ({
      ...channel,
      available: channel.available && balanceAmountFitsChannel(channel),
    })),
  )
  const feeRate = computed(() => paymentChannelFeeRate(selectedChannel.value))
  const feeAmount = computed(() =>
    feeRate.value > 0
      && validAmount.value > 0
      && isPaymentAmountRepresentable(validAmount.value, selectedCurrency.value)
      ? paymentFeeAmount(validAmount.value, feeRate.value, selectedCurrency.value)
      : 0,
  )
  const totalAmount = computed(() =>
    selectedChannel.value && isPaymentAmountRepresentable(validAmount.value, selectedCurrency.value)
      ? balanceTotalAmountForChannel(validAmount.value, selectedChannel.value)
      : validAmount.value,
  )
  const amountError = computed(() => {
    if (validAmount.value <= 0) return ''
    if (!channelOptions.value.some(channel => channel.available && balanceAmountFitsChannel(channel))) {
      return options.t('payment.amountNoMethod')
    }
    const limit = selectedLimit.value
    if (!limit || limit.amount_ranges?.length) return ''
    const currency = normalizePaymentCurrency(limit.currency)
    if (!isPaymentAmountRepresentable(validAmount.value, currency)) return ''
    const payAmount = balanceTotalAmountForChannel(validAmount.value, limit)
    if (limit.single_min > 0 && payAmount < limit.single_min) {
      return options.t('payment.amountTooLow', { min: formatSelectedPaymentAmount(limit.single_min) })
    }
    if (limit.single_max > 0 && payAmount > limit.single_max) {
      return options.t('payment.amountTooHigh', { max: formatSelectedPaymentAmount(limit.single_max) })
    }
    return ''
  })
  const canSubmit = computed(() =>
    validAmount.value > 0
      && !!selectedChannel.value
      && balanceAmountFitsChannel(selectedChannel.value)
      && selectedLimit.value?.available !== false,
  )

  const subPaymentAmount = computed(() => {
    const price = options.selectedPlan.value?.price ?? 0
    return subscriptionPaymentAmountForCurrency(price, selectedCurrency.value)
  })
  const subFeeAmount = computed(() => {
    if (
      feeRate.value <= 0
      || subPaymentAmount.value <= 0
      || !isPaymentAmountRepresentable(subPaymentAmount.value, selectedCurrency.value)
    ) return 0
    return paymentFeeAmount(subPaymentAmount.value, feeRate.value, selectedCurrency.value)
  })
  const subTotalAmount = computed(() => {
    if (feeRate.value <= 0 || subPaymentAmount.value <= 0) return subPaymentAmount.value
    return roundPaymentAmount(subPaymentAmount.value + subFeeAmount.value, selectedCurrency.value)
  })

  function subscriptionTotalAmountForCurrency(
    value: number,
    currency: string,
    rate = feeRate.value,
  ): number {
    const paymentAmount = subscriptionPaymentAmountForCurrency(value, currency)
    if (rate <= 0 || paymentAmount <= 0) return paymentAmount
    const fee = paymentFeeAmount(paymentAmount, rate, currency)
    return roundPaymentAmount(paymentAmount + fee, currency)
  }

  function subscriptionAmountFitsChannel(
    channel: PaymentChannelOption,
    price = options.selectedPlan.value?.price ?? 0,
  ): boolean {
    const currency = normalizePaymentCurrency(channel.currency)
    const paymentAmount = subscriptionPaymentAmountForCurrency(price, currency)
    if (!isPaymentAmountRepresentable(paymentAmount, currency)) return false
    return amountFitsChannel(
      subscriptionTotalAmountForCurrency(price, currency, paymentChannelFeeRate(channel)),
      channel,
    )
  }

  const subMethodOptions = computed<PaymentChannelOption[]>(() =>
    channelOptions.value.map(channel => ({
      ...channel,
      available: channel.available && subscriptionAmountFitsChannel(channel),
    })),
  )
  const canSubmitSubscription = computed(() =>
    options.selectedPlan.value !== null
      && !!selectedChannel.value
      && subscriptionAmountFitsChannel(selectedChannel.value)
      && selectedLimit.value?.available !== false,
  )

  watch(
    () => [options.activeTab.value, validAmount.value, options.selectedChannelId.value] as const,
    ([tab, currentAmount, channelId]) => {
      if (tab !== 'recharge' || currentAmount <= 0) return
      const current = channelOptions.value.find(channel => channel.id === channelId)
      if (current && current.available && balanceAmountFitsChannel(current)) return
      const available = channelOptions.value.find(channel =>
        channel.available && balanceAmountFitsChannel(channel),
      )
      if (available) options.selectedChannelId.value = available.id
    },
  )

  watch(
    () => [options.activeTab.value, options.selectedPlan.value?.price ?? 0, options.selectedChannelId.value] as const,
    ([tab, price, channelId]) => {
      if (tab !== 'subscription' || price <= 0) return
      const current = channelOptions.value.find(channel => channel.id === channelId)
      if (current && current.available && subscriptionAmountFitsChannel(current)) return
      const available = channelOptions.value.find(channel =>
        channel.available && subscriptionAmountFitsChannel(channel),
      )
      if (available) options.selectedChannelId.value = available.id
    },
  )

  return {
    channelOptions,
    enabledChannelIds,
    selectedChannel,
    validAmount,
    balanceRechargeMultiplier,
    subscriptionUsdToCnyRate,
    creditedAmount,
    globalMinAmount,
    globalMaxAmount,
    selectedLimit,
    selectedCurrency,
    methodOptions,
    feeRate,
    feeAmount,
    totalAmount,
    amountError,
    canSubmit,
    subPaymentAmount,
    subFeeAmount,
    subTotalAmount,
    subMethodOptions,
    canSubmitSubscription,
    balanceAmountFitsChannel,
    subscriptionAmountFitsChannel,
    formatSelectedPaymentAmount,
    formatSelectedSubscriptionPaymentAmount,
  }
}
