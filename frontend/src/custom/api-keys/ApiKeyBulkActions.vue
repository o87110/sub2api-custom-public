<template>
  <section
    v-if="selectedRows.length > 0"
    class="mb-4 rounded-xl border border-primary-200 bg-primary-50/80 p-3 shadow-sm dark:border-primary-800/70 dark:bg-primary-900/20"
    role="region"
    :aria-label="text('selected', { count: selectedRows.length })"
    :aria-busy="busy"
    data-test="api-key-bulk-actions"
  >
    <div class="flex flex-col gap-3 xl:flex-row xl:items-center xl:justify-between">
      <div class="flex min-h-11 flex-wrap items-center gap-x-3 gap-y-2">
        <span
          class="text-sm font-semibold text-primary-900 dark:text-primary-100"
          aria-live="polite"
          data-test="bulk-selected-count"
        >
          {{ text('selected', { count: selectedRows.length }) }}
        </span>
        <button
          type="button"
          class="min-h-11 rounded-lg px-2 text-sm font-medium text-primary-700 transition-colors hover:bg-primary-100 hover:text-primary-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/40 disabled:cursor-not-allowed disabled:opacity-50 dark:text-primary-300 dark:hover:bg-primary-900/40 dark:hover:text-primary-100"
          :disabled="busy"
          data-test="bulk-clear"
          @click="clearSelection"
        >
          {{ text('clear') }}
        </button>
      </div>

      <div class="flex flex-col gap-2 sm:flex-row sm:flex-wrap sm:items-center sm:justify-end">
        <div class="min-w-0 sm:w-72">
          <label class="sr-only" for="api-key-bulk-group">{{ text('targetGroup') }}</label>
          <Select
            id="api-key-bulk-group"
            :model-value="targetGroupId"
            :options="groupOptions"
            :placeholder="text('selectGroup')"
            :search-placeholder="text('searchGroup')"
            :empty-text="text('noGroups')"
            :aria-label="text('targetGroup')"
            :disabled="busy || groupOptions.length === 0"
            searchable
            data-test="bulk-group-select"
            @update:model-value="setTargetGroup"
          >
            <template #selected>
              <GroupBadge
                v-if="selectedGroup"
                :name="selectedGroup.label"
                :platform="selectedGroup.platform"
                :subscription-type="selectedGroup.subscriptionType"
                :rate-multiplier="selectedGroup.rate"
                :user-rate-multiplier="selectedGroup.userRate"
                :peak-rate-enabled="selectedGroup.peakRateEnabled"
                :peak-start="selectedGroup.peakStart"
                :peak-end="selectedGroup.peakEnd"
                :peak-rate-multiplier="selectedGroup.peakRateMultiplier"
              />
              <span v-else class="text-gray-400 dark:text-dark-400">{{ text('selectGroup') }}</span>
            </template>
            <template #option="{ option, selected }">
              <GroupOptionItem
                :name="(option as BulkGroupOption).label"
                :platform="(option as BulkGroupOption).platform"
                :subscription-type="(option as BulkGroupOption).subscriptionType"
                :rate-multiplier="(option as BulkGroupOption).rate"
                :user-rate-multiplier="(option as BulkGroupOption).userRate"
                :peak-rate-enabled="(option as BulkGroupOption).peakRateEnabled"
                :peak-start="(option as BulkGroupOption).peakStart"
                :peak-end="(option as BulkGroupOption).peakEnd"
                :peak-rate-multiplier="(option as BulkGroupOption).peakRateMultiplier"
                :description="(option as BulkGroupOption).description"
                :selected="selected"
              />
            </template>
          </Select>
        </div>

        <button
          type="button"
          class="btn btn-primary min-h-11"
          :disabled="busy || targetGroupId === null"
          data-test="bulk-apply-group"
          @click="applyGroup"
        >
          <Icon
            :name="currentAction === 'group' ? 'refresh' : 'users'"
            size="sm"
            class="mr-2"
            :class="{ 'animate-spin': currentAction === 'group' }"
          />
          {{ currentAction === 'group' ? text('processing') : text('applyGroup') }}
        </button>

        <button
          type="button"
          class="btn btn-secondary min-h-11"
          :disabled="busy"
          data-test="bulk-disable"
          @click="requestConfirmation('disable')"
        >
          <Icon name="ban" size="sm" class="mr-2" />
          {{ currentAction === 'disable' ? text('processing') : text('disable') }}
        </button>

        <button
          type="button"
          class="btn btn-danger min-h-11"
          :disabled="busy"
          data-test="bulk-delete"
          @click="requestConfirmation('delete')"
        >
          <Icon name="trash" size="sm" class="mr-2" />
          {{ currentAction === 'delete' ? text('processing') : text('delete') }}
        </button>
      </div>
    </div>
  </section>

  <ConfirmDialog
    :show="pendingConfirmation !== null"
    :title="confirmationTitle"
    :message="confirmationMessage"
    :confirm-text="confirmationConfirmText"
    :cancel-text="commonText('cancel')"
    :danger="true"
    @confirm="confirmPendingAction"
    @cancel="pendingConfirmation = null"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { keysAPI } from '@/api'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import GroupOptionItem from '@/components/common/GroupOptionItem.vue'
import Select from '@/components/common/Select.vue'
import Icon from '@/components/icons/Icon.vue'
import { useAppStore } from '@/stores/app'
import type { ApiKey, Group, GroupPlatform, SubscriptionType } from '@/types'
import {
  runApiKeyBulkAction,
  type ApiKeyBulkAction,
  type ApiKeyBulkCompletedResult
} from './bulkActions'
import {
  customApiKeyBulkText,
  type ApiKeyBulkTextKey
} from './i18n'

interface BulkGroupOption {
  [key: string]: unknown
  value: number
  label: string
  description: string | null
  rate: number
  userRate: number | null
  peakRateEnabled: boolean
  peakStart: string
  peakEnd: string
  peakRateMultiplier: number
  subscriptionType: SubscriptionType
  platform: GroupPlatform
}

interface PendingConfirmation {
  action: 'disable' | 'delete'
  eligibleIds: number[]
  skippedIds: number[]
}

const props = defineProps<{
  rows: ApiKey[]
  selectedIds: number[]
  groups: Group[]
  userGroupRates: Record<number, number>
}>()

const emit = defineEmits<{
  'update:selectedIds': [ids: number[]]
  'busy-change': [busy: boolean]
  completed: [result: ApiKeyBulkCompletedResult]
}>()

const { locale, t } = useI18n()
const appStore = useAppStore()
const targetGroupId = ref<number | null>(null)
const currentAction = ref<ApiKeyBulkAction | null>(null)
const pendingConfirmation = ref<PendingConfirmation | null>(null)

const text = (key: ApiKeyBulkTextKey, params: Record<string, string | number> = {}) =>
  customApiKeyBulkText(locale.value, key, params)

const commonText = (key: string) => t(`common.${key}`)

const selectedRows = computed(() => {
  const selected = new Set(props.selectedIds)
  return props.rows.filter((row) => selected.has(row.id))
})

const groupOptions = computed<BulkGroupOption[]>(() =>
  props.groups.map((group) => ({
    value: group.id,
    label: group.name,
    description: group.description,
    rate: group.rate_multiplier,
    userRate: props.userGroupRates[group.id] ?? null,
    peakRateEnabled: group.peak_rate_enabled,
    peakStart: group.peak_start,
    peakEnd: group.peak_end,
    peakRateMultiplier: group.peak_rate_multiplier,
    subscriptionType: group.subscription_type,
    platform: group.platform
  }))
)

const selectedGroup = computed(() =>
  groupOptions.value.find((group) => group.value === targetGroupId.value) ?? null
)

const busy = computed(() => currentAction.value !== null)

const confirmationTitle = computed(() =>
  pendingConfirmation.value?.action === 'delete'
    ? text('deleteConfirmTitle')
    : text('disableConfirmTitle')
)

const confirmationMessage = computed(() => {
  const pending = pendingConfirmation.value
  if (!pending) return ''
  if (pending.action === 'delete') {
    return text('deleteConfirmMessage', { count: pending.eligibleIds.length })
  }
  return text('disableConfirmMessage', {
    count: pending.eligibleIds.length,
    skipped: pending.skippedIds.length
  })
})

const confirmationConfirmText = computed(() => {
  const pending = pendingConfirmation.value
  if (!pending) return ''
  return pending.action === 'delete'
    ? text('deleteConfirm', { count: pending.eligibleIds.length })
    : text('disableConfirm', { count: pending.eligibleIds.length })
})

const actionLabel = (action: ApiKeyBulkAction) => {
  if (action === 'group') return text('groupAction')
  if (action === 'disable') return text('disableAction')
  return text('deleteAction')
}

const setTargetGroup = (value: string | number | boolean | null) => {
  targetGroupId.value = typeof value === 'number' ? value : null
}

const clearSelection = () => {
  if (busy.value) return
  targetGroupId.value = null
  pendingConfirmation.value = null
  emit('update:selectedIds', [])
}

const reportOutcome = (
  action: ApiKeyBulkAction,
  succeededIds: number[],
  skippedIds: number[],
  failedIds: number[]
) => {
  const params = {
    action: actionLabel(action),
    success: succeededIds.length,
    skipped: skippedIds.length,
    failed: failedIds.length
  }
  if (failedIds.length === 0) {
    appStore.showSuccess(text('success', params))
  } else if (succeededIds.length > 0 || skippedIds.length > 0) {
    appStore.showWarning(text('partial', params))
  } else {
    appStore.showError(text('failed', params))
  }
}

const completeWithoutRequests = (action: ApiKeyBulkAction, skippedIds: number[]) => {
  targetGroupId.value = action === 'group' ? null : targetGroupId.value
  emit('update:selectedIds', [])
  appStore.showInfo(text('noChanges', {
    action: actionLabel(action),
    skipped: skippedIds.length
  }))
  emit('completed', {
    action,
    succeededIds: [],
    failedIds: [],
    skippedIds
  })
}

const executeAction = async (
  action: ApiKeyBulkAction,
  eligibleIds: number[],
  skippedIds: number[]
) => {
  if (busy.value) return
  if (eligibleIds.length === 0) {
    completeWithoutRequests(action, skippedIds)
    return
  }

  const groupId = targetGroupId.value
  currentAction.value = action
  emit('busy-change', true)

  try {
    const result = await runApiKeyBulkAction(eligibleIds, async (id) => {
      if (action === 'group') {
        if (groupId === null) return
        await keysAPI.update(id, { group_id: groupId })
        return
      }
      if (action === 'disable') {
        await keysAPI.toggleStatus(id, 'inactive')
        return
      }
      await keysAPI.delete(id)
    })

    emit('update:selectedIds', result.failedIds)
    reportOutcome(action, result.succeededIds, skippedIds, result.failedIds)
    if (result.failedIds.length === 0 && action === 'group') {
      targetGroupId.value = null
    }
    emit('completed', {
      action,
      succeededIds: result.succeededIds,
      failedIds: result.failedIds,
      skippedIds
    })
  } finally {
    currentAction.value = null
    emit('busy-change', false)
  }
}

const applyGroup = async () => {
  if (targetGroupId.value === null || busy.value) return
  const eligibleIds = selectedRows.value
    .filter((row) => row.group_id !== targetGroupId.value)
    .map((row) => row.id)
  const skippedIds = selectedRows.value
    .filter((row) => row.group_id === targetGroupId.value)
    .map((row) => row.id)
  await executeAction('group', eligibleIds, skippedIds)
}

const requestConfirmation = (action: 'disable' | 'delete') => {
  if (busy.value) return
  const rows = selectedRows.value
  if (action === 'delete') {
    pendingConfirmation.value = {
      action,
      eligibleIds: rows.map((row) => row.id),
      skippedIds: []
    }
    return
  }

  const eligibleIds = rows.filter((row) => row.status !== 'inactive').map((row) => row.id)
  const skippedIds = rows.filter((row) => row.status === 'inactive').map((row) => row.id)
  if (eligibleIds.length === 0) {
    completeWithoutRequests('disable', skippedIds)
    return
  }
  pendingConfirmation.value = { action, eligibleIds, skippedIds }
}

const confirmPendingAction = async () => {
  const pending = pendingConfirmation.value
  if (!pending || busy.value) return
  pendingConfirmation.value = null
  await executeAction(pending.action, pending.eligibleIds, pending.skippedIds)
}

watch(
  () => props.selectedIds.length,
  (count) => {
    if (count === 0 && !busy.value) {
      targetGroupId.value = null
      pendingConfirmation.value = null
    }
  }
)

watch(groupOptions, (options) => {
  if (
    targetGroupId.value !== null &&
    !options.some((option) => option.value === targetGroupId.value)
  ) {
    targetGroupId.value = null
  }
})
</script>
