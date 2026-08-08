import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post } = vi.hoisted(() => ({ post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post } }))

import { resetQuota } from '@/api/admin/subscriptions'

describe('admin subscription reset quota API', () => {
  const options = { daily: true, weekly: true, monthly: true }

  beforeEach(() => {
    post.mockReset()
    post.mockResolvedValue({ data: { id: 7 } })
  })

  it('携带调用方提供的幂等键', async () => {
    await resetQuota(7, options, 'reset-subscription-7')

    expect(post).toHaveBeenCalledWith('/admin/subscriptions/7/reset-quota', options, {
      headers: { 'Idempotency-Key': 'reset-subscription-7' }
    })
  })

  it('未提供幂等键时保持旧调用兼容', async () => {
    await resetQuota(7, options)

    expect(post).toHaveBeenCalledWith('/admin/subscriptions/7/reset-quota', options, undefined)
  })
})
