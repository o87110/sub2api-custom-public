import { paymentAPI } from '@/api/payment'
import type { PaymentOrder } from '@/types/payment'

const VERIFY_RETRY_INTERVAL_MS = 15_000
const VERIFY_RETRY_MAX_ATTEMPTS = 6

/**
 * Provides a throttled, per-payment-page upstream reconciliation helper.
 * The backend remains the source of truth; this only accelerates recovery when
 * a provider webhook is delayed or lost.
 */
export function createPaymentOrderRecovery() {
  let verifyAttempts = 0
  let lastVerifyAt = 0

  async function recoverPendingOrder(order: PaymentOrder): Promise<PaymentOrder> {
    const outTradeNo = String(order.out_trade_no || '').trim()
    const status = String(order.status || '').trim().toUpperCase()
    if (!outTradeNo || status !== 'PENDING') return order

    const now = Date.now()
    if (verifyAttempts >= VERIFY_RETRY_MAX_ATTEMPTS || now - lastVerifyAt < VERIFY_RETRY_INTERVAL_MS) {
      return order
    }

    lastVerifyAt = now
    verifyAttempts += 1
    try {
      const result = await paymentAPI.verifyOrder(outTradeNo)
      return result.data ?? order
    } catch {
      return order
    }
  }

  function reset() {
    verifyAttempts = 0
    lastVerifyAt = 0
  }

  return { recoverPendingOrder, reset }
}
