<template>
  <template v-if="subscription.manual_bulk_quota_reset_editable">
    <button
      type="button"
      :disabled="updating"
      :aria-pressed="subscription.manual_bulk_quota_reset_enabled"
      class="flex min-h-11 min-w-11 flex-col items-center gap-0.5 rounded-lg p-1.5 transition-colors disabled:cursor-not-allowed disabled:opacity-50"
      :class="subscription.manual_bulk_quota_reset_enabled
        ? 'text-green-600 hover:bg-green-50 dark:text-green-400 dark:hover:bg-green-900/20'
        : 'text-gray-500 hover:bg-purple-50 hover:text-purple-600 dark:hover:bg-purple-900/20 dark:hover:text-purple-400'"
      data-testid="manual-bulk-reset-eligibility-action"
      @click="showConfirm = true"
    >
      <Icon :name="subscription.manual_bulk_quota_reset_enabled ? 'check' : 'ban'" size="sm" />
      <span class="text-xs">
        {{ t(subscription.manual_bulk_quota_reset_enabled
          ? 'admin.subscriptions.manualEligibility.actionEnabled'
          : 'admin.subscriptions.manualEligibility.actionDisabled') }}
      </span>
    </button>

    <ConfirmDialog
      :show="showConfirm"
      :title="t('admin.subscriptions.manualEligibility.title')"
      :message="t(
        subscription.manual_bulk_quota_reset_enabled
          ? 'admin.subscriptions.manualEligibility.disableConfirm'
          : 'admin.subscriptions.manualEligibility.enableConfirm',
        { user: subscription.user?.email }
      )"
      :confirm-text="t(
        subscription.manual_bulk_quota_reset_enabled
          ? 'admin.subscriptions.manualEligibility.disable'
          : 'admin.subscriptions.manualEligibility.enable'
      )"
      :cancel-text="t('common.cancel')"
      @confirm="confirmUpdate"
      @cancel="cancelUpdate"
    />
  </template>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { UserSubscription } from '@/types'
import { updateCurrentCycleBulkResetEligibility } from '@/api/admin/subscriptions'
import { useAppStore } from '@/stores/app'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'

const props = defineProps<{ subscription: UserSubscription }>()

const emit = defineEmits<{
  updated: []
}>()

const { t } = useI18n()
const appStore = useAppStore()
const showConfirm = ref(false)
const updating = ref(false)

function cancelUpdate() {
  if (updating.value) return
  showConfirm.value = false
}

async function confirmUpdate() {
  if (updating.value) return
  updating.value = true
  const enabled = !props.subscription.manual_bulk_quota_reset_enabled
  try {
    await updateCurrentCycleBulkResetEligibility(props.subscription.id, enabled)
    appStore.showSuccess(t(enabled
      ? 'admin.subscriptions.manualEligibility.enabledSuccess'
      : 'admin.subscriptions.manualEligibility.disabledSuccess'))
    showConfirm.value = false
    emit('updated')
  } catch (error: any) {
    appStore.showError(error.response?.data?.detail || t('admin.subscriptions.manualEligibility.updateFailed'))
    console.error('Error updating bulk reset eligibility:', error)
  } finally {
    updating.value = false
  }
}
</script>
