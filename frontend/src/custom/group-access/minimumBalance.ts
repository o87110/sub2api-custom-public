import type { Group } from '@/types'

export const GROUP_MINIMUM_BALANCE_NOT_MET = 'GROUP_MINIMUM_BALANCE_NOT_MET'

export interface GroupBalanceRequirement {
  groupName: string
  minimumBalance: number
  currentBalance: number
  usableBalance: number
  balanceGap: number
  eligible: boolean
}

export function groupBalanceRequirement(group: Group | null | undefined): GroupBalanceRequirement | null {
  if (
    !group ||
    !(group.minimum_balance > 0) ||
    group.balance_eligible !== false ||
    typeof group.current_balance !== 'number'
  ) {
    return null
  }
  return {
    groupName: group.name,
    minimumBalance: group.minimum_balance,
    currentBalance: group.current_balance,
    usableBalance: group.usable_balance ?? Math.max(group.current_balance - group.minimum_balance, 0),
    balanceGap: group.balance_gap ?? Math.max(group.minimum_balance - group.current_balance, 0),
    eligible: false
  }
}

export function formatGroupBalance(value: number): string {
  if (!Number.isFinite(value)) return '$0.00'
  const absolute = Math.abs(value)
  const maximumFractionDigits = absolute > 0 && absolute < 0.01 ? 8 : 2
  return `$${value.toLocaleString('en-US', {
    minimumFractionDigits: 2,
    maximumFractionDigits
  })}`
}

interface APIErrorData {
  reason?: string
  code?: string
  metadata?: Record<string, string>
}

interface APIErrorShape extends APIErrorData {
  response?: {
    data?: APIErrorData
  }
}

function metadataNumber(metadata: Record<string, string> | undefined, key: string): number | null {
  const value = Number(metadata?.[key])
  return Number.isFinite(value) ? value : null
}

export function minimumBalanceErrorToast(error: unknown, locale: string): string | null {
  const normalizedError = error as APIErrorShape | undefined
  const data = normalizedError?.response?.data ?? normalizedError
  if ((data?.reason ?? data?.code) !== GROUP_MINIMUM_BALANCE_NOT_MET) return null

  const metadata = data?.metadata
  const groupName = metadata?.group_name || ''
  const current = metadataNumber(metadata, 'current_balance')
  const minimum = metadataNumber(metadata, 'minimum_balance')
  const gap = metadataNumber(metadata, 'balance_gap')
  if (current === null || minimum === null || gap === null) return null

  const zh = locale.toLowerCase().startsWith('zh')
  if (gap > 0) {
    return zh
      ? `无法使用“${groupName}”：当前余额 ${formatGroupBalance(current)}，余额需高于 ${formatGroupBalance(minimum)}，还需增加超过 ${formatGroupBalance(gap)}。`
      : `Cannot use “${groupName}”: current balance is ${formatGroupBalance(current)}, the balance must be greater than ${formatGroupBalance(minimum)}, and more than ${formatGroupBalance(gap)} must be added.`
  }
  return zh
    ? `无法使用“${groupName}”：当前余额刚好为 ${formatGroupBalance(current)}，必须高于 ${formatGroupBalance(minimum)}；余额增加后将自动恢复。`
    : `Cannot use “${groupName}”: the current balance is exactly ${formatGroupBalance(current)} and must be greater than ${formatGroupBalance(minimum)}. Access resumes automatically after the balance increases.`
}
