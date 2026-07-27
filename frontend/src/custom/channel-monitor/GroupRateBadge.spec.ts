import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import GroupRateBadge from './GroupRateBadge.vue'
import { formatGroupRateMultiplier } from './groupRate'

const locale = ref('zh-CN')

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ locale }),
}))

describe('GroupRateBadge', () => {
  it.each([
    [0, '0'],
    [0.18, '0.18'],
    [1.23456, '1.2346'],
    [2.5, '2.5'],
    [-0, '0'],
  ])('formats %s without unnecessary trailing zeroes', (rate, expected) => {
    expect(formatGroupRateMultiplier(rate)).toBe(expected)
  })

  it('renders an accessible compact badge in Chinese and English', async () => {
    const wrapper = mount(GroupRateBadge, { props: { rate: 0.18 } })
    const badge = wrapper.get('[data-testid="channel-monitor-group-rate"]')

    expect(badge.text()).toBe('0.18x')
    expect(badge.attributes('aria-label')).toBe('分组默认倍率：0.18x')
    expect(badge.classes()).toEqual(expect.arrayContaining([
      'text-xs',
      'tabular-nums',
      'dark:text-gray-200',
    ]))

    locale.value = 'en'
    await wrapper.vm.$nextTick()
    expect(badge.attributes('aria-label')).toBe('Default group rate: 0.18x')
  })

  it('does not render an invalid numeric value', () => {
    const wrapper = mount(GroupRateBadge, { props: { rate: Number.NaN } })
    expect(wrapper.find('[data-testid="channel-monitor-group-rate"]').exists()).toBe(false)
  })
})
