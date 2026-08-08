import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import SubscriptionCycleStats from '../SubscriptionCycleStats.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (_key: string, values: { usage: string; count: number }) =>
      `本周期累计 $${values.usage} · 已重置 ${values.count} 次`
  })
}))

describe('SubscriptionCycleStats', () => {
  it('未重置时隐藏整行', () => {
    const wrapper = mount(SubscriptionCycleStats, {
      props: { cycleUsageUsd: 12.34, manualQuotaResetCount: 0 }
    })

    expect(wrapper.find('[data-testid="subscription-cycle-stats"]').exists()).toBe(false)
  })

  it('有重置记录时展示本周期累计和次数', () => {
    const wrapper = mount(SubscriptionCycleStats, {
      props: { cycleUsageUsd: 12.345, manualQuotaResetCount: 2 }
    })

    expect(wrapper.text()).toBe('本周期累计 $12.35 · 已重置 2 次')
  })
})
