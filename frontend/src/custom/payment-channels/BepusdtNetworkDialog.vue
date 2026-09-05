<template>
  <BaseDialog
    :show="show"
    :title="t('payment.bepusdt.networkDialogTitle')"
    width="narrow"
    @close="emit('cancel')"
  >
    <div class="space-y-4">
      <div class="rounded-lg border border-primary-100 bg-primary-50 px-3 py-2 text-sm text-primary-800 dark:border-primary-900/50 dark:bg-primary-950/40 dark:text-primary-200">
        <span class="text-gray-500 dark:text-dark-400">{{ t('payment.bepusdt.currency') }}</span>
        <span class="ml-2 font-semibold">USDT</span>
      </div>
      <div>
        <label for="bepusdt-network" class="input-label">
          {{ t('payment.bepusdt.network') }}
        </label>
        <select
          id="bepusdt-network"
          v-model="selected"
          class="input"
          :disabled="networks.length === 0"
        >
          <option v-for="network in networks" :key="network.code" :value="network.code">
            {{ network.display_name }}
          </option>
        </select>
        <p v-if="networks.length === 0" class="mt-2 text-xs text-red-600 dark:text-red-400">
          {{ t('payment.bepusdt.noNetworks') }}
        </p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-3">
        <button type="button" class="btn btn-secondary" @click="emit('cancel')">
          {{ t('common.cancel') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="!selected || networks.length === 0"
          @click="confirm"
        >
          {{ t('common.confirm') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'

export interface BepusdtNetworkOption {
  code: string
  display_name: string
}

const props = defineProps<{
  show: boolean
  networks: BepusdtNetworkOption[]
}>()

const emit = defineEmits<{
  cancel: []
  confirm: [network: string]
}>()

const { t } = useI18n()
const selected = ref('')

watch(
  [() => props.show, () => props.networks],
  ([show, networks]) => {
    if (!show) return
    const preferred = networks.find(network => network.code === 'bep20')
    selected.value = preferred?.code || networks[0]?.code || ''
  },
  { immediate: true, deep: true },
)

function confirm() {
  if (selected.value) emit('confirm', selected.value)
}
</script>
