import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'

import type { MonitorHealth, MonitorMetric } from '@/api/channelMonitorV2'
import RelayPulseMatrix from '@/features/channel-monitor-v2/RelayPulseMatrix.vue'
import {
  matrixWheelZoomHint,
  resolveMatrixWheelZoomTrack,
} from './matrixWheelPolicy'

const locale = ref('zh-CN')

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale,
      t: (key: string) => key,
    }),
  }
})

const health: MonitorHealth = {
  overall: 'healthy',
  error_rate: 'healthy',
  ttft: 'healthy',
  cache: 'healthy',
  score: 100,
  error_rate_score: 100,
  ttft_score: 100,
  cache_score: 100,
  minimum_sample: 20,
}

function metrics(): MonitorMetric {
  return {
    success_requests: 10,
    error_requests: 0,
    request_count: 10,
    token_count: 100,
    rpm: 2,
    tpm: 60,
    error_rate: 0,
    cache_rate: 1,
    cache_rate_numerator: 100,
    cache_rate_denominator: 100,
    ttft: { sample_count: 10, p50_ms: 100, p95_ms: 300, avg_ms: 150 },
    duration: { sample_count: 10, p50_ms: 500, p95_ms: 900, avg_ms: 600 },
    upstream_affected_requests: 0,
    upstream_attempt_count: 10,
  }
}

function mountMatrix() {
  return mount(RelayPulseMatrix, {
    props: {
      rows: [{
        platform: 'anthropic',
        group_id: 1,
        group_name: 'default',
        model: 'claude',
        metrics: metrics(),
        health,
        buckets: [{
          bucket_start: '2026-08-01T00:00:00Z',
          metrics: metrics(),
          health,
        }],
      }],
      coverage: {
        requested_start: '2026-08-01T00:00:00Z',
        requested_end: '2026-08-01T00:10:00Z',
        coverage_start: '2026-08-01T00:00:00Z',
        data_through: '2026-08-01T00:10:00Z',
        computed_at: '2026-08-01T00:10:00Z',
        aggregation_lag_seconds: 0,
        coverage_complete: true,
        bucket_seconds: 60,
      },
      healthMode: 'overall' as const,
    },
  })
}

function dispatchWheel(element: Element, init: WheelEventInit = {}): WheelEvent {
  const event = new WheelEvent('wheel', {
    bubbles: true,
    cancelable: true,
    deltaY: -100,
    ...init,
  })
  element.dispatchEvent(event)
  return event
}

describe('matrix wheel policy', () => {
  it('requires Alt/Option and a pulse-track target', () => {
    const track = document.createElement('div')
    track.className = 'pulse-track'
    const cell = document.createElement('span')
    track.appendChild(cell)
    const outside = document.createElement('div')

    expect(resolveMatrixWheelZoomTrack({ altKey: false, target: cell })).toBeNull()
    expect(resolveMatrixWheelZoomTrack({ altKey: true, target: outside })).toBeNull()
    expect(resolveMatrixWheelZoomTrack({ altKey: true, target: cell })).toBe(track)
  })

  it('safely ignores non-element event targets', () => {
    expect(resolveMatrixWheelZoomTrack({ altKey: true, target: null })).toBeNull()
    expect(resolveMatrixWheelZoomTrack({
      altKey: true,
      target: document.createTextNode('text'),
    })).toBeNull()
  })

  it('provides Chinese and English gesture hints', () => {
    expect(matrixWheelZoomHint('zh-CN')).toBe(
      '按住 Alt/Option，在色块上滚轮缩放（区间变窄、色块变宽）',
    )
    expect(matrixWheelZoomHint('en-US')).toBe(
      'Hold Alt/Option and scroll over a pulse block to zoom (narrower range, wider blocks)',
    )
  })
})

describe('RelayPulseMatrix custom wheel bridge', () => {
  it('leaves plain wheel events native across the matrix', async () => {
    const wrapper = mountMatrix()
    const reset = wrapper.get('button')

    for (const selector of ['.dimension-cell', '.summary-value', '.pulse-cell']) {
      const event = dispatchWheel(wrapper.get(selector).element)
      expect(event.defaultPrevented).toBe(false)
    }

    await nextTick()
    expect(reset.attributes('disabled')).toBeDefined()
  })

  it('ignores Alt/Option wheel outside the pulse track', async () => {
    const wrapper = mountMatrix()
    const event = dispatchWheel(wrapper.get('.summary-value').element, { altKey: true })

    expect(event.defaultPrevented).toBe(false)
    await nextTick()
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })

  it('zooms only with Alt/Option over a pulse block and can reset', async () => {
    const wrapper = mountMatrix()
    const event = dispatchWheel(wrapper.get('.pulse-cell').element, { altKey: true })

    expect(event.defaultPrevented).toBe(true)
    await nextTick()
    expect(wrapper.get('button').attributes('disabled')).toBeUndefined()

    await wrapper.get('button').trigger('click')
    expect(wrapper.get('button').attributes('disabled')).toBeDefined()
  })
})
