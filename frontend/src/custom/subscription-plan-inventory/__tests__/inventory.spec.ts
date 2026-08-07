import { describe, expect, it } from 'vitest'

import {
  PLAN_NOT_AVAILABLE_ERROR,
  PLAN_SOLD_OUT_ERROR,
  SOLD_OUT_ACTION_DISABLE_PURCHASE,
  canListSoldOutPlan,
  inventoryQuantityValue,
  isInventoryQuantity,
  isPlanSoldOut,
  isPositiveInventoryQuantity,
  planAvailabilityError,
  reconcilePlanAvailability,
  synchronizePlanAvailability,
} from '../inventory'

describe('subscription plan inventory helpers', () => {
  it('accepts only safe positive integers', () => {
    expect(isPositiveInventoryQuantity('1')).toBe(true)
    expect(isPositiveInventoryQuantity(' 12 ')).toBe(true)
    expect(isPositiveInventoryQuantity('')).toBe(false)
    expect(isPositiveInventoryQuantity('0')).toBe(false)
    expect(isPositiveInventoryQuantity('-1')).toBe(false)
    expect(isPositiveInventoryQuantity('1.5')).toBe(false)
    expect(isPositiveInventoryQuantity('9007199254740992')).toBe(false)
  })

  it('maps blank to unlimited and detects sold-out plans', () => {
    expect(inventoryQuantityValue('')).toBeNull()
    expect(inventoryQuantityValue('7')).toBe(7)
    expect(isPlanSoldOut({ remaining_quantity: 0 })).toBe(true)
    expect(isPlanSoldOut({ remaining_quantity: null })).toBe(false)
    expect(isPlanSoldOut({ sold_out: true })).toBe(true)
    expect(isPlanSoldOut({ sold_out: false })).toBe(false)
  })

  it('allows zero only for disable-purchase plans', () => {
    expect(isInventoryQuantity('0', true)).toBe(true)
    expect(isInventoryQuantity('0', false)).toBe(false)
    expect(isInventoryQuantity('-1', true)).toBe(false)
    expect(isInventoryQuantity('1.5', true)).toBe(false)
    expect(canListSoldOutPlan({
      remaining_quantity: 0,
      sold_out_action: SOLD_OUT_ACTION_DISABLE_PURCHASE,
    })).toBe(true)
    expect(canListSoldOutPlan({ remaining_quantity: 0, sold_out_action: 'delist' })).toBe(false)
  })

  it('recognizes availability errors and applies a safe local fallback', () => {
    const plans = [
      { id: 1, sold_out: false },
      { id: 2, sold_out: false },
    ]

    expect(planAvailabilityError({ reason: PLAN_SOLD_OUT_ERROR })).toBe(PLAN_SOLD_OUT_ERROR)
    expect(planAvailabilityError({ reason: PLAN_NOT_AVAILABLE_ERROR })).toBe(PLAN_NOT_AVAILABLE_ERROR)
    expect(planAvailabilityError({ reason: 'INVALID_AMOUNT' })).toBeNull()
    expect(reconcilePlanAvailability(plans, 1, PLAN_SOLD_OUT_ERROR)).toEqual([
      { id: 1, sold_out: true },
      { id: 2, sold_out: false },
    ])
    expect(reconcilePlanAvailability(plans, 1, PLAN_NOT_AVAILABLE_ERROR)).toEqual([
      { id: 2, sold_out: false },
    ])
  })

  it('prefers refreshed plans but keeps the error-derived state if refresh fails', async () => {
    const current = [{ id: 1, sold_out: false }]
    const refreshed = [{ id: 1, sold_out: false }, { id: 2, sold_out: false }]

    await expect(synchronizePlanAvailability(
      current,
      1,
      PLAN_SOLD_OUT_ERROR,
      async () => refreshed,
    )).resolves.toEqual([
      { id: 1, sold_out: true },
      { id: 2, sold_out: false },
    ])
    await expect(synchronizePlanAvailability(
      current,
      1,
      PLAN_NOT_AVAILABLE_ERROR,
      async () => { throw new Error('refresh failed') },
    )).resolves.toEqual([])
  })
})
