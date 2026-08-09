import { beforeEach, describe, expect, it, vi } from 'vitest'

const { post, put } = vi.hoisted(() => ({ post: vi.fn(), put: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { post, put } }))

import {
  assign,
  bulkAssign,
  updateCurrentCycleBulkResetEligibility
} from '@/api/admin/subscriptions'

describe('admin manual subscription bulk reset eligibility API', () => {
  beforeEach(() => {
    post.mockReset()
    put.mockReset()
    post.mockResolvedValue({ data: { id: 7 } })
    put.mockResolvedValue({ data: { id: 7, manual_bulk_quota_reset_enabled: true } })
  })

  it('submits eligibility for single and bulk manual assignments', async () => {
    await assign({
      user_id: 1,
      group_id: 2,
      validity_days: 30,
      allow_bulk_quota_reset: true
    })
    await bulkAssign({
      user_ids: [1, 3],
      group_id: 2,
      validity_days: 30,
      allow_bulk_quota_reset: true
    })

    expect(post).toHaveBeenNthCalledWith(1, '/admin/subscriptions/assign', {
      user_id: 1,
      group_id: 2,
      validity_days: 30,
      allow_bulk_quota_reset: true
    })
    expect(post).toHaveBeenNthCalledWith(2, '/admin/subscriptions/bulk-assign', {
      user_ids: [1, 3],
      group_id: 2,
      validity_days: 30,
      allow_bulk_quota_reset: true
    })
  })

  it('updates only the current cycle eligibility', async () => {
    await updateCurrentCycleBulkResetEligibility(7, true)

    expect(put).toHaveBeenCalledWith(
      '/admin/subscriptions/7/current-cycle-bulk-reset-eligibility',
      { enabled: true }
    )
  })
})
