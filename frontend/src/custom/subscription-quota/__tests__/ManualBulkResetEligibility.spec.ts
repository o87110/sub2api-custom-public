import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import type { UserSubscription } from '@/types'

import ManualBulkResetAssignmentToggle from '../ManualBulkResetAssignmentToggle.vue'
import ManualBulkResetEligibilityAction from '../ManualBulkResetEligibilityAction.vue'

const { updateEligibility, showSuccess, showError } = vi.hoisted(() => ({
  updateEligibility: vi.fn(),
  showSuccess: vi.fn(),
  showError: vi.fn()
}))

vi.mock('@/api/admin/subscriptions', () => ({
  updateCurrentCycleBulkResetEligibility: updateEligibility
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showSuccess, showError })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key })
}))

const ConfirmDialogStub = defineComponent({
  props: { show: Boolean },
  emits: ['confirm', 'cancel'],
  template: '<div v-if="show"><button data-testid="confirm" @click="$emit(\'confirm\')">confirm</button></div>'
})

function subscription(overrides: Partial<UserSubscription> = {}): UserSubscription {
  return {
    id: 7,
    user_id: 1,
    group_id: 2,
    status: 'active',
    starts_at: '2026-08-01T00:00:00Z',
    expires_at: '2026-09-01T00:00:00Z',
    daily_usage_usd: 0,
    weekly_usage_usd: 0,
    monthly_usage_usd: 0,
    daily_window_start: null,
    weekly_window_start: null,
    monthly_window_start: null,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    user: { id: 1, email: 'manual@example.com' } as UserSubscription['user'],
    manual_bulk_quota_reset_enabled: false,
    manual_bulk_quota_reset_editable: true,
    ...overrides
  }
}

describe('manual subscription bulk reset eligibility controls', () => {
  beforeEach(() => {
    updateEligibility.mockReset()
    showSuccess.mockReset()
    showError.mockReset()
    updateEligibility.mockResolvedValue({ id: 7 })
  })

  it('keeps the assignment toggle off by default and emits explicit changes', async () => {
    const wrapper = mount(ManualBulkResetAssignmentToggle, { props: { modelValue: false } })
    const checkbox = wrapper.get('[data-testid="assign-bulk-reset-eligibility"]')

    expect((checkbox.element as HTMLInputElement).checked).toBe(false)
    await checkbox.setValue(true)
    expect(wrapper.emitted('update:modelValue')).toEqual([[true]])
  })

  it('updates an editable current manual cycle and emits refresh', async () => {
    const wrapper = mount(ManualBulkResetEligibilityAction, {
      props: { subscription: subscription() },
      global: { stubs: { ConfirmDialog: ConfirmDialogStub, Icon: true } }
    })

    expect(wrapper.get('[data-testid="manual-bulk-reset-eligibility-action"]').attributes('aria-pressed')).toBe('false')
    await wrapper.get('[data-testid="manual-bulk-reset-eligibility-action"]').trigger('click')
    await wrapper.get('[data-testid="confirm"]').trigger('click')
    await flushPromises()

    expect(updateEligibility).toHaveBeenCalledWith(7, true)
    expect(showSuccess).toHaveBeenCalledOnce()
    expect(wrapper.emitted('updated')).toHaveLength(1)
  })

  it('does not render an action for payment or other non-editable cycles', () => {
    const wrapper = mount(ManualBulkResetEligibilityAction, {
      props: { subscription: subscription({ manual_bulk_quota_reset_editable: false }) },
      global: { stubs: { ConfirmDialog: ConfirmDialogStub, Icon: true } }
    })

    expect(wrapper.find('[data-testid="manual-bulk-reset-eligibility-action"]').exists()).toBe(false)
  })
})
