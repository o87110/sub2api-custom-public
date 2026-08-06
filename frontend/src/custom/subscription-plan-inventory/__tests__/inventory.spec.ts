import { describe, expect, it } from 'vitest'

import {
  inventoryQuantityValue,
  isPlanSoldOut,
  isPositiveInventoryQuantity,
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
  })
})
