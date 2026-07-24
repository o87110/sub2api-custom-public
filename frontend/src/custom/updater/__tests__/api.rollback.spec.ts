import { beforeEach, describe, expect, it, vi } from 'vitest'

const { get, post } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: {
    get,
    post,
  },
}))

import { getRollbackVersions, rollback, type RollbackVersionInfo } from '@/custom/updater/api'

describe('custom updater rollback API', () => {
  beforeEach(() => {
    get.mockReset()
    post.mockReset()
  })

  it('getRollbackVersions fetches the rollback version list', async () => {
    const versions: RollbackVersionInfo[] = [
      {
        version: '0.1.160-custom.1',
        name: 'Sub2API v0.1.160 custom.1',
        body: 'Custom maintenance release',
        published_at: '2026-07-07T00:00:00Z',
        html_url: 'https://github.com/o87110/sub2api-custom-public/releases/tag/v0.1.160-custom.1'
      }
    ]
    get.mockResolvedValue({ data: { versions } })

    const result = await getRollbackVersions()

    expect(get).toHaveBeenCalledWith('/admin/system/rollback-versions')
    expect(result.versions).toEqual(versions)
  })

  it('rollback posts the target version in the request body', async () => {
    post.mockResolvedValue({ data: { message: 'ok', need_restart: true } })

    const result = await rollback('0.1.146-custom.1')

    expect(post).toHaveBeenCalledWith(
      '/admin/system/rollback',
      { version: '0.1.146-custom.1' },
      { timeout: 15 * 60 * 1000 }
    )
    expect(result.need_restart).toBe(true)
  })
})
