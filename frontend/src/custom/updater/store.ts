import { ref } from 'vue'
import { defineStore } from 'pinia'

import {
  checkUpdates,
  type ReleaseInfo,
  type VersionInfo
} from '@/custom/updater/api'

export const useCustomUpdaterStore = defineStore('custom-updater', () => {
  const versionLoaded = ref(false)
  const versionLoading = ref(false)
  const versionError = ref('')
  const versionStale = ref(false)
  const currentVersion = ref('')
  const currentBuildVersion = ref('')
  const latestVersion = ref('')
  const hasUpdate = ref(false)
  const buildType = ref('source')
  const releaseInfo = ref<ReleaseInfo | null>(null)
  const updateRepository = ref('')
  const officialRepository = ref('')
  const officialLatestVersion = ref('')
  const hasOfficialUpdate = ref(false)
  const officialReleaseInfo = ref<ReleaseInfo | null>(null)
  const officialReleaseWarning = ref('')

  function snapshot(): VersionInfo {
    return {
      current_version: currentVersion.value,
      current_build_version: currentBuildVersion.value,
      latest_version: latestVersion.value,
      has_update: hasUpdate.value,
      build_type: buildType.value,
      release_info: releaseInfo.value || undefined,
      update_repository: updateRepository.value || undefined,
      official_repository: officialRepository.value || undefined,
      official_latest_version: officialLatestVersion.value || undefined,
      has_official_update: hasOfficialUpdate.value,
      official_release_info: officialReleaseInfo.value || undefined,
      official_release_warning: officialReleaseWarning.value || undefined,
      warning: versionError.value || undefined,
      cached: true
    }
  }

  async function fetchVersion(force = false): Promise<VersionInfo | null> {
    if (versionLoaded.value && !force) return snapshot()
    if (versionLoading.value) return null

    versionLoading.value = true
    versionError.value = ''
    try {
      const data = await checkUpdates(force)
      currentVersion.value = data.current_version
      currentBuildVersion.value = data.current_build_version || data.current_version
      latestVersion.value = data.latest_version
      hasUpdate.value = data.has_update
      buildType.value = data.build_type || 'source'
      releaseInfo.value = data.release_info || null
      updateRepository.value = data.update_repository || ''
      officialRepository.value = data.official_repository || ''
      officialLatestVersion.value = data.official_latest_version || ''
      hasOfficialUpdate.value = data.has_official_update || false
      officialReleaseInfo.value = data.official_release_info || null
      officialReleaseWarning.value = data.official_release_warning || ''
      versionLoaded.value = true
      versionError.value = data.warning || ''
      versionStale.value = Boolean(data.warning && data.cached)
      return data
    } catch (error) {
      console.error('Failed to fetch custom updater version:', error)
      const requestError = error as {
        response?: { data?: { message?: string } }
        message?: string
      }
      versionError.value =
        requestError.response?.data?.message ||
        requestError.message ||
        'Failed to check for updates'
      versionStale.value = versionLoaded.value
      if (!versionLoaded.value) {
        hasUpdate.value = false
        hasOfficialUpdate.value = false
        latestVersion.value = ''
        officialLatestVersion.value = ''
        releaseInfo.value = null
        officialReleaseInfo.value = null
      }
      return null
    } finally {
      versionLoading.value = false
    }
  }

  function clearVersionCache(): void {
    versionLoaded.value = false
    versionError.value = ''
    versionStale.value = false
    hasUpdate.value = false
    hasOfficialUpdate.value = false
  }

  return {
    versionLoading,
    versionError,
    versionStale,
    currentVersion,
    currentBuildVersion,
    latestVersion,
    hasUpdate,
    buildType,
    releaseInfo,
    updateRepository,
    officialRepository,
    officialLatestVersion,
    hasOfficialUpdate,
    officialReleaseInfo,
    officialReleaseWarning,
    fetchVersion,
    clearVersionCache
  }
})
