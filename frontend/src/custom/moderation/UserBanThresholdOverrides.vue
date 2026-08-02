<template>
  <div ref="containerRef" class="space-y-4">
    <div>
      <h4 class="text-sm font-semibold text-gray-900 dark:text-white">
        {{ customT('userBanThresholdTitle') }}
      </h4>
      <p class="mt-1 text-xs leading-5 text-gray-500 dark:text-gray-400">
        {{ customT('userBanThresholdHint') }}
      </p>
    </div>

    <div class="relative">
      <Icon
        name="search"
        size="sm"
        class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400"
      />
      <input
        ref="searchInputRef"
        v-model="searchQuery"
        type="search"
        autocomplete="off"
        class="input input-sm w-full pl-9"
        :placeholder="customT('userBanThresholdSearchPlaceholder')"
        :aria-label="customT('userBanThresholdSearchPlaceholder')"
        @input="debounceSearch"
        @focus="showDropdown = true"
        @keydown.esc="showDropdown = false"
      />

      <div
        v-if="showDropdown && searchQuery.trim()"
        class="absolute z-50 mt-1 max-h-60 w-full overflow-auto rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-600 dark:bg-dark-700"
      >
        <div v-if="searchLoading" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
          {{ t('common.loading') }}
        </div>
        <div v-else-if="searchError" class="px-4 py-3 text-sm text-red-600 dark:text-red-400" role="alert">
          {{ searchError }}
        </div>
        <div v-else-if="availableResults.length === 0" class="px-4 py-3 text-sm text-gray-500 dark:text-gray-400">
          {{ customT('userBanThresholdSearchEmpty') }}
        </div>
        <template v-else>
          <button
            v-for="user in availableResults"
            :key="user.id"
            type="button"
            class="flex min-h-11 w-full items-center justify-between gap-3 px-4 py-2 text-left text-sm hover:bg-gray-100 disabled:cursor-not-allowed disabled:opacity-60 dark:hover:bg-dark-600"
            :disabled="user.deleted || selectingUserID !== null"
            @click="selectUser(user)"
          >
            <span class="min-w-0 truncate font-medium text-gray-900 dark:text-white">
              {{ user.email }}
              <span v-if="user.deleted" class="ml-1 text-xs font-normal text-gray-400">
                {{ customT('userBanThresholdDeleted') }}
              </span>
            </span>
            <span class="shrink-0 text-xs text-gray-400">#{{ user.id }}</span>
          </button>
        </template>
      </div>
    </div>

    <p v-if="selectionError" class="text-xs text-red-600 dark:text-red-400" role="alert">
      {{ selectionError }}
    </p>

    <div v-if="modelValue.length === 0" class="rounded-lg border border-dashed border-gray-200 px-4 py-5 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-gray-400">
      {{ customT('userBanThresholdEmpty') }}
    </div>

    <div v-else class="space-y-3">
      <div
        v-for="(override, index) in modelValue"
        :key="`${override.user_id}-${index}`"
        class="grid gap-3 rounded-lg border border-gray-200 p-3 dark:border-dark-600 sm:grid-cols-[minmax(0,1fr)_11rem_2.75rem] sm:items-start"
      >
        <div class="min-w-0 py-1">
          <p class="truncate text-sm font-medium text-gray-900 dark:text-white" :title="userLabel(override.user_id)">
            {{ userLabel(override.user_id) }}
          </p>
          <div class="mt-1 flex flex-wrap items-center gap-2 text-xs text-gray-400">
            <span>#{{ override.user_id }}</span>
            <span v-if="selectedUsers[override.user_id]?.deleted" class="text-amber-600 dark:text-amber-400">
              {{ customT('userBanThresholdDeleted') }}
            </span>
            <span v-else-if="selectedUsers[override.user_id]?.role === 'admin'" class="text-amber-600 dark:text-amber-400">
              {{ customT('userBanThresholdAdmin') }}
            </span>
            <span v-else-if="selectedUsers[override.user_id]?.missing" class="text-amber-600 dark:text-amber-400">
              {{ customT('userBanThresholdUnavailable') }}
            </span>
          </div>
        </div>

        <div>
          <label :for="`user-ban-threshold-${index}`" class="input-label">
            {{ customT('userBanThresholdField') }}
          </label>
          <input
            :id="`user-ban-threshold-${index}`"
            :data-user-id="override.user_id"
            :value="override.ban_threshold"
            type="number"
            min="1"
            :max="maxUserBanThreshold"
            step="1"
            class="input input-sm w-full"
            :class="thresholdError(override) ? 'border-red-500 focus:border-red-500 focus:ring-red-500' : ''"
            :aria-invalid="Boolean(thresholdError(override))"
            :aria-describedby="thresholdError(override) ? `user-ban-threshold-error-${index}` : undefined"
            @input="updateThreshold(index, $event)"
          />
          <p
            v-if="thresholdError(override)"
            :id="`user-ban-threshold-error-${index}`"
            class="mt-1 text-xs text-red-600 dark:text-red-400"
          >
            {{ thresholdError(override) }}
          </p>
        </div>

        <button
          type="button"
          class="inline-flex h-11 w-11 items-center justify-center rounded-lg text-gray-400 hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400"
          :aria-label="customT('userBanThresholdRemove')"
          :title="customT('userBanThresholdRemove')"
          @click="removeOverride(index)"
        >
          <Icon name="trash" size="sm" />
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api/admin'
import type { SimpleUser } from '@/api/admin/usage'
import type { AdminUser } from '@/types'
import Icon from '@/components/icons/Icon.vue'
import type { UserBanThresholdOverride } from '@/custom/moderation/api'
import { customModerationText, type CustomModerationTextKey } from '@/custom/moderation/i18n'
import {
  isValidUserBanThresholdOverride,
  maxUserBanThreshold,
  normalizedDefaultBanThreshold,
} from '@/custom/moderation/userBanThresholds'

type SelectedUser = {
  id: number
  email: string
  role?: AdminUser['role']
  deleted?: boolean
  missing?: boolean
}

const props = defineProps<{
  modelValue: UserBanThresholdOverride[]
  defaultThreshold: number
}>()

const emit = defineEmits<{
  'update:modelValue': [value: UserBanThresholdOverride[]]
}>()

const { t, locale } = useI18n()
const customT = (key: CustomModerationTextKey, params?: Record<string, string | number>) => customModerationText(locale.value, key, params)
const containerRef = ref<HTMLElement | null>(null)
const searchInputRef = ref<HTMLInputElement | null>(null)
const searchQuery = ref('')
const searchResults = ref<SimpleUser[]>([])
const searchLoading = ref(false)
const searchError = ref('')
const selectionError = ref('')
const showDropdown = ref(false)
const selectingUserID = ref<number | null>(null)
const selectedUsers = ref<Record<number, SelectedUser>>({})
let searchTimer: ReturnType<typeof setTimeout> | null = null
let searchSequence = 0

const selectedUserIDs = computed(() => new Set(props.modelValue.map((item) => item.user_id)))
const availableResults = computed(() => searchResults.value.filter((user) => !selectedUserIDs.value.has(user.id)))

function clearPendingSearch(): void {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
  searchSequence += 1
}

function debounceSearch(): void {
  clearPendingSearch()
  searchError.value = ''
  selectionError.value = ''
  const query = searchQuery.value.trim()
  showDropdown.value = true
  if (!query) {
    searchResults.value = []
    searchLoading.value = false
    return
  }

  const sequence = searchSequence
  searchTimer = setTimeout(async () => {
    searchLoading.value = true
    try {
      const results = await adminAPI.usage.searchUsers(query)
      if (sequence === searchSequence) searchResults.value = results
    } catch {
      if (sequence === searchSequence) {
        searchResults.value = []
        searchError.value = customT('userBanThresholdSearchFailed')
      }
    } finally {
      if (sequence === searchSequence) searchLoading.value = false
    }
  }, 300)
}

async function selectUser(result: SimpleUser): Promise<void> {
  if (selectingUserID.value !== null || result.deleted || selectedUserIDs.value.has(result.id)) return
  selectingUserID.value = result.id
  selectionError.value = ''
  try {
    const user = await adminAPI.users.getById(result.id, true)
    if (user.deleted_at) {
      selectionError.value = customT('userBanThresholdDeletedRejected')
      return
    }
    if (user.role === 'admin') {
      selectionError.value = customT('userBanThresholdAdminRejected')
      return
    }
    if (selectedUserIDs.value.has(user.id)) return
    selectedUsers.value = {
      ...selectedUsers.value,
      [user.id]: { id: user.id, email: user.email, role: user.role, deleted: false },
    }
    emit('update:modelValue', [
      ...props.modelValue,
      { user_id: user.id, ban_threshold: normalizedDefaultBanThreshold(props.defaultThreshold) },
    ])
    clearPendingSearch()
    searchQuery.value = ''
    searchResults.value = []
    showDropdown.value = false
    await nextTick()
    containerRef.value
      ?.querySelector<HTMLInputElement>(`input[data-user-id="${user.id}"]`)
      ?.focus()
  } catch {
    selectionError.value = customT('userBanThresholdUserLoadFailed')
  } finally {
    selectingUserID.value = null
  }
}

function updateThreshold(index: number, event: Event): void {
  const input = event.target as HTMLInputElement
  const next = props.modelValue.map((item, itemIndex) => itemIndex === index
    ? { ...item, ban_threshold: input.value === '' ? Number.NaN : input.valueAsNumber }
    : item)
  emit('update:modelValue', next)
}

function removeOverride(index: number): void {
  emit('update:modelValue', props.modelValue.filter((_, itemIndex) => itemIndex !== index))
  void nextTick(() => searchInputRef.value?.focus())
}

function thresholdError(override: UserBanThresholdOverride): string {
  return isValidUserBanThresholdOverride(override) ? '' : customT('userBanThresholdInvalid')
}

function userLabel(userID: number): string {
  return selectedUsers.value[userID]?.email || customT('userBanThresholdUserFallback', { id: userID })
}

async function hydrateSelectedUsers(userIDs: number[]): Promise<void> {
  const missingIDs = userIDs.filter((id) => !selectedUsers.value[id])
  if (missingIDs.length === 0) return

  const users = await Promise.all(missingIDs.map(async (id): Promise<SelectedUser> => {
    try {
      const user = await adminAPI.users.getById(id, true)
      return {
        id: user.id,
        email: user.email,
        role: user.role,
        deleted: Boolean(user.deleted_at),
      }
    } catch {
      return { id, email: '', missing: true }
    }
  }))

  const next = { ...selectedUsers.value }
  for (const user of users) {
    if (props.modelValue.some((item) => item.user_id === user.id)) next[user.id] = user
  }
  selectedUsers.value = next
}

function handleDocumentClick(event: MouseEvent): void {
  const target = event.target as Node | null
  if (target && !containerRef.value?.contains(target)) showDropdown.value = false
}

watch(
  () => props.modelValue.map((item) => item.user_id),
  (userIDs) => { void hydrateSelectedUsers(userIDs) },
  { immediate: true },
)

onMounted(() => document.addEventListener('click', handleDocumentClick))
onUnmounted(() => {
  clearPendingSearch()
  document.removeEventListener('click', handleDocumentClick)
})
</script>
