import { extractApiErrorCode } from '@/utils/apiError'

export const SOLD_OUT_ACTION_DELIST = 'delist' as const
export const SOLD_OUT_ACTION_DISABLE_PURCHASE = 'disable_purchase' as const
export const PLAN_SOLD_OUT_ERROR = 'PLAN_SOLD_OUT' as const
export const PLAN_NOT_AVAILABLE_ERROR = 'PLAN_NOT_AVAILABLE' as const

export type SoldOutAction =
  | typeof SOLD_OUT_ACTION_DELIST
  | typeof SOLD_OUT_ACTION_DISABLE_PURCHASE

export type PlanAvailabilityError =
  | typeof PLAN_SOLD_OUT_ERROR
  | typeof PLAN_NOT_AVAILABLE_ERROR

export function isInventoryQuantity(value: string, allowZero = false): boolean {
  const normalized = value.trim()
  if (allowZero && normalized === '0') return true
  if (!/^[1-9]\d*$/.test(normalized)) return false
  return Number.isSafeInteger(Number(normalized))
}

export function isPositiveInventoryQuantity(value: string): boolean {
  return isInventoryQuantity(value)
}

export function inventoryQuantityValue(value: string): number | null {
  const normalized = value.trim()
  return normalized === '' ? null : Number(normalized)
}

export function isPlanSoldOut(plan?: { sold_out?: boolean; remaining_quantity?: number | null } | null): boolean {
  return plan?.sold_out === true || plan?.remaining_quantity === 0
}

export function isPlanPurchasable(plan?: {
  sold_out?: boolean
  remaining_quantity?: number | null
  renewal_available?: boolean
} | null): boolean {
  return !!plan && (!isPlanSoldOut(plan) || plan.renewal_available === true)
}

export function canListSoldOutPlan(plan: {
  sold_out_action?: SoldOutAction
  remaining_quantity?: number | null
}): boolean {
  return plan.remaining_quantity === 0 && plan.sold_out_action === SOLD_OUT_ACTION_DISABLE_PURCHASE
}

export function planAvailabilityError(err: unknown): PlanAvailabilityError | null {
  const code = extractApiErrorCode(err)
  if (code === PLAN_SOLD_OUT_ERROR || code === PLAN_NOT_AVAILABLE_ERROR) return code
  return null
}

export function reconcilePlanAvailability<T extends { id: number; sold_out?: boolean }>(
  plans: readonly T[],
  planId: number,
  reason: PlanAvailabilityError,
): T[] {
  if (reason === PLAN_NOT_AVAILABLE_ERROR) {
    return plans.filter(plan => plan.id !== planId)
  }
  return plans.map(plan => plan.id === planId ? { ...plan, sold_out: true } : plan)
}

export async function synchronizePlanAvailability<T extends { id: number; sold_out?: boolean }>(
  plans: readonly T[],
  planId: number,
  reason: PlanAvailabilityError,
  loadPlans: () => Promise<readonly T[]>,
): Promise<T[]> {
  const fallback = reconcilePlanAvailability(plans, planId, reason)
  try {
    return reconcilePlanAvailability(await loadPlans(), planId, reason)
  } catch {
    return fallback
  }
}
