import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'

import BulkQuotaResetDialog from '../BulkQuotaResetDialog.vue'

const { listCandidates, bulkReset } = vi.hoisted(() => ({
  listCandidates: vi.fn(),
  bulkReset: vi.fn()
}))

vi.mock('@/api/admin/subscriptions', () => ({
  BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS: 300,
  listBulkResetQuotaCandidates: listCandidates,
  bulkResetQuota: bulkReset
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) =>
      params ? `${key}:${JSON.stringify(params)}` : key
  })
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: Boolean,
    title: String,
    closeOnEscape: Boolean,
    showCloseButton: Boolean
  },
  emits: ['close'],
  template: '<div v-if="show"><slot /><footer><slot name="footer" /></footer></div>'
})

const candidates = {
  user_count: 2,
  subscription_count: 2,
  items: [
    {
      subscription_id: 11,
      user_id: 1,
      user_email: 'one@example.com',
      username: 'one',
      plan_id: 101,
      plan_name: 'Plan A',
      cycle_usage_usd: 12.34,
      manual_quota_reset_count: 2
    },
    {
      subscription_id: 12,
      user_id: 2,
      user_email: 'two@example.com',
      username: 'two',
      plan_id: 102,
      plan_name: 'Plan B',
      cycle_usage_usd: 5,
      manual_quota_reset_count: 0
    }
  ]
}

function mountDialog() {
  return mount(BulkQuotaResetDialog, {
    props: { show: true },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Icon: true
      }
    }
  })
}

describe('BulkQuotaResetDialog', () => {
  beforeEach(() => {
    listCandidates.mockReset()
    bulkReset.mockReset()
    listCandidates.mockResolvedValue(candidates)
  })

  it('defaults to all selected and supports partial and empty selection', async () => {
    const wrapper = mountDialog()
    await flushPromises()

    const selectAll = wrapper.get('#bulk-reset-select-all').element as HTMLInputElement
    expect(selectAll.checked).toBe(true)
    expect(wrapper.text()).toContain('$12.34')

    await wrapper.get('#bulk-reset-subscription-11').setValue(false)
    expect((wrapper.get('#bulk-reset-select-all').element as HTMLInputElement).indeterminate).toBe(true)

    await wrapper.get('#bulk-reset-subscription-12').setValue(false)
    expect(wrapper.get('[data-testid="bulk-reset-submit"]').attributes('disabled')).toBeDefined()
  })

  it('submits only selected subscriptions and renders per-item results', async () => {
    bulkReset.mockResolvedValue({
      requested_count: 1,
      success_count: 0,
      skipped_count: 1,
      failed_count: 0,
      items: [
        {
          subscription_id: 12,
          status: 'skipped',
          reason_code: 'SUBSCRIPTION_NO_LONGER_ELIGIBLE'
        }
      ]
    })
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('#bulk-reset-subscription-11').setValue(false)
    await wrapper.get('[data-testid="bulk-reset-submit"]').trigger('click')
    await flushPromises()

    expect(bulkReset).toHaveBeenCalledOnce()
    expect(bulkReset.mock.calls[0][0]).toEqual([12])
    expect(typeof bulkReset.mock.calls[0][1]).toBe('string')
    expect(wrapper.find('[data-testid="bulk-reset-result"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.subscriptions.bulkReset.noLongerEligible')
  })

  it('shows the plan eligibility hint when there are no candidates', async () => {
    listCandidates.mockResolvedValue({ user_count: 0, subscription_count: 0, items: [] })
    const wrapper = mountDialog()
    await flushPromises()

    expect(wrapper.find('[data-testid="bulk-reset-empty"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.subscriptions.bulkReset.emptyHint')
  })

  it('selects at most 300 candidates and explains how to process the remainder', async () => {
    const items = Array.from({ length: 301 }, (_, index) => ({
      ...candidates.items[0],
      subscription_id: index + 1,
      user_id: index + 1,
      user_email: `user-${index + 1}@example.com`
    }))
    listCandidates.mockResolvedValue({
      user_count: items.length,
      subscription_count: items.length,
      items
    })

    const wrapper = mountDialog()
    await flushPromises()

    const selectedRows = wrapper
      .findAll('input[id^="bulk-reset-subscription-"]')
      .filter(input => (input.element as HTMLInputElement).checked)
    expect(selectedRows).toHaveLength(300)
    expect(wrapper.get('#bulk-reset-subscription-301').attributes('disabled')).toBeDefined()
    expect(wrapper.find('[data-testid="bulk-reset-limit-hint"]').exists()).toBe(true)
  })

  it('retries an unknown submission with the same selection and idempotency key', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => undefined)
    bulkReset
      .mockRejectedValueOnce(new Error('network timeout'))
      .mockResolvedValueOnce({
        requested_count: 1,
        success_count: 1,
        skipped_count: 0,
        failed_count: 0,
        items: [{ subscription_id: 12, status: 'success' }]
      })
    const wrapper = mountDialog()
    await flushPromises()

    await wrapper.get('#bulk-reset-subscription-11').setValue(false)
    await wrapper.get('[data-testid="bulk-reset-submit"]').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-testid="bulk-reset-submit-error"]').exists()).toBe(true)
    expect(wrapper.get('#bulk-reset-subscription-12').attributes('disabled')).toBeDefined()
    const firstKey = bulkReset.mock.calls[0][1]

    await wrapper.get('[data-testid="bulk-reset-submit"]').trigger('click')
    await flushPromises()

    expect(bulkReset.mock.calls[1][0]).toEqual([12])
    expect(bulkReset.mock.calls[1][1]).toBe(firstKey)
    expect(wrapper.find('[data-testid="bulk-reset-result"]').exists()).toBe(true)
  })
})
