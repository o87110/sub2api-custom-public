import type { ProviderInstance } from '@/types/payment'
import { normalizeVisibleMethod } from '@/components/payment/paymentFlow'
import { parseEasyPayCustomMethods } from '@/components/payment/providerConfig'
import { sortPaymentChannelOptions, type PaymentChannelOption } from './paymentChannels'

export interface AdminPaymentChannel {
  id: string
  paymentType: string
  providerKey: string
  displayName?: string
  enabled: boolean
  instanceCount: number
}

interface MutableAdminPaymentChannel extends AdminPaymentChannel {
  displayNames: Set<string>
}

export function stablePaymentChannelID(paymentType: string, providerKey: string): string {
  const method = normalizeVisibleMethod(paymentType) || paymentType.trim().toLowerCase()
  const provider = providerKey.trim().toLowerCase()
  if (method === 'alipay' && provider === 'easypay') return 'easypay_alipay'
  if (method === 'alipay' && provider === 'alipay') return 'official_alipay'
  if (method === 'wxpay' && provider === 'easypay') return 'easypay_wxpay'
  if (method === 'wxpay' && provider === 'wxpay') return 'official_wxpay'
  if (!provider || provider === method) return method
  return `${provider}_${method}`
}

export function aggregateAdminPaymentChannels(
  providers: ProviderInstance[],
): AdminPaymentChannel[] {
  const grouped = new Map<string, MutableAdminPaymentChannel>()

  for (const provider of providers) {
    const providerKey = String(provider.provider_key || '').trim().toLowerCase()
    if (!providerKey) continue
    const displayNames = easyPayDisplayNames(provider)
    for (const paymentType of providerPaymentTypes(provider)) {
      const id = stablePaymentChannelID(paymentType, providerKey)
      if (!id) continue
      const displayName = displayNames.get(paymentType)
      const existing = grouped.get(id)
      if (existing) {
        existing.enabled ||= provider.enabled
        existing.instanceCount += 1
        if (displayName) existing.displayNames.add(displayName)
        continue
      }
      grouped.set(id, {
        id,
        paymentType,
        providerKey,
        displayName,
        enabled: provider.enabled,
        instanceCount: 1,
        displayNames: new Set(displayName ? [displayName] : []),
      })
    }
  }

  const channels = [...grouped.values()].map(({ displayNames, ...channel }) => ({
    ...channel,
    displayName: displayNames.size === 1 ? [...displayNames][0] : undefined,
  }))
  const byID = new Map(channels.map(channel => [channel.id, channel]))
  return sortPaymentChannelOptions(
    channels.map<PaymentChannelOption>(channel => ({
      id: channel.id,
      payment_type: channel.paymentType,
      provider_key: channel.providerKey,
      display_name: channel.displayName,
      fee_rate: 0,
      daily_limit: 0,
      single_min: 0,
      single_max: 0,
      available: channel.enabled,
    })),
  ).map(option => byID.get(option.id)!)
}

function providerPaymentTypes(provider: ProviderInstance): string[] {
  const providerKey = String(provider.provider_key || '').trim().toLowerCase()
  if (providerKey === 'stripe') return ['stripe']

  const rawTypes = Array.isArray(provider.supported_types)
    ? provider.supported_types
    : []
  if (rawTypes.length === 0) {
    if (providerKey === 'alipay') return ['alipay']
    if (providerKey === 'wxpay') return ['wxpay']
    if (providerKey === 'airwallex') return ['airwallex']
    return []
  }

  const result: string[] = []
  const seen = new Set<string>()
  for (const rawType of rawTypes) {
    const paymentType = normalizeVisibleMethod(rawType) || String(rawType || '').trim().toLowerCase()
    if (!paymentType || seen.has(paymentType)) continue
    if (providerKey === 'alipay' && paymentType !== 'alipay') continue
    if (providerKey === 'wxpay' && paymentType !== 'wxpay') continue
    seen.add(paymentType)
    result.push(paymentType)
  }
  return result
}

function easyPayDisplayNames(provider: ProviderInstance): Map<string, string> {
  if (String(provider.provider_key || '').trim().toLowerCase() !== 'easypay') {
    return new Map()
  }
  return new Map(
    parseEasyPayCustomMethods(provider.config?.customMethods)
      .map(method => [
        normalizeVisibleMethod(method.type) || method.type.trim().toLowerCase(),
        method.displayName.trim(),
      ] as const)
      .filter((entry): entry is readonly [string, string] => !!entry[0] && !!entry[1]),
  )
}
