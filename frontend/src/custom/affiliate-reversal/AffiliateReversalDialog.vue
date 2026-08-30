<template>
  <BaseDialog
    :show="show"
    :title="t('admin.affiliates.reversal.dialogTitle')"
    width="wide"
    :close-on-escape="!submitting"
    :show-close-button="!submitting"
    @close="handleClose"
  >
    <div class="space-y-5" :aria-busy="loading || submitting">
      <div v-if="loading" class="flex min-h-40 items-center justify-center" role="status">
        <div class="h-7 w-7 animate-spin rounded-full border-2 border-red-500 border-t-transparent motion-reduce:animate-none" aria-hidden="true"></div>
        <span class="ml-3 text-sm text-gray-600 dark:text-gray-300">
          {{ t('admin.affiliates.reversal.previewLoading') }}
        </span>
      </div>

      <div v-else-if="previewError" class="rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-900/50 dark:bg-red-950/30" role="alert">
        <p class="text-sm font-medium text-red-800 dark:text-red-200">{{ previewError }}</p>
        <button type="button" class="btn btn-secondary btn-sm mt-3" @click="loadPreview">
          {{ t('common.retry') }}
        </button>
      </div>

      <template v-else-if="preview">
        <div class="grid gap-3 sm:grid-cols-3">
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
            <div class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.affiliates.reversal.orderCount') }}</div>
            <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">{{ preview.order_count }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
            <div class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.affiliates.reversal.totalAmount') }}</div>
            <div class="mt-1 text-lg font-semibold text-gray-900 dark:text-white">${{ formatAmount(preview.total_rebate_amount) }}</div>
          </div>
          <div class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800">
            <div class="text-xs text-gray-500 dark:text-dark-400">{{ t('admin.affiliates.reversal.balanceDeducted') }}</div>
            <div class="mt-1 text-lg font-semibold text-red-700 dark:text-red-300">${{ formatAmount(preview.total_balance_deducted) }}</div>
          </div>
        </div>

        <div class="overflow-x-auto rounded-lg border border-gray-200 dark:border-dark-700">
          <table class="min-w-full divide-y divide-gray-200 text-sm dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800">
              <tr class="text-left text-xs text-gray-500 dark:text-dark-400">
                <th class="px-3 py-2 font-medium">{{ t('admin.affiliates.records.inviter') }}</th>
                <th class="px-3 py-2 text-right font-medium">{{ t('admin.affiliates.reversal.frozenDeducted') }}</th>
                <th class="px-3 py-2 text-right font-medium">{{ t('admin.affiliates.reversal.quotaDeducted') }}</th>
                <th class="px-3 py-2 text-right font-medium">{{ t('admin.affiliates.reversal.balanceDeducted') }}</th>
                <th class="px-3 py-2 text-right font-medium">{{ t('admin.affiliates.reversal.balanceAfter') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-100 bg-white dark:divide-dark-800 dark:bg-dark-900">
              <tr v-for="impact in preview.inviters" :key="impact.inviter_id">
                <td class="px-3 py-3">
                  <div class="font-mono text-xs text-gray-500 dark:text-dark-400">#{{ impact.inviter_id }}</div>
                  <div class="max-w-52 truncate font-medium text-gray-900 dark:text-white">{{ impact.inviter_email || '-' }}</div>
                  <div class="max-w-52 truncate text-xs text-gray-500 dark:text-dark-400">{{ impact.inviter_username || '-' }}</div>
                </td>
                <td class="px-3 py-3 text-right tabular-nums">${{ formatAmount(impact.frozen_quota_deducted) }}</td>
                <td class="px-3 py-3 text-right tabular-nums">${{ formatAmount(impact.available_quota_deducted) }}</td>
                <td class="px-3 py-3 text-right tabular-nums text-red-700 dark:text-red-300">${{ formatAmount(impact.balance_deducted) }}</td>
                <td
                  class="px-3 py-3 text-right font-semibold tabular-nums"
                  :class="impact.will_be_negative ? 'text-red-700 dark:text-red-300' : 'text-gray-900 dark:text-white'"
                >
                  ${{ formatAmount(impact.balance_after) }}
                </td>
              </tr>
            </tbody>
          </table>
        </div>

        <div
          v-if="preview.has_negative_balance"
          class="rounded-lg border border-red-300 bg-red-50 p-4 dark:border-red-800 dark:bg-red-950/30"
          role="alert"
        >
          <p class="text-sm font-semibold text-red-800 dark:text-red-200">
            {{ t('admin.affiliates.reversal.negativeWarning', { count: preview.negative_balance_users }) }}
          </p>
          <p class="mt-1 text-xs leading-5 text-red-700 dark:text-red-300">
            {{ t('admin.affiliates.reversal.negativeHint') }}
          </p>
          <label class="mt-3 flex min-h-11 cursor-pointer items-center gap-2 text-sm font-medium text-red-900 dark:text-red-100">
            <input
              v-model="confirmNegative"
              type="checkbox"
              :disabled="submitting || !!idempotencyKey"
              data-test="confirm-negative-balance"
              class="h-4 w-4 rounded border-red-300 text-red-600 focus:ring-red-500 dark:border-red-700 dark:bg-dark-900"
            />
            {{ t('admin.affiliates.reversal.confirmNegative') }}
          </label>
        </div>

        <div>
          <label for="affiliate-reversal-reason" class="block text-sm font-medium text-gray-700 dark:text-gray-200">
            {{ t('admin.affiliates.reversal.reasonLabel') }}
          </label>
          <textarea
            id="affiliate-reversal-reason"
            v-model="reason"
            class="input mt-2 min-h-24 resize-y"
            maxlength="500"
            :placeholder="t('admin.affiliates.reversal.reasonPlaceholder')"
            :disabled="submitting || !!idempotencyKey"
            data-test="reversal-reason"
          ></textarea>
          <div class="mt-1 flex justify-between text-xs text-gray-500 dark:text-dark-400">
            <span>{{ t('admin.affiliates.reversal.irreversibleHint') }}</span>
            <span>{{ reason.length }}/500</span>
          </div>
        </div>

        <div v-if="submitError" class="rounded-lg border border-red-200 bg-red-50 p-3 text-sm text-red-800 dark:border-red-900/50 dark:bg-red-950/30 dark:text-red-200" role="alert" data-test="submit-error">
          {{ submitError }}
        </div>
      </template>
    </div>

    <template #footer>
      <div class="flex w-full flex-col-reverse gap-2 sm:flex-row sm:justify-end">
        <button type="button" class="btn btn-secondary min-h-11" :disabled="submitting" @click="handleClose">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-danger min-h-11"
          :disabled="!canSubmit"
          data-test="submit-reversal"
          @click="submitReversal"
        >
          <span v-if="submitting" class="mr-2 h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent motion-reduce:animate-none" aria-hidden="true"></span>
          {{ submitting ? t('admin.affiliates.reversal.submitting') : t('admin.affiliates.reversal.confirmAction') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  affiliatesAPI,
  type AffiliateReversalPreview,
  type AffiliateReversalResult,
} from '@/api/admin/affiliates'
import { extractI18nErrorMessage } from '@/utils/apiError'

const props = defineProps<{
  show: boolean
  orderIds: number[]
}>()

const emit = defineEmits<{
  close: []
  completed: [result: AffiliateReversalResult]
  'busy-change': [busy: boolean]
}>()

const { t } = useI18n()
const loading = ref(false)
const submitting = ref(false)
const preview = ref<AffiliateReversalPreview | null>(null)
const previewError = ref('')
const submitError = ref('')
const reason = ref('')
const confirmNegative = ref(false)
const idempotencyKey = ref<string | null>(null)
let requestSequence = 0

const canSubmit = computed(() =>
  !!preview.value
  && !loading.value
  && !submitting.value
  && reason.value.trim().length > 0
  && (!preview.value.has_negative_balance || confirmNegative.value),
)

watch(
  () => props.show,
  show => {
    if (show) {
      resetDialog()
      void loadPreview()
    } else {
      requestSequence++
    }
  },
  { immediate: true },
)

function resetDialog() {
  preview.value = null
  previewError.value = ''
  submitError.value = ''
  reason.value = ''
  confirmNegative.value = false
  idempotencyKey.value = null
}

async function loadPreview() {
  const sequence = ++requestSequence
  loading.value = true
  previewError.value = ''
  preview.value = null
  confirmNegative.value = false
  idempotencyKey.value = null
  try {
    const result = await affiliatesAPI.previewRebateReversal(props.orderIds)
    if (sequence !== requestSequence || !props.show) return
    preview.value = result
  } catch (error) {
    if (sequence !== requestSequence || !props.show) return
    previewError.value = extractI18nErrorMessage(
      error,
      t,
      'admin.affiliates.reversal.errors',
      t('admin.affiliates.reversal.previewFailed'),
    )
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

async function submitReversal() {
  if (!canSubmit.value || !preview.value) return
  submitting.value = true
  submitError.value = ''
  emit('busy-change', true)
  if (!idempotencyKey.value) {
    idempotencyKey.value = globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`
  }
  try {
    const result = await affiliatesAPI.reverseRebates({
      order_ids: props.orderIds,
      preview_token: preview.value.preview_token,
      reason: reason.value.trim(),
      confirm_negative_balance: confirmNegative.value,
    }, idempotencyKey.value)
    emit('completed', result)
  } catch (error) {
    submitError.value = extractI18nErrorMessage(
      error,
      t,
      'admin.affiliates.reversal.errors',
      t('admin.affiliates.reversal.submitFailed'),
    )
  } finally {
    submitting.value = false
    emit('busy-change', false)
  }
}

function handleClose() {
  if (submitting.value) return
  emit('close')
}

function formatAmount(value: number): string {
  return Number(value || 0).toFixed(2)
}
</script>
