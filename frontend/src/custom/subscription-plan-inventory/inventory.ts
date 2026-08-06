export function isPositiveInventoryQuantity(value: string): boolean {
  const normalized = value.trim()
  if (!/^[1-9]\d*$/.test(normalized)) return false
  return Number.isSafeInteger(Number(normalized))
}

export function inventoryQuantityValue(value: string): number | null {
  const normalized = value.trim()
  return normalized === '' ? null : Number(normalized)
}

export function isPlanSoldOut(plan: { remaining_quantity?: number | null }): boolean {
  return plan.remaining_quantity === 0
}
