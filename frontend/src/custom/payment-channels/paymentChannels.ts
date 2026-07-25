import type { CheckoutInfoResponse, MethodLimit, PaymentMethodOption } from '@/types/payment'
import { getVisibleMethods, normalizeVisibleMethod } from '@/components/payment/paymentFlow'

export const ALIPAY_MOBILE_PRECREATE_DEEP_LINK = 'alipay_mobile_precreate_deep_link'

export interface PaymentChannelOption {
  id: string
  payment_type: string
  provider_key: string
  display_name?: string
  currency?: string
  fee_rate: number
  daily_limit: number
  single_min: number
  single_max: number
  amount_ranges?: Array<{
    single_min: number
    single_max: number
  }>
  available: boolean
  capabilities?: string[]
  legacy?: boolean
}

const CHANNEL_ORDER = [
  'easypay_alipay',
  'official_alipay',
  'alipay',
  'easypay_wxpay',
  'official_wxpay',
  'wxpay',
  'stripe',
  'airwallex',
] as const

const FIXED_CHANNEL_LABEL_KEYS: Record<string, string> = {
  easypay_alipay: 'payment.channels.easypayAlipay',
  official_alipay: 'payment.channels.officialAlipay',
  easypay_wxpay: 'payment.channels.easypayWxpay',
  official_wxpay: 'payment.channels.officialWxpay',
}

type Translate = (key: string, fallback?: string | Record<string, unknown>) => string

export function normalizePaymentChannelOptions(checkout: CheckoutInfoResponse): PaymentChannelOption[] {
  const methodOptions = Array.isArray(checkout.method_options) ? checkout.method_options : []
  if (methodOptions.length > 0) {
    return sortPaymentChannelOptions(
      methodOptions
        .map(normalizeApiOption)
        .filter((option): option is PaymentChannelOption => option !== null),
    )
  }

  const visibleMethods = getVisibleMethods(checkout.methods || {})
  return sortPaymentChannelOptions(
    Object.entries(visibleMethods).map(([paymentType, limit]) =>
      buildLegacyOption(paymentType, limit, checkout),
    ),
  )
}

export function sortPaymentChannelOptions(options: PaymentChannelOption[]): PaymentChannelOption[] {
  return [...options].sort((left, right) => {
    const leftIndex = CHANNEL_ORDER.indexOf(left.id as (typeof CHANNEL_ORDER)[number])
    const rightIndex = CHANNEL_ORDER.indexOf(right.id as (typeof CHANNEL_ORDER)[number])
    const leftOrder = leftIndex < 0 ? 999 : leftIndex
    const rightOrder = rightIndex < 0 ? 999 : rightIndex
    if (leftOrder !== rightOrder) return leftOrder - rightOrder
    return left.id.localeCompare(right.id)
  })
}

export function paymentChannelLabel(option: PaymentChannelOption, t: Translate): string {
  const fixedKey = FIXED_CHANNEL_LABEL_KEYS[option.id]
  if (fixedKey) return t(fixedKey)
  return option.display_name || t(`payment.methods.${option.payment_type}`, option.payment_type)
}

export function findPaymentChannel(
  options: PaymentChannelOption[],
  channelID?: string,
  paymentType?: string,
  providerKey?: string,
): PaymentChannelOption | undefined {
  const normalizedProvider = (providerKey || '').trim().toLowerCase()
  if (channelID) {
    const exact = options.find(option => option.id === channelID)
    if (exact) return exact
  }
  const normalizedPaymentType = normalizeVisibleMethod(paymentType || '') || (paymentType || '').trim()
  if (!normalizedPaymentType) return undefined
  if (normalizedProvider) {
    const exactProvider = options.find(option =>
      option.payment_type === normalizedPaymentType && option.provider_key === normalizedProvider,
    )
    if (exactProvider) return exactProvider
  }
  return options.find(option => option.payment_type === normalizedPaymentType)
}

export function paymentChannelSupports(option: PaymentChannelOption | undefined, capability: string): boolean {
  return !!option?.capabilities?.includes(capability)
}

export function findBackupPaymentChannel(
  options: PaymentChannelOption[],
  currentChannelID: string,
  paymentType: string,
  canUse: (option: PaymentChannelOption) => boolean,
): PaymentChannelOption | undefined {
  const normalizedPaymentType = normalizeVisibleMethod(paymentType) || paymentType.trim()
  return options.find(option =>
    option.id !== currentChannelID
    && option.payment_type === normalizedPaymentType
    && option.available
    && canUse(option),
  )
}

export function isGatewayChannelFailureCode(code: string | undefined): boolean {
  const normalizedCode = code || ''
  return normalizedCode.startsWith('WXPAY_CONFIG_') || [
    'PAYMENT_GATEWAY_ERROR',
    'NO_AVAILABLE_INSTANCE',
    'PAYMENT_PROVIDER_MISCONFIGURED',
    'PAYMENT_METHOD_CURRENCY_CONFLICT',
    'WECHAT_PAYMENT_MP_NOT_CONFIGURED',
    'WECHAT_H5_NOT_AUTHORIZED',
  ].includes(normalizedCode)
}

function normalizeApiOption(raw: PaymentMethodOption): PaymentChannelOption | null {
  if (!raw || typeof raw !== 'object') return null
  const id = String(raw.id || '').trim()
  const paymentType = normalizeVisibleMethod(String(raw.payment_type || '')) || String(raw.payment_type || '').trim()
  const providerKey = String(raw.provider_key || '').trim().toLowerCase()
  if (!id || !paymentType || !providerKey) return null
  return {
    id,
    payment_type: paymentType,
    provider_key: providerKey,
    display_name: typeof raw.display_name === 'string' ? raw.display_name.trim() || undefined : undefined,
    currency: typeof raw.currency === 'string' ? raw.currency : undefined,
    fee_rate: finiteNumber(raw.fee_rate),
    daily_limit: finiteNumber(raw.daily_limit),
    single_min: finiteNumber(raw.single_min),
    single_max: finiteNumber(raw.single_max),
    amount_ranges: normalizeAmountRanges(raw.amount_ranges),
    available: raw.available !== false,
    capabilities: Array.isArray(raw.capabilities)
      ? raw.capabilities.filter((capability): capability is string => typeof capability === 'string')
      : undefined,
  }
}

function normalizeAmountRanges(
  ranges: PaymentMethodOption['amount_ranges'],
): PaymentChannelOption['amount_ranges'] {
  if (!Array.isArray(ranges) || ranges.length === 0) return undefined
  const normalized = ranges
    .filter(range => !!range && typeof range === 'object')
    .map(range => ({
      single_min: finiteNumber(range.single_min),
      single_max: finiteNumber(range.single_max),
    }))
    .filter(range => range.single_max <= 0 || range.single_min <= range.single_max)
  return normalized.length > 0 ? normalized : undefined
}

function buildLegacyOption(
  paymentType: string,
  limit: MethodLimit,
  checkout: CheckoutInfoResponse,
): PaymentChannelOption {
  const capabilities = paymentType === 'alipay' && checkout.alipay_mobile_precreate_deep_link
    ? [ALIPAY_MOBILE_PRECREATE_DEEP_LINK]
    : undefined
  return {
    id: paymentType,
    payment_type: paymentType,
    provider_key: '',
    display_name: limit.display_name,
    currency: limit.currency,
    fee_rate: finiteNumber(checkout.recharge_fee_rate ?? limit.fee_rate),
    daily_limit: finiteNumber(limit.daily_limit),
    single_min: finiteNumber(limit.single_min),
    single_max: finiteNumber(limit.single_max),
    available: limit.available !== false,
    capabilities,
    legacy: true,
  }
}

function finiteNumber(value: unknown): number {
  const numberValue = Number(value)
  return Number.isFinite(numberValue) && numberValue > 0 ? numberValue : 0
}
