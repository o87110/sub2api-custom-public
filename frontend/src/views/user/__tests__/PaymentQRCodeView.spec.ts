import { flushPromises, mount } from '@vue/test-utils'

const pollOrderStatus = vi.hoisted(() => vi.fn())
const verifyOrder = vi.hoisted(() => vi.fn())
const cancelOrder = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())
const showError = vi.hoisted(() => vi.fn())
const toCanvas = vi.hoisted(() => vi.fn())

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('vue-router', () => ({
  useRoute: () => ({
    query: {
      order_id: '42',
      qr: 'https://pay.example.com/qr/42',
      payment_type: 'easypay',
      expires_at: '2099-01-01T12:30:00Z',
    },
  }),
  useRouter: () => ({ push: routerPush }),
}))

vi.mock('@/stores/payment', () => ({
  usePaymentStore: () => ({ pollOrderStatus }),
}))

vi.mock('@/stores', () => ({
  useAppStore: () => ({ showError }),
}))

vi.mock('@/api/payment', () => ({
  paymentAPI: { cancelOrder, verifyOrder },
}))

vi.mock('qrcode', () => ({
  default: { toCanvas },
}))

import PaymentQRCodeView from '../PaymentQRCodeView.vue'

const pendingOrder = {
  id: 42,
  user_id: 9,
  amount: 88,
  pay_amount: 88,
  fee_rate: 0,
  payment_type: 'easypay',
  out_trade_no: 'sub2_easypay_qr_42',
  status: 'PENDING',
  order_type: 'balance',
  created_at: '2026-08-25T00:00:00Z',
  expires_at: '2099-01-01T12:30:00Z',
  refund_amount: 0,
}

describe('PaymentQRCodeView', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    pollOrderStatus.mockReset().mockResolvedValue(pendingOrder)
    verifyOrder.mockReset().mockResolvedValue({ data: { ...pendingOrder, status: 'COMPLETED' } })
    cancelOrder.mockReset()
    routerPush.mockReset()
    showError.mockReset()
    toCanvas.mockReset().mockResolvedValue(undefined)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('actively verifies a pending non-WeChat QR order and opens the success result', async () => {
    mount(PaymentQRCodeView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
        },
      },
    })

    await flushPromises()
    await vi.advanceTimersByTimeAsync(3000)
    await flushPromises()

    expect(pollOrderStatus).toHaveBeenCalledWith(42)
    expect(verifyOrder).toHaveBeenCalledWith('sub2_easypay_qr_42')
    expect(routerPush).toHaveBeenCalledWith({
      path: '/payment/result',
      query: { order_id: '42', status: 'success' },
    })
  })
})
