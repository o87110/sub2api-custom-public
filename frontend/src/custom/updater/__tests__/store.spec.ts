import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { checkUpdates, type VersionInfo } from '@/custom/updater/api'
import { useCustomUpdaterStore } from '@/custom/updater/store'

vi.mock('@/custom/updater/api', () => ({
  checkUpdates: vi.fn()
}))

const versionInfo: VersionInfo = {
  current_version: '0.1.162',
  current_build_version: '0.1.162-custom.6',
  latest_version: '0.1.162-custom.7',
  has_update: true,
  cached: false,
  build_type: 'release'
}

describe('custom updater store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    vi.mocked(checkUpdates).mockReset()
  })

  it('records an initial version-check failure without reporting an update result', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    vi.mocked(checkUpdates).mockRejectedValue(new Error('GitHub API unavailable'))
    const store = useCustomUpdaterStore()

    await expect(store.fetchVersion()).resolves.toBeNull()

    expect(store.versionError).toBe('GitHub API unavailable')
    expect(store.versionStale).toBe(false)
    expect(store.hasUpdate).toBe(false)
    expect(store.latestVersion).toBe('')
    consoleError.mockRestore()
  })

  it('marks retained version data as stale when a forced refresh fails', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    vi.mocked(checkUpdates)
      .mockResolvedValueOnce(versionInfo)
      .mockRejectedValueOnce(new Error('token rejected'))
    const store = useCustomUpdaterStore()

    await store.fetchVersion()
    await expect(store.fetchVersion(true)).resolves.toBeNull()

    expect(store.versionError).toBe('token rejected')
    expect(store.versionStale).toBe(true)
    expect(store.hasUpdate).toBe(true)
    expect(store.latestVersion).toBe('0.1.162-custom.7')
    consoleError.mockRestore()
  })

  it('clears the error and stale marker after a successful retry', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => undefined)
    vi.mocked(checkUpdates)
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce(versionInfo)
    const store = useCustomUpdaterStore()

    await store.fetchVersion()
    await expect(store.fetchVersion(true)).resolves.toEqual(versionInfo)

    expect(store.versionError).toBe('')
    expect(store.versionStale).toBe(false)
    expect(store.hasUpdate).toBe(true)
    consoleError.mockRestore()
  })

  it('propagates a successful cached fallback warning as stale version data', async () => {
    vi.mocked(checkUpdates).mockResolvedValue({
      ...versionInfo,
      cached: true,
      warning: 'Using cached data: GitHub API unavailable'
    })
    const store = useCustomUpdaterStore()

    await expect(store.fetchVersion()).resolves.toMatchObject({ cached: true })

    expect(store.versionError).toBe('Using cached data: GitHub API unavailable')
    expect(store.versionStale).toBe(true)
    expect(store.hasUpdate).toBe(true)
  })

  it('propagates a successful source warning without marking uncached data as stale', async () => {
    vi.mocked(checkUpdates).mockResolvedValue({
      ...versionInfo,
      cached: false,
      has_update: false,
      warning: 'GitHub API unavailable'
    })
    const store = useCustomUpdaterStore()

    await store.fetchVersion()

    expect(store.versionError).toBe('GitHub API unavailable')
    expect(store.versionStale).toBe(false)
    expect(store.hasUpdate).toBe(false)
  })
})
