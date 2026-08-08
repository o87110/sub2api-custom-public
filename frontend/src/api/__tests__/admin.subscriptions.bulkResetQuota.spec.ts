import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

import { listBulkResetQuotaCandidates } from '@/api/admin/subscriptions'
import {
  BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS,
  bulkResetQuota
} from '@/custom/subscription-quota/bulkReset'

describe('admin subscription bulk quota reset API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('loads bulk reset candidates from the static route', async () => {
    const response = { user_count: 1, subscription_count: 1, items: [] }
    get.mockResolvedValue({ data: response })

    await expect(listBulkResetQuotaCandidates()).resolves.toEqual(response)
    expect(get).toHaveBeenCalledWith('/admin/subscriptions/bulk-reset-quota/candidates')
  })

  it('submits selected subscriptions with the required idempotency key', async () => {
    const response = {
      requested_count: 2,
      success_count: 2,
      skipped_count: 0,
      failed_count: 0,
      items: []
    }
    post.mockResolvedValue({ data: response })

    await expect(bulkResetQuota([7, 9], 'bulk-reset-key')).resolves.toEqual(response)
    expect(post).toHaveBeenCalledWith(
      '/admin/subscriptions/bulk-reset-quota',
      { subscription_ids: [7, 9] },
      { headers: { 'Idempotency-Key': 'bulk-reset-key' } }
    )
  })

  it('rejects an oversized batch before sending the request', async () => {
    const ids = Array.from(
      { length: BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS + 1 },
      (_, index) => index + 1
    )

    await expect(bulkResetQuota(ids, 'oversized-bulk-reset')).rejects.toBeInstanceOf(RangeError)
    expect(post).not.toHaveBeenCalled()
  })
})
