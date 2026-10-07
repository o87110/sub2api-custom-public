import { ref, type Ref } from 'vue'

import type { PaymentChannelOption } from './paymentChannels'
import { isEasyPayAlipayChannel } from './paymentChannels'
import type { OrderType } from '@/types/payment'

interface PendingAlipayOrder {
  amount: number
  displayAmount: number
  orderType: OrderType
  planId?: number
}

interface UseAlipayPaymentWarningOptions {
  selectedChannel: Ref<PaymentChannelOption | null | undefined>
  createOrder: (amount: number, orderType: OrderType, planId?: number) => Promise<void>
}

export function useAlipayPaymentWarning(options: UseAlipayPaymentWarningOptions) {
  const showAlipayWarningDialog = ref(false)
  const pendingAlipayOrder = ref<PendingAlipayOrder | null>(null)

  function requestAlipayWarning(
    amount: number,
    displayAmount: number,
    orderType: OrderType,
    planId?: number,
  ): boolean {
    const channel = options.selectedChannel.value
    if (!isEasyPayAlipayChannel(channel ?? undefined) || channel?.payment_notice_enabled === false) {
      return false
    }
    pendingAlipayOrder.value = { amount, displayAmount, orderType, planId }
    showAlipayWarningDialog.value = true
    return true
  }

  function cancelAlipayWarning() {
    showAlipayWarningDialog.value = false
    pendingAlipayOrder.value = null
  }

  async function confirmAlipayWarning() {
    const pending = pendingAlipayOrder.value
    cancelAlipayWarning()
    if (!pending) return
    await options.createOrder(pending.amount, pending.orderType, pending.planId)
  }

  return {
    showAlipayWarningDialog,
    pendingAlipayOrder,
    requestAlipayWarning,
    cancelAlipayWarning,
    confirmAlipayWarning,
  }
}
