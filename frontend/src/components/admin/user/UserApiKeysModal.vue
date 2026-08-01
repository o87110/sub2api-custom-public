<template>
  <BaseDialog :show="show" :title="t('admin.users.userApiKeys')" width="wide" @close="handleClose">
    <div v-if="user" class="space-y-4">
      <div class="flex items-center gap-3 rounded-xl bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex h-10 w-10 items-center justify-center rounded-full bg-primary-100 dark:bg-primary-900/30">
          <span class="text-lg font-medium text-primary-700 dark:text-primary-300">{{ user.email.charAt(0).toUpperCase() }}</span>
        </div>
        <div><p class="font-medium text-gray-900 dark:text-white">{{ user.email }}</p><p class="text-sm text-gray-500 dark:text-dark-400">{{ user.username }}</p></div>
      </div>
      <div v-if="loading" class="flex justify-center py-8"><svg class="h-8 w-8 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg></div>
      <div v-else-if="apiKeys.length === 0" class="py-8 text-center"><p class="text-sm text-gray-500">{{ t('admin.users.noApiKeys') }}</p></div>
      <div v-else ref="scrollContainerRef" class="max-h-96 space-y-3 overflow-y-auto" @scroll="closeGroupSelector">
        <div v-for="key in apiKeys" :key="key.id" class="rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-600 dark:bg-dark-800">
          <div class="flex items-start justify-between">
            <div class="min-w-0 flex-1">
              <div class="mb-1 flex items-center gap-2"><span class="font-medium text-gray-900 dark:text-white">{{ key.name }}</span><span :class="['badge text-xs', key.status === 'active' ? 'badge-success' : 'badge-danger']">{{ key.status }}</span></div>
              <p class="truncate font-mono text-sm text-gray-500">{{ key.key.substring(0, 20) }}...{{ key.key.substring(key.key.length - 8) }}</p>
            </div>
          </div>
          <div class="mt-3 flex flex-wrap gap-4 text-xs text-gray-500">
            <div class="flex items-center gap-1">
              <span>{{ t('admin.users.group') }}:</span>
              <button
                @click="openGroupSelector(key)"
                class="-mx-1 flex min-h-11 cursor-pointer items-center gap-2 rounded-lg px-2 transition-colors hover:bg-gray-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 dark:hover:bg-dark-700"
                :disabled="updatingKeyIds.has(key.id)"
                :aria-label="apiKeyMultiGroupEnabled ? priorityText('edit') : t('keys.clickToChangeGroup')"
              >
                <GroupBadge
                  v-if="key.group_id && key.group"
                  :name="key.group.name"
                  :platform="key.group.platform"
                  :subscription-type="key.group.subscription_type"
                  :rate-multiplier="key.group.rate_multiplier"
                  :peak-rate-enabled="key.group.peak_rate_enabled"
                  :peak-start="key.group.peak_start"
                  :peak-end="key.group.peak_end"
                  :peak-rate-multiplier="key.group.peak_rate_multiplier"
                />
                <span v-else class="text-gray-400 italic">{{ t('admin.users.none') }}</span>
                <span
                  v-if="apiKeyMultiGroupEnabled && (key.group_ids?.length || 0) > 1"
                  class="inline-flex h-6 min-w-6 items-center justify-center rounded-full bg-primary-100 px-1.5 font-semibold tabular-nums text-primary-700 dark:bg-primary-900/40 dark:text-primary-200"
                >
                  +{{ key.group_ids.length - 1 }}
                </span>
                <svg v-if="updatingKeyIds.has(key.id)" class="h-3 w-3 animate-spin text-primary-500" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"></circle><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path></svg>
                <span v-else class="text-xs text-gray-500 dark:text-dark-300">
                  {{ apiKeyMultiGroupEnabled ? priorityText('edit') : t('keys.clickToChangeGroup') }}
                </span>
              </button>
            </div>
            <div class="flex items-center gap-1"><span>{{ t('admin.users.columns.created') }}: {{ formatDateTime(key.created_at) }}</span></div>
          </div>
        </div>
      </div>
    </div>
  </BaseDialog>

  <BaseDialog
    :show="groupSelectorKeyId !== null"
    :title="apiKeyMultiGroupEnabled ? priorityText('edit') : t('keys.clickToChangeGroup')"
    width="normal"
    @close="closeGroupSelector"
  >
    <ApiKeyGroupPriorityEditor
      v-if="apiKeyMultiGroupEnabled"
      :model-value="selectedKeyForGroup?.group_ids || []"
      :groups="allGroups"
      :selected-groups="selectedKeyForGroup?.groups || []"
      :busy="selectedKeyForGroup ? updatingKeyIds.has(selectedKeyForGroup.id) : false"
      :error="groupFieldError"
      show-actions
      @save="changeGroups"
      @cancel="closeGroupSelector"
    />
    <div v-else class="space-y-4">
      <div>
        <label for="legacy-admin-key-dialog-group" class="input-label">{{ t('admin.users.group') }}</label>
        <Select
          id="legacy-admin-key-dialog-group"
          :model-value="legacyDialogGroupID"
          :options="legacyGroupOptions"
          :error="Boolean(groupFieldError)"
          :aria-describedby="groupFieldError ? 'legacy-admin-key-dialog-group-error' : undefined"
          @update:model-value="setLegacyDialogGroup"
        />
        <p v-if="groupFieldError" id="legacy-admin-key-dialog-group-error" class="mt-1 text-sm text-red-500">{{ groupFieldError }}</p>
      </div>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary min-h-11" :disabled="selectedKeyForGroup ? updatingKeyIds.has(selectedKeyForGroup.id) : false" @click="closeGroupSelector">
          {{ t('common.cancel') }}
        </button>
        <button type="button" class="btn btn-primary min-h-11" :disabled="selectedKeyForGroup ? updatingKeyIds.has(selectedKeyForGroup.id) : false" @click="changeLegacyGroup">
          {{ t('common.save') }}
        </button>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import { formatDateTime } from '@/utils/format'
import type { AdminUser, AdminGroup, ApiKey } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Select from '@/components/common/Select.vue'
import ApiKeyGroupPriorityEditor from '@/custom/api-keys/ApiKeyGroupPriorityEditor.vue'
import {
  apiKeyGroupPriorityText,
  type ApiKeyGroupPriorityTextKey
} from '@/custom/api-keys/priorityI18n'
import { apiKeyGroupFieldError } from '@/custom/api-keys/fieldError'

const props = defineProps<{ show: boolean; user: AdminUser | null }>()
const emit = defineEmits(['close'])
const { t, locale } = useI18n()
const appStore = useAppStore()
const priorityText = (
  key: ApiKeyGroupPriorityTextKey,
  params: Record<string, string | number> = {}
) => apiKeyGroupPriorityText(locale.value, key, params)

const apiKeys = ref<ApiKey[]>([])
const allGroups = ref<AdminGroup[]>([])
const loading = ref(false)
const updatingKeyIds = ref(new Set<number>())
const groupSelectorKeyId = ref<number | null>(null)
const scrollContainerRef = ref<HTMLElement | null>(null)
const groupFieldError = ref('')
const legacyDialogGroupID = ref(0)

const apiKeyMultiGroupEnabled = computed(
  () => appStore.cachedPublicSettings?.api_key_multi_group_enabled === true
)

const legacyGroupOptions = computed(() => [
  { value: 0, label: t('admin.users.none') },
  ...allGroups.value.map((group) => ({ value: group.id, label: group.name }))
])

const setLegacyDialogGroup = (value: string | number | boolean | null) => {
  legacyDialogGroupID.value = Number(value) || 0
}

const selectedKeyForGroup = computed(() => {
  if (groupSelectorKeyId.value === null) return null
  return apiKeys.value.find((k) => k.id === groupSelectorKeyId.value) || null
})

watch(() => props.show, (v) => {
  if (v && props.user) {
    load()
    loadGroups()
  } else {
    closeGroupSelector()
  }
})

const load = async () => {
  if (!props.user) return
  loading.value = true
  try {
    const res = await adminAPI.users.getUserApiKeys(props.user.id)
    apiKeys.value = res.items || []
  } catch (error) {
    console.error('Failed to load API keys:', error)
  } finally {
    loading.value = false
  }
}

const loadGroups = async () => {
  try {
    const groups = await adminAPI.groups.getAll()
    allGroups.value = groups
  } catch (error) {
    console.error('Failed to load groups:', error)
  }
}

const openGroupSelector = (key: ApiKey) => {
  groupFieldError.value = ''
  legacyDialogGroupID.value = key.group_id ?? 0
  groupSelectorKeyId.value = key.id
}

const closeGroupSelector = () => {
  if (selectedKeyForGroup.value && updatingKeyIds.value.has(selectedKeyForGroup.value.id)) return
  groupSelectorKeyId.value = null
  groupFieldError.value = ''
}

const changeGroups = async (groupIDs: number[]) => {
  const key = selectedKeyForGroup.value
  if (!key) return
  updatingKeyIds.value.add(key.id)
  groupFieldError.value = ''
  try {
    const result = await adminAPI.apiKeys.updateApiKeyGroups(key.id, groupIDs)
    // Update local data
    const idx = apiKeys.value.findIndex((k) => k.id === key.id)
    if (idx !== -1) {
      apiKeys.value[idx] = result.api_key
    }
    if (result.auto_granted_group_access && result.granted_group_name) {
      appStore.showSuccess(t('admin.users.groupChangedWithGrant', { group: result.granted_group_name }))
    } else {
      appStore.showSuccess(t('admin.users.groupChangedSuccess'))
    }
    groupSelectorKeyId.value = null
  } catch (error: unknown) {
    groupFieldError.value =
      apiKeyGroupFieldError(error, ['group_ids']) ||
      t('admin.users.groupChangeFailed')
  } finally {
    updatingKeyIds.value.delete(key.id)
  }
}

const changeLegacyGroup = async () => {
  const key = selectedKeyForGroup.value
  if (!key) return
  updatingKeyIds.value.add(key.id)
  groupFieldError.value = ''
  try {
    const result = await adminAPI.apiKeys.updateApiKeyGroup(
      key.id,
      legacyDialogGroupID.value > 0 ? legacyDialogGroupID.value : null
    )
    const idx = apiKeys.value.findIndex((item) => item.id === key.id)
    if (idx !== -1) apiKeys.value[idx] = result.api_key
    appStore.showSuccess(t('admin.users.groupChangedSuccess'))
    groupSelectorKeyId.value = null
  } catch (error: unknown) {
    groupFieldError.value =
      apiKeyGroupFieldError(error, ['group_id']) ||
      t('admin.users.groupChangeFailed')
  } finally {
    updatingKeyIds.value.delete(key.id)
  }
}

const handleClose = () => {
  closeGroupSelector()
  emit('close')
}

</script>
