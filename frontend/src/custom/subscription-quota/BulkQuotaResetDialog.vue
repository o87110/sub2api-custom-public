<template>
  <BaseDialog
    :show="show"
    :title="t('admin.subscriptions.bulkReset.title')"
    width="extra-wide"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="handleClose"
  >
    <div v-if="loading" class="flex min-h-56 items-center justify-center" data-testid="bulk-reset-loading">
      <div class="text-center text-sm text-gray-500 dark:text-gray-400">
        <Icon name="refresh" size="lg" class="mx-auto mb-3 animate-spin" />
        {{ t('admin.subscriptions.bulkReset.loading') }}
      </div>
    </div>

    <div v-else-if="loadError" class="flex min-h-56 items-center justify-center" data-testid="bulk-reset-error">
      <div class="max-w-md text-center">
        <p class="text-sm font-medium text-red-600 dark:text-red-400">
          {{ t('admin.subscriptions.bulkReset.loadFailed') }}
        </p>
        <button type="button" class="btn btn-secondary mt-4" @click="loadCandidates">
          {{ t('admin.subscriptions.bulkReset.retry') }}
        </button>
      </div>
    </div>

    <div v-else-if="result" class="space-y-5" data-testid="bulk-reset-result">
      <div class="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <ResultCount :label="t('admin.subscriptions.bulkReset.requested')" :value="result.requested_count" tone="neutral" />
        <ResultCount :label="t('admin.subscriptions.bulkReset.success')" :value="result.success_count" tone="success" />
        <ResultCount :label="t('admin.subscriptions.bulkReset.skipped')" :value="result.skipped_count" tone="warning" />
        <ResultCount :label="t('admin.subscriptions.bulkReset.failed')" :value="result.failed_count" tone="danger" />
      </div>

      <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="min-w-[720px] w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs font-medium uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="px-4 py-3">{{ t('admin.subscriptions.bulkReset.user') }}</th>
              <th class="px-4 py-3">{{ t('admin.subscriptions.bulkReset.plan') }}</th>
              <th class="px-4 py-3">{{ t('admin.subscriptions.bulkReset.status') }}</th>
              <th class="px-4 py-3">{{ t('admin.subscriptions.bulkReset.reason') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="item in result.items" :key="item.subscription_id">
              <td class="px-4 py-3 text-gray-700 dark:text-gray-200">
                <div class="font-medium">{{ candidateByID.get(item.subscription_id)?.user_email || `#${item.subscription_id}` }}</div>
                <div v-if="candidateByID.get(item.subscription_id)?.username" class="text-xs text-gray-500 dark:text-gray-400">
                  {{ candidateByID.get(item.subscription_id)?.username }}
                </div>
              </td>
              <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                {{ candidateByID.get(item.subscription_id)?.plan_name || '—' }}
              </td>
              <td class="px-4 py-3">
                <span :class="statusClass(item.status)" class="inline-flex rounded-full px-2.5 py-1 text-xs font-medium">
                  {{ statusLabel(item.status) }}
                </span>
              </td>
              <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                {{ resultReason(item) }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <div v-else-if="candidates.length === 0" class="flex min-h-56 items-center justify-center" data-testid="bulk-reset-empty">
      <div class="max-w-lg text-center">
        <h4 class="text-base font-semibold text-gray-900 dark:text-white">
          {{ t('admin.subscriptions.bulkReset.emptyTitle') }}
        </h4>
        <p class="mt-2 text-sm leading-6 text-gray-500 dark:text-gray-400">
          {{ t('admin.subscriptions.bulkReset.emptyHint') }}
        </p>
      </div>
    </div>

    <div v-else class="space-y-4" data-testid="bulk-reset-candidates">
      <div class="flex flex-wrap items-center justify-between gap-3">
        <p class="text-sm text-gray-600 dark:text-gray-300">
          {{ t('admin.subscriptions.bulkReset.summary', { users: candidateList?.user_count ?? 0, subscriptions: candidateList?.subscription_count ?? 0 }) }}
        </p>
        <span class="text-sm font-medium text-primary-600 dark:text-primary-400">
          {{ t('admin.subscriptions.bulkReset.selected', { count: selectedCount }) }}
        </span>
      </div>

      <p
        v-if="candidates.length > BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS"
        class="rounded-lg bg-blue-50 px-4 py-3 text-sm text-blue-700 dark:bg-blue-900/20 dark:text-blue-300"
        data-testid="bulk-reset-limit-hint"
      >
        {{ t('admin.subscriptions.bulkReset.selectionLimit', { count: BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS }) }}
      </p>

      <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-600">
        <table class="min-w-[820px] w-full text-left text-sm">
          <thead class="bg-gray-50 text-xs font-medium uppercase tracking-wide text-gray-500 dark:bg-dark-800 dark:text-gray-400">
            <tr>
              <th class="w-12 px-4 py-3">
                <input
                  id="bulk-reset-select-all"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
                  :checked="allSelected"
                  :indeterminate="someSelected"
                  :disabled="submitError"
                  :aria-label="t('admin.subscriptions.bulkReset.selectAll')"
                  @change="toggleAll(($event.target as HTMLInputElement).checked)"
                />
              </th>
              <th class="px-4 py-3">{{ t('admin.subscriptions.bulkReset.user') }}</th>
              <th class="px-4 py-3">{{ t('admin.subscriptions.bulkReset.plan') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.subscriptions.bulkReset.cycleUsage') }}</th>
              <th class="px-4 py-3 text-right">{{ t('admin.subscriptions.bulkReset.resetCount') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100 dark:divide-dark-700">
            <tr v-for="candidate in candidates" :key="candidate.subscription_id" class="hover:bg-gray-50/70 dark:hover:bg-dark-800/60">
              <td class="px-4 py-3">
                <input
                  :id="`bulk-reset-subscription-${candidate.subscription_id}`"
                  type="checkbox"
                  class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500 dark:border-dark-500 dark:bg-dark-700"
                  :checked="selectedIDs.has(candidate.subscription_id)"
                  :disabled="submitError || (!selectedIDs.has(candidate.subscription_id) && selectedCount >= BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS)"
                  :aria-label="t('admin.subscriptions.bulkReset.selectSubscription', { email: candidate.user_email, plan: candidate.plan_name })"
                  @change="toggleOne(candidate.subscription_id, ($event.target as HTMLInputElement).checked)"
                />
              </td>
              <td class="px-4 py-3 text-gray-700 dark:text-gray-200">
                <label :for="`bulk-reset-subscription-${candidate.subscription_id}`" class="cursor-pointer font-medium">
                  {{ candidate.user_email }}
                </label>
                <div v-if="candidate.username" class="text-xs text-gray-500 dark:text-gray-400">
                  {{ candidate.username }} · #{{ candidate.user_id }}
                </div>
              </td>
              <td class="px-4 py-3 text-gray-600 dark:text-gray-300">
                {{ candidate.plan_name }}
              </td>
              <td class="px-4 py-3 text-right tabular-nums text-gray-700 dark:text-gray-200">
                {{ formatUsage(candidate.cycle_usage_usd) }}
              </td>
              <td class="px-4 py-3 text-right tabular-nums text-gray-700 dark:text-gray-200">
                {{ candidate.manual_quota_reset_count }}
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      <p v-if="submitError" class="rounded-lg bg-red-50 px-4 py-3 text-sm leading-6 text-red-700 dark:bg-red-900/20 dark:text-red-300" data-testid="bulk-reset-submit-error">
        {{ t('admin.subscriptions.bulkReset.submitFailed') }}
      </p>
      <p class="rounded-lg bg-amber-50 px-4 py-3 text-sm leading-6 text-amber-800 dark:bg-amber-900/20 dark:text-amber-300">
        {{ t('admin.subscriptions.bulkReset.warning') }}
      </p>
    </div>

    <template #footer>
      <button v-if="!result" type="button" class="btn btn-secondary" :disabled="submitting" @click="handleClose">
        {{ t('common.cancel') }}
      </button>
      <button
        v-if="!result && !loading && !loadError && candidates.length > 0"
        type="button"
        class="btn btn-primary"
        :disabled="submitting || selectedCount === 0"
        data-testid="bulk-reset-submit"
        @click="submitReset"
      >
        {{ submitting ? t('admin.subscriptions.bulkReset.submitting') : submitError ? t('admin.subscriptions.bulkReset.retrySubmit') : t('admin.subscriptions.bulkReset.submit', { count: selectedCount }) }}
      </button>
      <button v-if="result" type="button" class="btn btn-primary" @click="handleClose">
        {{ t('common.close') }}
      </button>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import ResultCount from './BulkQuotaResetResultCount.vue'
import {
  listBulkResetQuotaCandidates,
  type BulkQuotaResetCandidateList,
  type BulkQuotaResetItemResult,
  type BulkQuotaResetItemStatus,
  type BulkQuotaResetResult
} from '@/api/admin/subscriptions'
import {
  BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS,
  bulkResetQuota
} from './bulkReset'

const props = defineProps<{ show: boolean }>()

const emit = defineEmits<{
  close: []
  completed: []
}>()

const { t } = useI18n()
const loading = ref(false)
const loadError = ref(false)
const submitting = ref(false)
const submitError = ref(false)
const candidateList = ref<BulkQuotaResetCandidateList | null>(null)
const selectedIDs = ref<Set<number>>(new Set())
const result = ref<BulkQuotaResetResult | null>(null)
const submissionIDs = ref<number[]>([])
const idempotencyKey = ref<string | null>(null)
let requestSequence = 0

const candidates = computed(() => candidateList.value?.items ?? [])
const selectedCount = computed(() => selectedIDs.value.size)
const selectableCount = computed(() => Math.min(candidates.value.length, BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS))
const allSelected = computed(() => selectableCount.value > 0 && selectedCount.value === selectableCount.value)
const someSelected = computed(() => selectedCount.value > 0 && !allSelected.value)
const candidateByID = computed(() => new Map(candidates.value.map(candidate => [candidate.subscription_id, candidate])))

watch(
  () => props.show,
  show => {
    if (show) {
      result.value = null
      void loadCandidates()
    } else {
      requestSequence++
    }
  },
  { immediate: true }
)

async function loadCandidates() {
  const sequence = ++requestSequence
  loading.value = true
  loadError.value = false
  candidateList.value = null
  selectedIDs.value = new Set()
  submitError.value = false
  submissionIDs.value = []
  idempotencyKey.value = null
  try {
    const response = await listBulkResetQuotaCandidates()
    if (sequence !== requestSequence || !props.show) return
    candidateList.value = response
    selectedIDs.value = new Set(
      response.items
        .slice(0, BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS)
        .map(item => item.subscription_id)
    )
  } catch (error) {
    if (sequence !== requestSequence || !props.show) return
    loadError.value = true
    console.error('Failed to load bulk quota reset candidates:', error)
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

function toggleAll(checked: boolean) {
  if (submitError.value) return
  selectedIDs.value = checked
    ? new Set(
        candidates.value
          .slice(0, BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS)
          .map(candidate => candidate.subscription_id)
      )
    : new Set()
}

function toggleOne(subscriptionID: number, checked: boolean) {
  if (submitError.value) return
  const next = new Set(selectedIDs.value)
  if (checked && next.size < BULK_QUOTA_RESET_MAX_SUBSCRIPTIONS) next.add(subscriptionID)
  else next.delete(subscriptionID)
  selectedIDs.value = next
}

async function submitReset() {
  if (submitting.value || selectedCount.value === 0) return
  submitting.value = true
  submitError.value = false
  if (!idempotencyKey.value) {
    submissionIDs.value = Array.from(selectedIDs.value)
    idempotencyKey.value = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`
  }
  try {
    result.value = await bulkResetQuota(
      submissionIDs.value,
      idempotencyKey.value
    )
  } catch (error) {
    console.error('Failed to bulk reset subscription quotas:', error)
    submitError.value = true
  } finally {
    submitting.value = false
  }
}

function handleClose() {
  if (submitting.value) return
  if (result.value) emit('completed')
  emit('close')
}

function formatUsage(value: number) {
  const normalized = Number.isFinite(value) ? Math.max(value, 0) : 0
  return `$${normalized.toFixed(2)}`
}

function statusLabel(status: BulkQuotaResetItemStatus) {
  return t(`admin.subscriptions.bulkReset.${status}`)
}

function statusClass(status: BulkQuotaResetItemStatus) {
  if (status === 'success') return 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
  if (status === 'skipped') return 'bg-amber-100 text-amber-700 dark:bg-amber-900/30 dark:text-amber-300'
  return 'bg-red-100 text-red-700 dark:bg-red-900/30 dark:text-red-300'
}

function resultReason(item: BulkQuotaResetItemResult) {
  if (item.status === 'success') return t('admin.subscriptions.bulkReset.completedReason')
  if (item.reason_code === 'SUBSCRIPTION_NO_LONGER_ELIGIBLE') return t('admin.subscriptions.bulkReset.noLongerEligible')
  if (item.reason_code === 'QUOTA_RESET_FAILED') return t('admin.subscriptions.bulkReset.resetFailedReason')
  return t('admin.subscriptions.bulkReset.requestFailedReason')
}
</script>
