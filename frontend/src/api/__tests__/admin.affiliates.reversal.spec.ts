import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))

vi.mock('@/api/client', () => ({ apiClient: { get, post } }))

import {
  listRebateRecords,
  previewRebateReversal,
  reverseRebates,
} from '@/api/admin/affiliates'

describe('admin affiliate reversal API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('forwards the admin rebate status filter', async () => {
    get.mockResolvedValue({ data: { items: [], total: 0, page: 1, page_size: 20, pages: 1 } })

    await listRebateRecords({ rebate_status: 'reversed' })

    expect(get).toHaveBeenCalledWith('/admin/affiliates/rebates', {
      params: expect.objectContaining({ rebate_status: 'reversed' }),
    })
  })

  it('previews and submits an atomic reversal with its idempotency key', async () => {
    const preview = { preview_token: 'token', order_count: 2, inviters: [] }
    const result = { reversed_count: 2, orders: [], inviters: [] }
    post.mockResolvedValueOnce({ data: preview }).mockResolvedValueOnce({ data: result })

    await expect(previewRebateReversal([11, 12])).resolves.toEqual(preview)
    expect(post).toHaveBeenNthCalledWith(
      1,
      '/admin/affiliates/rebates/reversal-preview',
      { order_ids: [11, 12] },
    )

    const payload = {
      order_ids: [11, 12],
      preview_token: 'token',
      reason: 'test correction',
      confirm_negative_balance: true,
    }
    await expect(reverseRebates(payload, 'stable-key')).resolves.toEqual(result)
    expect(post).toHaveBeenNthCalledWith(
      2,
      '/admin/affiliates/rebates/reverse',
      payload,
      { headers: { 'Idempotency-Key': 'stable-key' } },
    )
  })
})
