import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import VersionBadge from '../VersionBadge.vue'

const { clearVersionCache, fetchVersion, performUpdate, updaterStore } = vi.hoisted(() => {
  const fetchVersion = vi.fn()
  const clearVersionCache = vi.fn()
  const performUpdate = vi.fn()

  return {
    clearVersionCache,
    fetchVersion,
    performUpdate,
    updaterStore: {
      versionLoading: false,
      versionError: '',
      currentVersion: '0.1.162',
      currentBuildVersion: '0.1.162-custom.6',
      latestVersion: '0.1.162-custom.7',
      hasUpdate: true,
      releaseInfo: null,
      buildType: 'release',
      updateRepository: 'o87110/sub2api-custom-public',
      officialRepository: 'Wei-Shaw/sub2api',
      officialLatestVersion: '0.1.162',
      hasOfficialUpdate: false,
      officialReleaseInfo: null,
      officialReleaseWarning: '',
      fetchVersion,
      clearVersionCache
    }
  }
})

vi.mock('@/stores', () => ({
  useAuthStore: () => ({ isAdmin: true })
}))

vi.mock('@/custom/updater/store', () => ({
  useCustomUpdaterStore: () => updaterStore
}))

vi.mock('@/custom/updater/api', () => ({
  performUpdate,
  restartService: vi.fn(),
  getRollbackVersions: vi.fn(),
  rollback: vi.fn()
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: { value: 'en' },
    t: (key: string) => key
  })
}))

function findButton(text: string, wrapper: ReturnType<typeof mount>) {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) throw new Error(`button not found: ${text}`)
  return button
}

async function mountOpenBadge() {
  const wrapper = mount(VersionBadge, {
    global: {
      stubs: { Icon: true }
    }
  })
  await flushPromises()
  await wrapper.get('button').trigger('click')
  return wrapper
}

describe('VersionBadge error retry', () => {
  beforeEach(() => {
    fetchVersion.mockReset()
    fetchVersion.mockResolvedValue(null)
    clearVersionCache.mockReset()
    performUpdate.mockReset()
    updaterStore.versionLoading = false
    updaterStore.versionError = ''
    updaterStore.hasUpdate = true
  })

  it('rechecks the version without installing when the version check failed', async () => {
    updaterStore.versionError = 'GitHub API unavailable'
    updaterStore.hasUpdate = false
    const wrapper = await mountOpenBadge()
    fetchVersion.mockClear()

    await findButton('version.retry', wrapper).trigger('click')
    await flushPromises()

    expect(fetchVersion).toHaveBeenCalledOnce()
    expect(fetchVersion).toHaveBeenCalledWith(true)
    expect(performUpdate).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('retries installation when the update request failed', async () => {
    performUpdate.mockRejectedValueOnce(new Error('install failed'))
    performUpdate.mockResolvedValueOnce({ message: 'updated', need_restart: false })
    const wrapper = await mountOpenBadge()

    await findButton('version.updateNow', wrapper).trigger('click')
    await flushPromises()
    await findButton('version.retry', wrapper).trigger('click')
    await flushPromises()

    expect(performUpdate).toHaveBeenCalledTimes(2)
    expect(fetchVersion).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
