import { describe, expect, it } from 'vitest'
import {
  isPaymentAmountRepresentable,
  multiplyAndRoundPaymentAmount,
  paymentCurrencyFractionDigits,
  paymentFeeAmount,
} from './paymentMoney'

describe('payment money precision', () => {
  it('rounds decimal subscription conversion like the backend', () => {
    expect(multiplyAndRoundPaymentAmount(1.14, 7.25, 'CNY')).toBe(8.27)
  })

  it('ceil-rounds fees without binary floating-point overcharge', () => {
    expect(paymentFeeAmount(1, 7, 'CNY')).toBe(0.07)
  })

  it('uses each currency minor-unit precision', () => {
    expect(paymentCurrencyFractionDigits('JPY')).toBe(0)
    expect(paymentCurrencyFractionDigits('CNY')).toBe(2)
    expect(paymentCurrencyFractionDigits('KWD')).toBe(3)
    expect(paymentCurrencyFractionDigits('IQD')).toBe(3)
    expect(isPaymentAmountRepresentable(10.5, 'JPY')).toBe(false)
    expect(isPaymentAmountRepresentable(1.001, 'KWD')).toBe(true)
  })
})
