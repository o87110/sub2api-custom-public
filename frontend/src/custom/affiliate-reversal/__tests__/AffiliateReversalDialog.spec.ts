import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import AffiliateReversalDialog from '../AffiliateReversalDialog.vue'

const { previewRebateReversal, reverseRebates } = vi.hoisted(() => ({
  previewRebateReversal: vi.fn(),
  reverseRebates: vi.fn(),
}))

vi.mock('@/api/admin/affiliates', () => ({
  affiliatesAPI: {
    previewRebateReversal,
    reverseRebates,
  },
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key,
  }),
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: { show: Boolean, title: String },
  emits: ['close'],
  template: '<div v-if="show"><slot /><footer><slot name="footer" /></footer></div>',
})

const preview = {
  preview_token: 'preview-token',
  order_count: 2,
  total_rebate_amount: 12,
  total_balance_deducted: 5,
  negative_balance_users: 1,
  has_negative_balance: true,
  orders: [],
  inviters: [{
    inviter_id: 7,
    inviter_email: 'inviter@example.com',
    inviter_username: 'inviter',
    order_count: 2,
    total_rebate_amount: 12,
    frozen_quota_deducted: 2,
    available_quota_deducted: 5,
    balance_deducted: 5,
    balance_before: 1,
    balance_after: -4,
    history_quota_before: 20,
    history_quota_after: 8,
    total_recharged_before: 10,
    total_recharged_after: 5,
    will_be_negative: true,
  }],
}

function mountDialog() {
  return mount(AffiliateReversalDialog, {
    props: { show: true, orderIds: [101, 102] },
    global: { stubs: { BaseDialog: BaseDialogStub } },
  })
}

describe('AffiliateReversalDialog', () => {
  beforeEach(() => {
    previewRebateReversal.mockReset()
    reverseRebates.mockReset()
    previewRebateReversal.mockResolvedValue(preview)
  })

  it('requires a reason and explicit negative-balance confirmation', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const submit = wrapper.get('[data-test="submit-reversal"]')
    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="reversal-reason"]').setValue('test correction')
    expect(submit.attributes('disabled')).toBeDefined()

    await wrapper.get('[data-test="confirm-negative-balance"]').setValue(true)
    expect(submit.attributes('disabled')).toBeUndefined()
  })

  it('submits the preview token with a stable idempotency key', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    reverseRebates
      .mockRejectedValueOnce(new Error('network timeout'))
      .mockResolvedValueOnce({
        reversed_count: 2,
        total_rebate_amount: 12,
        total_balance_deducted: 5,
        negative_balance_users: 1,
        orders: [],
        inviters: preview.inviters,
      })
    const wrapper = mountDialog()
    await flushPromises()
    await wrapper.get('[data-test="reversal-reason"]').setValue('test correction')
    await wrapper.get('[data-test="confirm-negative-balance"]').setValue(true)

    await wrapper.get('[data-test="submit-reversal"]').trigger('click')
    await flushPromises()
    expect(wrapper.find('[data-test="submit-error"]').exists()).toBe(true)
    const firstKey = reverseRebates.mock.calls[0][1]
    expect(reverseRebates.mock.calls[0][0]).toEqual({
      order_ids: [101, 102],
      preview_token: 'preview-token',
      reason: 'test correction',
      confirm_negative_balance: true,
    })

    await wrapper.get('[data-test="submit-reversal"]').trigger('click')
    await flushPromises()
    expect(reverseRebates.mock.calls[1][1]).toBe(firstKey)
    expect(wrapper.emitted('completed')).toHaveLength(1)
  })
})
