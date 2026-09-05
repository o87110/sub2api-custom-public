import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import SubscriptionsView from '../SubscriptionsView.vue'

const { getMySubscriptions, routerPush, showError } = vi.hoisted(() => ({
  getMySubscriptions: vi.fn(),
  routerPush: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/subscriptions', () => ({
  default: { getMySubscriptions },
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError, cachedPublicSettings: null }),
}))

function subscription(id: number, status: string, groupId: number) {
  return {
    id,
    user_id: 1,
    group_id: groupId,
    starts_at: '2026-08-01T00:00:00Z',
    expires_at: '2026-09-01T00:00:00Z',
    status,
    daily_usage_usd: 0,
    weekly_usage_usd: 0,
    monthly_usage_usd: 0,
    cycle_usage_usd: 0,
    manual_quota_reset_count: 0,
    created_at: '2026-08-01T00:00:00Z',
    updated_at: '2026-08-01T00:00:00Z',
    group: {
      id: groupId,
      name: `Group ${groupId}`,
      platform: 'openai',
      rate_multiplier: 1,
      daily_limit_usd: null,
      weekly_limit_usd: null,
      monthly_limit_usd: null,
    },
  }
}

describe('SubscriptionsView renewal links', () => {
  beforeEach(() => {
    routerPush.mockReset()
    showError.mockReset()
    getMySubscriptions.mockResolvedValue([
      subscription(1, 'active', 10),
      subscription(2, 'expired', 20),
      subscription(3, 'suspended', 30),
    ])
  })

  it('shows renewal for active and expired subscriptions but not suspended ones', async () => {
    const wrapper = mount(SubscriptionsView, {
      global: {
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          Icon: true,
          SubscriptionCycleStats: true,
        },
      },
    })
    await flushPromises()

    const renewalButtons = wrapper.findAll('button').filter(button => button.text() === 'payment.renewNow')
    expect(renewalButtons).toHaveLength(2)
    await renewalButtons[1].trigger('click')
    expect(routerPush).toHaveBeenCalledWith({
      path: '/purchase',
      query: { tab: 'subscription', group: '20' },
    })
  })
})
