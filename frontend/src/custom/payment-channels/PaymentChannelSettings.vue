<template>
  <section id="payment-channel-settings" class="card" aria-labelledby="payment-channel-settings-title">
    <div class="border-b border-gray-100 px-4 py-4 dark:border-dark-700 sm:px-6">
      <h2
        id="payment-channel-settings-title"
        class="text-lg font-semibold text-gray-900 dark:text-white"
      >
        {{ copy.title }}
      </h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">
        {{ copy.description }}
      </p>
    </div>

    <div class="space-y-3 p-4 sm:p-6">
      <div
        v-if="channels.length === 0"
        class="rounded-lg border border-dashed border-gray-300 px-4 py-6 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
      >
        {{ copy.empty }}
      </div>

      <div
        v-for="channel in channels"
        :key="channel.id"
        class="grid grid-cols-1 gap-4 rounded-xl border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-800/60 md:grid-cols-[minmax(180px,0.8fr)_minmax(220px,1fr)_minmax(190px,0.7fr)] md:items-start"
        :data-test="`channel-setting-${channel.id}`"
      >
        <div class="flex min-w-0 items-center gap-3">
          <img
            :src="methodIcon(channel.paymentType)"
            alt=""
            class="h-9 w-9 shrink-0 object-contain"
          />
          <div class="min-w-0">
            <p class="truncate text-sm font-semibold text-gray-900 dark:text-white">
              {{ channelLabel(channel) }}
            </p>
            <div class="mt-1 flex flex-wrap items-center gap-2">
              <span
                :class="[
                  'inline-flex rounded-full px-2 py-0.5 text-xs font-medium',
                  channel.enabled
                    ? 'bg-green-100 text-green-700 dark:bg-green-900/30 dark:text-green-300'
                    : 'bg-gray-100 text-gray-600 dark:bg-dark-700 dark:text-dark-300',
                ]"
              >
                {{ channel.enabled ? copy.enabled : copy.disabled }}
              </span>
              <span class="text-xs text-gray-400 dark:text-dark-400">
                {{ copy.instances(channel.instanceCount) }}
              </span>
            </div>
          </div>
        </div>

        <div>
          <label
            class="input-label"
            :for="`${channel.id}-display-name`"
          >
            {{ copy.displayName }}
          </label>
          <input
            :id="`${channel.id}-display-name`"
            :value="drafts[channel.id]?.displayName || ''"
            type="text"
            class="input"
            :class="{ 'border-red-500 focus:border-red-500 focus:ring-red-500/30': errors[channel.id]?.displayName }"
            :placeholder="channelLabel(channel)"
            :aria-invalid="!!errors[channel.id]?.displayName"
            :aria-describedby="`${channel.id}-display-name-help`"
            :data-test="`channel-display-name-${channel.id}`"
            @input="updateDisplayName(channel.id, ($event.target as HTMLInputElement).value)"
          />
          <p
            v-if="errors[channel.id]?.displayName"
            :id="`${channel.id}-display-name-help`"
            class="mt-1 text-xs text-red-600 dark:text-red-400"
            role="alert"
          >
            {{ errors[channel.id]?.displayName }}
          </p>
          <p
            v-else
            :id="`${channel.id}-display-name-help`"
            class="mt-1 text-xs text-gray-400 dark:text-dark-400"
          >
            {{ copy.displayNameHint }}
          </p>
        </div>

        <div>
          <label
            class="input-label"
            :for="`${channel.id}-fee-rate`"
          >
            {{ copy.feeRate }}
          </label>
          <div class="relative">
            <input
              :id="`${channel.id}-fee-rate`"
              :value="drafts[channel.id]?.feeRate || ''"
              type="text"
              inputmode="decimal"
              autocomplete="off"
              class="input pr-8"
              :class="{ 'border-red-500 focus:border-red-500 focus:ring-red-500/30': errors[channel.id]?.feeRate }"
              :placeholder="copy.inheritPlaceholder(defaultFeeRate)"
              :aria-invalid="!!errors[channel.id]?.feeRate"
              :aria-describedby="`${channel.id}-fee-rate-help`"
              :data-test="`channel-fee-rate-${channel.id}`"
              @input="updateFeeRate(channel.id, ($event.target as HTMLInputElement).value)"
            />
            <span
              class="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-3 text-gray-400"
            >
              %
            </span>
          </div>
          <p
            v-if="errors[channel.id]?.feeRate"
            :id="`${channel.id}-fee-rate-help`"
            class="mt-1 text-xs text-red-600 dark:text-red-400"
            role="alert"
          >
            {{ errors[channel.id]?.feeRate }}
          </p>
          <p
            v-else
            :id="`${channel.id}-fee-rate-help`"
            class="mt-1 text-xs"
            :class="drafts[channel.id]?.feeRate !== '' && Number(drafts[channel.id]?.feeRate) === 0
              ? 'font-medium text-green-600 dark:text-green-400'
              : 'text-gray-400 dark:text-dark-400'"
          >
            {{ feeRateHint(channel.id) }}
          </p>
        </div>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { PaymentChannelSettings } from '@/api/admin/payment'
import type { ProviderInstance } from '@/types/payment'
import { paymentChannelLabel, type PaymentChannelOption } from './paymentChannels'
import { aggregateAdminPaymentChannels, type AdminPaymentChannel } from './adminPaymentChannels'
import alipayIcon from '@/assets/icons/alipay.svg'
import wxpayIcon from '@/assets/icons/wxpay.svg'
import stripeIcon from '@/assets/icons/stripe.svg'
import airwallexIcon from '@/assets/icons/airwallex.svg'
import paymentIcon from '@/assets/icons/payment.svg'

interface ChannelDraft {
  displayName: string
  feeRate: string
}

interface ChannelErrors {
  displayName?: string
  feeRate?: string
}

const props = defineProps<{
  modelValue: PaymentChannelSettings
  providers: ProviderInstance[]
  defaultFeeRate: number
}>()

const emit = defineEmits<{
  'update:modelValue': [settings: PaymentChannelSettings]
}>()

const { locale, t } = useI18n()
const drafts = reactive<Record<string, ChannelDraft>>({})
const errors = reactive<Record<string, ChannelErrors>>({})
const channels = computed(() => aggregateAdminPaymentChannels(props.providers))
const isZh = computed(() => locale.value.toLowerCase().startsWith('zh'))

const copy = computed(() => isZh.value ? {
  title: '用户渠道配置',
  description: '按用户实际看到的聚合渠道设置名称和手续费。留空时使用系统名称并继承默认支付手续费率。',
  empty: '创建支付服务商后，可在这里配置用户看到的渠道。',
  enabled: '当前可用',
  disabled: '已禁用',
  instances: (count: number) => `${count} 个实例`,
  displayName: '用户显示名称',
  displayNameHint: '仅用于充值、订阅选择器和备用渠道提示。',
  feeRate: '手续费率',
  inheritPlaceholder: (rate: number) => `继承默认 ${formatRate(rate)}%`,
  inheritHint: (rate: number) => `留空继承默认 ${formatRate(rate)}%`,
  freeHint: '免手续费',
  customFeeHint: (rate: string) => `使用 ${rate}%`,
  invalidName: '名称最多 100 个字符，且不能包含控制字符。',
  invalidFee: '请输入 0–100，最多两位小数。',
} : {
  title: 'User channel settings',
  description: 'Customize names and fees for provider-grouped channels shown to users. Empty values use the system name and default fee.',
  empty: 'Create a payment provider to configure its user-facing channel here.',
  enabled: 'Available',
  disabled: 'Disabled',
  instances: (count: number) => `${count} instance${count === 1 ? '' : 's'}`,
  displayName: 'User-facing name',
  displayNameHint: 'Used only in recharge, subscription, and backup-channel prompts.',
  feeRate: 'Fee rate',
  inheritPlaceholder: (rate: number) => `Inherit default ${formatRate(rate)}%`,
  inheritHint: (rate: number) => `Empty inherits default ${formatRate(rate)}%`,
  freeHint: 'No fee',
  customFeeHint: (rate: string) => `Use ${rate}%`,
  invalidName: 'Use at most 100 characters and no control characters.',
  invalidFee: 'Enter 0–100 with at most two decimal places.',
})

watch(
  [channels, () => props.modelValue],
  () => {
    const visibleIDs = new Set(channels.value.map(channel => channel.id))
    for (const channel of channels.value) {
      const setting = props.modelValue?.[channel.id]
      drafts[channel.id] = {
        displayName: setting?.display_name || '',
        feeRate: setting?.fee_rate === null || setting?.fee_rate === undefined
          ? ''
          : String(setting.fee_rate),
      }
      errors[channel.id] = {}
    }
    for (const channelID of Object.keys(drafts)) {
      if (!visibleIDs.has(channelID)) {
        delete drafts[channelID]
        delete errors[channelID]
      }
    }
  },
  { immediate: true, deep: true },
)

function updateDisplayName(channelID: string, value: string) {
  drafts[channelID].displayName = value
  errors[channelID].displayName = validateDisplayName(value)
  emitChannelSetting(channelID)
}

function updateFeeRate(channelID: string, value: string) {
  drafts[channelID].feeRate = value.trim()
  errors[channelID].feeRate = validateFeeRate(drafts[channelID].feeRate)
  emitChannelSetting(channelID)
}

function emitChannelSetting(channelID: string) {
  const draft = drafts[channelID]
  if (!draft || errors[channelID]?.displayName || errors[channelID]?.feeRate) return

  const next: PaymentChannelSettings = { ...(props.modelValue || {}) }
  const displayName = draft.displayName.trim()
  const feeRate = draft.feeRate === '' ? undefined : Number(draft.feeRate)
  if (!displayName && feeRate === undefined) {
    delete next[channelID]
  } else {
    next[channelID] = {
      ...(displayName ? { display_name: displayName } : {}),
      ...(feeRate === undefined ? {} : { fee_rate: feeRate }),
    }
  }
  emit('update:modelValue', next)
}

function validateDisplayName(value: string): string | undefined {
  const normalized = value.trim()
  if (
    Array.from(normalized).length > 100
    || containsControlCharacter(value)
  ) {
    return copy.value.invalidName
  }
  return undefined
}

function containsControlCharacter(value: string): boolean {
  return Array.from(value).some((character) => {
    const codePoint = character.codePointAt(0) ?? 0
    return codePoint <= 0x1f || (codePoint >= 0x7f && codePoint <= 0x9f)
  })
}

function validateFeeRate(value: string): string | undefined {
  if (value === '') return undefined
  if (!/^\d{1,3}(?:\.\d{1,2})?$/.test(value)) return copy.value.invalidFee
  const rate = Number(value)
  if (!Number.isFinite(rate) || rate < 0 || rate > 100) return copy.value.invalidFee
  return undefined
}

function validate(): boolean {
  let valid = true
  for (const channel of channels.value) {
    const draft = drafts[channel.id]
    const displayNameError = validateDisplayName(draft?.displayName || '')
    const feeRateError = validateFeeRate(draft?.feeRate || '')
    errors[channel.id] = {
      displayName: displayNameError,
      feeRate: feeRateError,
    }
    valid &&= !displayNameError && !feeRateError
  }
  if (!valid) {
    requestAnimationFrame(() => {
      document.querySelector<HTMLElement>(
        '#payment-channel-settings [aria-invalid="true"]',
      )?.focus()
    })
  }
  return valid
}

function channelLabel(channel: AdminPaymentChannel): string {
  return paymentChannelLabel({
    id: channel.id,
    payment_type: channel.paymentType,
    provider_key: channel.providerKey,
    display_name: channel.displayName,
    fee_rate: 0,
    daily_limit: 0,
    single_min: 0,
    single_max: 0,
    available: channel.enabled,
  } satisfies PaymentChannelOption, t)
}

function feeRateHint(channelID: string): string {
  const value = drafts[channelID]?.feeRate || ''
  if (value === '') return copy.value.inheritHint(props.defaultFeeRate)
  if (Number(value) === 0) return copy.value.freeHint
  return copy.value.customFeeHint(value)
}

function methodIcon(paymentType: string): string {
  if (paymentType === 'alipay') return alipayIcon
  if (paymentType === 'wxpay') return wxpayIcon
  if (paymentType === 'stripe') return stripeIcon
  if (paymentType === 'airwallex') return airwallexIcon
  return paymentIcon
}

function formatRate(rate: number): string {
  return Number.isFinite(Number(rate)) ? String(Number(rate)) : '0'
}

defineExpose({ validate })
</script>
