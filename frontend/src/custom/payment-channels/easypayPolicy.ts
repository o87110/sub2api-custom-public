export type EasyPayCustomTypeConflict = 'allowed' | 'exact_reserved' | 'prefix_reserved'

const EASY_PAY_EXACT_RESERVED_TYPES = new Set(['alipay', 'wxpay', 'stripe', 'card', 'link'])

/**
 * Classifies a canonical EasyPay custom payment type. Syntax validation stays
 * in the form layer; this helper owns only cross-provider name conflicts.
 */
export function classifyEasyPayCustomType(value: string): EasyPayCustomTypeConflict {
  const type = value.trim().toLowerCase()
  if (EASY_PAY_EXACT_RESERVED_TYPES.has(type)) return 'exact_reserved'
  if (type.startsWith('alipay') || type.startsWith('wxpay')) return 'prefix_reserved'
  return 'allowed'
}
