<template>
  <section
    ref="editorRoot"
    class="space-y-3"
    :aria-labelledby="labelId"
    :aria-busy="busy"
    data-test="api-key-group-priority-editor"
  >
    <div class="space-y-1">
      <div class="flex items-center justify-between gap-3">
        <h3 :id="labelId" class="text-sm font-medium text-gray-900 dark:text-white">{{ text('label') }}</h3>
        <span class="text-xs tabular-nums text-gray-500 dark:text-dark-400">
          {{ draft.length }}/{{ maxGroups }}
        </span>
      </div>
      <p class="text-sm leading-5 text-gray-500 dark:text-dark-300">{{ text('help') }}</p>
    </div>

    <div class="flex flex-col gap-2 sm:flex-row">
      <label class="sr-only" :for="selectId">{{ text('addPlaceholder') }}</label>
      <select
        ref="groupSelect"
        :id="selectId"
        v-model="pendingGroupID"
        class="input min-h-11 flex-1"
        :aria-labelledby="labelId"
        :aria-describedby="error ? errorId : undefined"
        :aria-invalid="Boolean(error)"
        :disabled="disabled || busy || addableGroups.length === 0 || draft.length >= maxGroups"
        data-test="group-priority-select"
      >
        <option value="">{{ text('addPlaceholder') }}</option>
        <option v-for="group in addableGroups" :key="group.id" :value="String(group.id)">
          {{ group.name }} · {{ group.platform }} · {{ group.subscription_type }} · {{ group.rate_multiplier }}x
        </option>
      </select>
      <button
        type="button"
        class="btn btn-secondary min-h-11 shrink-0 focus-visible:ring-2 focus-visible:ring-primary-500/50"
        :disabled="disabled || busy || pendingGroupID === '' || draft.length >= maxGroups"
        data-test="group-priority-add"
        @click="addPendingGroup"
      >
        <Icon name="plus" size="sm" class="mr-2" />
        {{ text('add') }}
      </button>
    </div>

    <p
      v-if="draft.length >= maxGroups"
      class="text-sm text-amber-700 dark:text-amber-300"
      role="status"
    >
      {{ text('maxReached', { max: maxGroups }) }}
    </p>
    <p
      v-else-if="selectedPlatform"
      class="text-xs text-gray-500 dark:text-dark-400"
    >
      {{ text('samePlatform', { platform: selectedPlatform }) }}
    </p>

    <ol v-if="draft.length > 0" class="space-y-2" data-test="group-priority-list">
      <li
        v-for="(groupID, index) in draft"
        :key="groupID"
        class="group flex min-w-0 items-center gap-2 rounded-xl border border-gray-200 bg-white p-2 shadow-sm transition-colors focus-within:border-primary-400 dark:border-dark-600 dark:bg-dark-800 dark:focus-within:border-primary-500"
        :class="{ 'opacity-60': dragIndex === index }"
        :tabindex="disabled || busy ? -1 : 0"
        :aria-disabled="disabled || busy"
        :aria-label="`${text('priority', { priority: index + 1 })}: ${groupForID(groupID)?.name ?? `#${groupID}`}`"
        :data-test="`group-priority-item-${groupID}`"
        @dragover.prevent
        @drop.prevent="dropAt(index)"
        @keydown.alt.up.self.prevent="move(index, -1)"
        @keydown.alt.down.self.prevent="move(index, 1)"
      >
        <span
          class="flex h-11 w-11 shrink-0 cursor-grab touch-none items-center justify-center rounded-lg bg-gray-100 font-semibold tabular-nums text-gray-700 active:cursor-grabbing dark:bg-dark-700 dark:text-gray-200"
          :draggable="!disabled && !busy"
          aria-hidden="true"
          :data-test="`group-priority-drag-${groupID}`"
          @dragstart="startDrag($event, index)"
          @dragend="dragIndex = null"
        >
          {{ index + 1 }}
        </span>
        <div class="min-w-0 flex-1">
          <GroupBadge
            v-if="groupForID(groupID)"
            class="max-w-full"
            :name="groupForID(groupID)!.name"
            :platform="groupForID(groupID)!.platform"
            :subscription-type="groupForID(groupID)!.subscription_type"
            :rate-multiplier="groupForID(groupID)!.rate_multiplier"
            :user-rate-multiplier="userGroupRates[groupID]"
            :peak-rate-enabled="groupForID(groupID)!.peak_rate_enabled"
            :peak-start="groupForID(groupID)!.peak_start"
            :peak-end="groupForID(groupID)!.peak_end"
            :peak-rate-multiplier="groupForID(groupID)!.peak_rate_multiplier"
          />
          <span v-else class="font-mono text-sm text-gray-700 dark:text-gray-200">#{{ groupID }}</span>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <button
            type="button"
            class="inline-flex h-11 w-11 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 disabled:cursor-not-allowed disabled:opacity-35 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
            :disabled="disabled || busy || index === 0"
            :aria-label="text('moveUp', { name: groupForID(groupID)?.name ?? `#${groupID}` })"
            :data-test="`group-priority-move-up-${groupID}`"
            @click="move(index, -1)"
          >
            <Icon name="arrowUp" size="sm" />
          </button>
          <button
            type="button"
            class="inline-flex h-11 w-11 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 focus:outline-none focus-visible:ring-2 focus-visible:ring-primary-500/50 disabled:cursor-not-allowed disabled:opacity-35 dark:text-dark-300 dark:hover:bg-dark-700 dark:hover:text-white"
            :disabled="disabled || busy || index === draft.length - 1"
            :aria-label="text('moveDown', { name: groupForID(groupID)?.name ?? `#${groupID}` })"
            :data-test="`group-priority-move-down-${groupID}`"
            @click="move(index, 1)"
          >
            <Icon name="arrowDown" size="sm" />
          </button>
          <button
            type="button"
            class="inline-flex h-11 w-11 items-center justify-center rounded-lg text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 focus:outline-none focus-visible:ring-2 focus-visible:ring-red-500/50 disabled:cursor-not-allowed disabled:opacity-35 dark:text-dark-300 dark:hover:bg-red-900/20 dark:hover:text-red-300"
            :disabled="disabled || busy"
            :aria-label="text('remove', { name: groupForID(groupID)?.name ?? `#${groupID}` })"
            :data-test="`group-priority-remove-${groupID}`"
            @click="remove(index)"
          >
            <Icon name="trash" size="sm" />
          </button>
        </div>
      </li>
    </ol>
    <div
      v-else
      class="rounded-xl border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
      data-test="group-priority-empty"
    >
      {{ text('empty') }}
    </div>

    <p v-if="error" :id="errorId" class="text-sm text-red-600 dark:text-red-400" role="alert">
      {{ error }}
    </p>
    <p class="sr-only" aria-live="polite">{{ announcement }}</p>

    <div v-if="showActions" class="flex flex-wrap justify-end gap-2 pt-1">
      <button
        type="button"
        class="btn btn-secondary min-h-11"
        :disabled="busy"
        data-test="group-priority-cancel"
        @click="cancel"
      >
        {{ text('cancel') }}
      </button>
      <button
        type="button"
        class="btn btn-primary min-h-11"
        :disabled="busy || !dirty"
        data-test="group-priority-save"
        @click="$emit('save', [...draft])"
      >
        <Icon v-if="busy" name="refresh" size="sm" class="mr-2 animate-spin" />
        {{ text('save') }}
      </button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import GroupBadge from '@/components/common/GroupBadge.vue'
import Icon from '@/components/icons/Icon.vue'
import { groupBalanceRequirement } from '@/custom/group-access/minimumBalance'
import type { Group } from '@/types'
import { apiKeyGroupPriorityText, type ApiKeyGroupPriorityTextKey } from './priorityI18n'

const props = withDefaults(defineProps<{
  modelValue: number[]
  groups: Group[]
  selectedGroups?: Group[]
  userGroupRates?: Record<number, number>
  maxGroups?: number
  disabled?: boolean
  busy?: boolean
  error?: string
  showActions?: boolean
}>(), {
  selectedGroups: () => [],
  userGroupRates: () => ({}),
  maxGroups: 10,
  disabled: false,
  busy: false,
  error: '',
  showActions: false
})

const emit = defineEmits<{
  'update:modelValue': [groupIDs: number[]]
  save: [groupIDs: number[]]
  cancel: []
}>()

const { locale } = useI18n()
const text = (
  key: ApiKeyGroupPriorityTextKey,
  params: Record<string, string | number> = {}
) => apiKeyGroupPriorityText(locale.value, key, params)

const selectId = `api-key-group-priority-${Math.random().toString(36).slice(2)}`
const labelId = `${selectId}-label`
const errorId = `${selectId}-error`
const draft = ref<number[]>([])
const pendingGroupID = ref('')
const dragIndex = ref<number | null>(null)
const announcement = ref('')
const editorRoot = ref<HTMLElement | null>(null)
const groupSelect = ref<HTMLSelectElement | null>(null)

const normalized = (values: number[]) =>
  values.filter((value, index) => Number.isInteger(value) && value > 0 && values.indexOf(value) === index)
    .slice(0, props.maxGroups)

watch(
  () => props.modelValue,
  (values) => {
    draft.value = normalized(values ?? [])
  },
  { immediate: true, deep: true }
)

const groupMap = computed(() => {
  const groups = new Map<number, Group>()
  for (const group of [...props.selectedGroups, ...props.groups]) groups.set(group.id, group)
  return groups
})

const groupForID = (groupID: number) => groupMap.value.get(groupID)
const selectedPlatform = computed(() => groupForID(draft.value[0])?.platform ?? '')
const originallySelectedGroupIDs = computed(
  () => new Set(props.selectedGroups.map((group) => group.id))
)
const addableGroups = computed(() => {
  const selected = new Set(draft.value)
  return props.groups.filter((group) =>
    !selected.has(group.id) &&
    (!selectedPlatform.value || group.platform === selectedPlatform.value) &&
    (originallySelectedGroupIDs.value.has(group.id) || groupBalanceRequirement(group) === null)
  )
})
const dirty = computed(() =>
  draft.value.length !== props.modelValue.length ||
  draft.value.some((groupID, index) => groupID !== props.modelValue[index])
)

const updateDraft = (next: number[]) => {
  draft.value = normalized(next)
  if (!props.showActions) {
    emit('update:modelValue', [...draft.value])
  }
}

const addPendingGroup = () => {
  if (props.disabled || props.busy) return
  const groupID = Number(pendingGroupID.value)
  if (!Number.isInteger(groupID) || groupID <= 0 || draft.value.length >= props.maxGroups) return
  const group = addableGroups.value.find((candidate) => candidate.id === groupID)
  if (!group) return
  updateDraft([...draft.value, groupID])
  pendingGroupID.value = ''
  announcement.value = text('added', {
    name: group.name,
    priority: draft.value.length
  })
}

const move = (index: number, direction: -1 | 1) => {
  if (props.disabled || props.busy) return
  const target = index + direction
  if (target < 0 || target >= draft.value.length) return
  const next = [...draft.value]
  const [groupID] = next.splice(index, 1)
  next.splice(target, 0, groupID)
  updateDraft(next)
  announcement.value = text('moved', {
    name: groupForID(groupID)?.name ?? `#${groupID}`,
    priority: target + 1
  })
}

const remove = async (index: number) => {
  if (props.disabled || props.busy) return
  const groupID = draft.value[index]
  const next = draft.value.filter((_, current) => current !== index)
  updateDraft(next)
  announcement.value = text('removed', { name: groupForID(groupID)?.name ?? `#${groupID}` })
  await nextTick()
  const focusGroupID = next[Math.min(index, next.length - 1)]
  if (focusGroupID !== undefined) {
    editorRoot.value
      ?.querySelector<HTMLElement>(`[data-test="group-priority-item-${focusGroupID}"]`)
      ?.focus()
    return
  }
  groupSelect.value?.focus()
}

const startDrag = (event: DragEvent, index: number) => {
  if (props.disabled || props.busy) return
  dragIndex.value = index
  if (event.dataTransfer) {
    event.dataTransfer.effectAllowed = 'move'
    event.dataTransfer.setData('text/plain', String(draft.value[index]))
  }
}

const dropAt = (index: number) => {
  if (props.disabled || props.busy) {
    dragIndex.value = null
    return
  }
  if (dragIndex.value === null || dragIndex.value === index) {
    dragIndex.value = null
    return
  }
  const from = dragIndex.value
  dragIndex.value = null
  const next = [...draft.value]
  const [groupID] = next.splice(from, 1)
  next.splice(index, 0, groupID)
  updateDraft(next)
  announcement.value = text('moved', {
    name: groupForID(groupID)?.name ?? `#${groupID}`,
    priority: index + 1
  })
}

const cancel = () => {
  draft.value = normalized(props.modelValue)
  pendingGroupID.value = ''
  emit('cancel')
}
</script>
