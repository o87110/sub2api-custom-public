import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import type { UserMonitorView } from '@/api/channelMonitor'
import MonitorCard from '@/components/user/monitor/MonitorCard.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: ref('zh-CN'),
    t: (key: string) => key,
  }),
}))

function makeMonitor(overrides: Partial<UserMonitorView> = {}): UserMonitorView {
  return {
    id: 1,
    name: 'T1.1 特价 Plus',
    provider: 'openai',
    group_name: '特价分组',
    group_rate_multiplier: 0.18,
    group_rate_display_template: '',
    primary_model: 'gpt-5.6-luna',
    primary_status: 'operational',
    primary_latency_ms: 1200,
    primary_ping_latency_ms: 9,
    availability_7d: 100,
    extra_models: [],
    timeline: [],
    ...overrides,
  }
}

function mountCard(item: UserMonitorView) {
  return mount(MonitorCard, {
    props: {
      item,
      window: '7d',
      availabilityValue: 100,
      countdownSeconds: 30,
    },
    global: {
      stubs: {
        ProviderIcon: true,
        MonitorMetricPair: true,
        MonitorAvailabilityRow: true,
        MonitorTimeline: true,
      },
    },
  })
}

describe('MonitorCard custom group rate bridge', () => {
  it('shows the group name beside the model and the rate below status', () => {
    const wrapper = mountCard(makeMonitor())
    const groupName = wrapper.get('[data-testid="channel-monitor-group-name"]')
    const groupRate = wrapper.get('[data-testid="channel-monitor-group-rate"]')

    expect(groupName.text()).toBe('特价分组')
    expect(groupName.attributes('title')).toBe('特价分组')
    expect(groupRate.text()).toBe('0.18x')
    expect(groupRate.element.parentElement?.className).toContain('flex-col')
    expect(groupRate.element.parentElement?.className).toContain('items-end')
  })

  it('keeps the stored group label when no local group can be resolved', () => {
    const wrapper = mountCard(makeMonitor({
      group_name: '外部渠道',
      group_rate_multiplier: null,
    }))

    expect(wrapper.find('[data-testid="channel-monitor-group-rate"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('外部渠道')
  })

  it('applies the configured display template without replacing the group name', () => {
    const wrapper = mountCard(makeMonitor({
      group_name: '内部标签',
      group_rate_multiplier: 0.9,
      group_rate_display_template: '{rate}优先用',
    }))

    expect(wrapper.get('[data-testid="channel-monitor-group-name"]').text()).toBe('内部标签')
    expect(wrapper.get('[data-testid="channel-monitor-group-rate"]').text()).toBe('0.9优先用')
  })
})
