<template>
  <AppLayout>
    <div class="mx-auto max-w-4xl space-y-6">
      <div v-if="loading" class="flex items-center justify-center py-20">
        <div class="h-8 w-8 animate-spin rounded-full border-4 border-primary-500 border-t-transparent"></div>
      </div>
      <template v-else>
        <!-- Tab Switcher (hide during payment and subscription confirm) -->
        <div v-if="tabs.length > 1 && paymentPhase === 'select' && !selectedPlan" class="flex space-x-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-800">
          <button v-for="tab in tabs" :key="tab.key"
            class="flex-1 rounded-lg px-4 py-2.5 text-sm font-medium transition-all"
            :class="activeTab === tab.key ? 'bg-white text-gray-900 shadow dark:bg-dark-700 dark:text-white' : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300'"
            @click="activeTab = tab.key">{{ tab.label }}</button>
        </div>
        <!-- Payment in progress (shared by recharge and subscription) -->
        <template v-if="paymentPhase === 'paying'">
          <PaymentStatusPanel
            :order-id="paymentState.orderId"
            :amount="paymentState.amount"
            :pay-amount="paymentState.payAmount"
            :qr-code="paymentState.qrCode"
            :expires-at="paymentState.expiresAt"
            :payment-type="paymentState.paymentType"
            :pay-url="paymentState.payUrl"
            :order-type="paymentState.orderType"
            :currency="paymentState.currency || selectedCurrency"
            :out-trade-no="paymentState.outTradeNo"
            :mobile-alipay-deep-link="paymentState.alipayMobilePrecreateDeepLink"
            @done="onPaymentDone"
            @success="onPaymentSuccess"
            @settled="onPaymentSettled"
          />
        </template>
        <!-- Tab content (select phase) -->
        <template v-else>
          <!-- Top-up Tab -->
          <template v-if="activeTab === 'recharge'">
            <!-- Recharge Account Card -->
            <div class="card p-5">
              <p class="text-xs font-medium text-gray-400 dark:text-gray-500">{{ t('payment.rechargeAccount') }}</p>
              <p class="mt-1 text-base font-semibold text-gray-900 dark:text-white">{{ user?.username || '' }}</p>
              <p class="mt-0.5 text-sm font-medium text-green-600 dark:text-green-400">{{ t('payment.currentBalance') }}: {{ user?.balance?.toFixed(2) || '0.00' }}</p>
            </div>
            <div v-if="enabledChannelIds.length === 0" class="card py-16 text-center">
              <p class="text-gray-500 dark:text-gray-400">{{ t('payment.notAvailable') }}</p>
            </div>
            <template v-else>
            <div class="card p-6">
              <AmountInput
                v-model="amount"
                :amounts="[10, 20, 50, 100, 200, 500, 1000, 2000, 5000]"
                :currency="selectedCurrency"
                :min="globalMinAmount"
                :max="globalMaxAmount"
              />
              <p v-if="amountError" class="mt-2 text-xs text-amber-600 dark:text-amber-300">{{ amountError }}</p>
            </div>
            <div v-if="enabledChannelIds.length >= 1" class="card p-6">
              <PaymentChannelSelector
                :methods="methodOptions"
                :selected="selectedChannelId"
                @select="selectedChannelId = $event"
              />
            </div>
            <div v-if="validAmount > 0" class="card p-6">
              <div class="space-y-2 text-sm">
                <div class="flex justify-between">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.paymentAmount') }}</span>
                  <span class="text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(validAmount) }}</span>
                </div>
                <div v-if="feeRate > 0" class="flex justify-between">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.fee') }} ({{ feeRate }}%)</span>
                  <span class="text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(feeAmount) }}</span>
                </div>
                <div v-if="feeRate > 0" class="flex justify-between border-t border-gray-200 pt-2 dark:border-dark-600">
                  <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('payment.actualPay') }}</span>
                  <span class="text-lg font-bold text-primary-600 dark:text-primary-400">{{ formatSelectedPaymentAmount(totalAmount) }}</span>
                </div>
                <div v-if="balanceRechargeMultiplier !== 1" class="flex justify-between" :class="{ 'border-t border-gray-200 pt-2 dark:border-dark-600': feeRate <= 0 }">
                  <span class="text-gray-500 dark:text-gray-400">{{ t('payment.creditedBalance') }}</span>
                  <span class="text-gray-900 dark:text-white">${{ creditedAmount.toFixed(2) }}</span>
                </div>
                <p v-if="balanceRechargeMultiplier !== 1" class="border-t border-gray-200 pt-2 text-xs text-gray-500 dark:border-dark-600 dark:text-gray-400">
                  {{ t('payment.rechargeRatePreview', { currency: selectedCurrency, usd: balanceRechargeMultiplier.toFixed(2) }) }}
                </p>
              </div>
            </div>
            <button :class="['btn w-full py-3 text-base font-medium', paymentButtonClass]" :disabled="!canSubmit || submitting" @click="handleSubmitRecharge">
              <span v-if="submitting" class="flex items-center justify-center gap-2">
                <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                {{ t('common.processing') }}
              </span>
              <span v-else>{{ t('payment.createOrder') }} {{ formatSelectedPaymentAmount(totalAmount) }}</span>
            </button>
            </template>
          </template>
          <!-- Subscribe Tab -->
          <template v-else-if="activeTab === 'subscription'">
            <!-- Subscription confirm (inline, replaces plan list) -->
            <template v-if="selectedPlan">
              <div class="card p-5">
                <!-- Header: platform badge + plan name -->
                <div class="mb-3 flex flex-wrap items-center gap-2">
                  <span :class="['rounded-md border px-2 py-0.5 text-xs font-medium', planBadgeClass]">
                    {{ platformLabel(selectedPlan.group_platform || '') }}
                  </span>
                  <h3 class="text-lg font-bold text-gray-900 dark:text-white">{{ selectedPlan.name }}</h3>
                </div>
                <!-- Price -->
                <div class="flex items-baseline gap-2">
                  <span v-if="selectedPlan.original_price" class="text-sm text-gray-400 line-through dark:text-gray-500">
                    {{ formatSelectedSubscriptionPaymentAmount(selectedPlan.original_price) }}
                  </span>
                  <span :class="['text-3xl font-bold', planTextClass]">{{ formatSelectedSubscriptionPaymentAmount(selectedPlan.price) }}</span>
                  <span class="text-sm text-gray-500 dark:text-gray-400">/ {{ planValiditySuffix }}</span>
                </div>
                <!-- Description -->
                <p v-if="selectedPlan.description" class="mt-2 text-sm leading-relaxed text-gray-500 dark:text-gray-400">
                  {{ selectedPlan.description }}
                </p>
                <!-- Rate + Limits grid -->
                <div class="mt-3 grid grid-cols-2 gap-3">
                  <div>
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.rate') }}</span>
                    <div class="flex items-baseline">
                      <span :class="['text-lg font-bold', planTextClass]">×{{ selectedPlan.rate_multiplier ?? 1 }}</span>
                    </div>
                  </div>
                  <div v-if="planHasPeakRate(selectedPlan)">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.peakRate') }}</span>
                    <div class="text-sm font-semibold text-amber-700 dark:text-amber-300">
                      {{ planPeakRateLabel(selectedPlan) }}
                    </div>
                  </div>
                  <div v-if="selectedPlan.daily_limit_usd != null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.dailyLimit') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">${{ selectedPlan.daily_limit_usd }}</div>
                  </div>
                  <div v-if="selectedPlan.weekly_limit_usd != null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.weeklyLimit') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">${{ selectedPlan.weekly_limit_usd }}</div>
                  </div>
                  <div v-if="selectedPlan.monthly_limit_usd != null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.monthlyLimit') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">${{ selectedPlan.monthly_limit_usd }}</div>
                  </div>
                  <div v-if="selectedPlan.daily_limit_usd == null && selectedPlan.weekly_limit_usd == null && selectedPlan.monthly_limit_usd == null">
                    <span class="text-xs text-gray-400 dark:text-gray-500">{{ t('payment.planCard.quota') }}</span>
                    <div class="text-lg font-semibold text-gray-800 dark:text-gray-200">{{ t('payment.planCard.unlimited') }}</div>
                  </div>
                </div>
              </div>
              <div v-if="enabledChannelIds.length >= 1" class="card p-6">
                <PaymentChannelSelector
                  :methods="subMethodOptions"
                  :selected="selectedChannelId"
                  @select="selectedChannelId = $event"
                />
              </div>
              <div v-if="feeRate > 0 && selectedPlan.price > 0" class="card p-6">
                <div class="space-y-2 text-sm">
                  <div class="flex justify-between">
                    <span class="text-gray-500 dark:text-gray-400">{{ t('payment.amountLabel') }}</span>
                    <span class="text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(subPaymentAmount) }}</span>
                  </div>
                  <div class="flex justify-between">
                    <span class="text-gray-500 dark:text-gray-400">{{ t('payment.fee') }} ({{ feeRate }}%)</span>
                    <span class="text-gray-900 dark:text-white">{{ formatSelectedPaymentAmount(subFeeAmount) }}</span>
                  </div>
                  <div class="flex justify-between border-t border-gray-200 pt-2 dark:border-dark-600">
                    <span class="font-medium text-gray-700 dark:text-gray-300">{{ t('payment.actualPay') }}</span>
                    <span class="text-lg font-bold text-primary-600 dark:text-primary-400">{{ formatSelectedPaymentAmount(subTotalAmount) }}</span>
                  </div>
                </div>
              </div>
              <button :class="['btn w-full py-3 text-base font-medium', paymentButtonClass]" :disabled="!canSubmitSubscription || submitting || !isPlanPurchasable(selectedPlan)" @click="confirmSubscribe">
                <span v-if="submitting" class="flex items-center justify-center gap-2">
                  <span class="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent"></span>
                  {{ t('common.processing') }}
                </span>
                <span v-else>{{ t('payment.createOrder') }} {{ formatSelectedPaymentAmount(subTotalAmount) }}</span>
              </button>
              <button class="btn btn-secondary w-full" @click="selectedPlan = null">{{ t('common.cancel') }}</button>
            </template>
            <!-- Plan list -->
            <template v-else>
              <div v-if="checkout.plans.length === 0" class="card py-16 text-center">
                <Icon name="gift" size="xl" class="mx-auto mb-3 text-gray-300 dark:text-dark-600" />
                <p class="text-gray-500 dark:text-gray-400">{{ t('payment.noPlans') }}</p>
              </div>
              <div v-else :class="planGridClass">
                <SubscriptionPlanCard v-for="plan in checkout.plans" :key="plan.id" :plan="plan" :active-subscriptions="activeSubscriptions" @select="selectPlan" />
              </div>
              <!-- Active subscriptions (compact, below plan list) -->
              <div v-if="activeSubscriptions.length > 0">
                <p class="mb-2 text-xs font-medium text-gray-400 dark:text-gray-500">{{ t('payment.activeSubscription') }}</p>
                <div class="space-y-2">
                  <div v-for="sub in activeSubscriptions" :key="sub.id"
                    class="flex items-center gap-3 rounded-xl border border-gray-100 bg-white px-3 py-2 dark:border-dark-700 dark:bg-dark-800">
                    <div :class="['h-6 w-1 shrink-0 rounded-full', platformAccentBarClass(sub.group?.platform || '')]" />
                    <div class="min-w-0 flex-1">
                      <div class="flex items-center gap-1.5">
                        <span class="truncate text-xs font-semibold text-gray-900 dark:text-white">{{ sub.group?.name || t('payment.groupFallback', { id: sub.group_id }) }}</span>
                        <span :class="['shrink-0 rounded-full px-1.5 py-0.5 text-[9px] font-medium', platformBadgeLightClass(sub.group?.platform || '')]">{{ platformLabel(sub.group?.platform || '') }}</span>
                      </div>
                      <div class="flex flex-wrap gap-x-3 text-[11px] text-gray-400 dark:text-gray-500">
                        <span>{{ t('payment.planCard.rate') }}: ×{{ sub.group?.rate_multiplier ?? 1 }}</span>
                        <span v-if="subscriptionHasPeakRate(sub)">{{ t('payment.planCard.peakRate') }}: {{ subscriptionPeakRateLabel(sub) }}</span>
                        <span v-if="sub.group?.daily_limit_usd == null && sub.group?.weekly_limit_usd == null && sub.group?.monthly_limit_usd == null">{{ t('payment.planCard.quota') }}: {{ t('payment.planCard.unlimited') }}</span>
                        <span v-if="sub.expires_at">{{ t('userSubscriptions.daysRemaining', { days: getDaysRemaining(sub.expires_at) }) }}</span>
                        <span v-else>{{ t('userSubscriptions.noExpiration') }}</span>
                      </div>
                    </div>
                    <span class="badge badge-success shrink-0 text-[10px]">{{ t('userSubscriptions.status.active') }}</span>
                  </div>
                </div>
              </div>
            </template>
          </template>
        </template>
        <div v-if="(checkout.help_text || checkout.help_image_url) && paymentPhase === 'select' && !selectedPlan" class="card p-4">
          <div class="flex flex-col items-center gap-3">
            <img v-if="checkout.help_image_url" :src="checkout.help_image_url" alt=""
              class="h-40 max-w-full cursor-pointer rounded-lg object-contain transition-opacity hover:opacity-80"
              @click="previewImage = checkout.help_image_url" />
            <p v-if="checkout.help_text" class="text-center text-sm text-gray-500 dark:text-gray-400">{{ checkout.help_text }}</p>
          </div>
        </div>
      </template>
    </div>
    <!-- Renewal Plan Selection Modal -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="showRenewalModal" class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4" @click.self="closeRenewalModal">
          <div class="relative w-full max-w-lg rounded-2xl border border-gray-200 bg-white p-6 shadow-2xl dark:border-dark-700 dark:bg-dark-900">
            <!-- Close button -->
            <button class="absolute right-4 top-4 rounded-lg p-1 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 dark:hover:bg-dark-700 dark:hover:text-gray-200" @click="closeRenewalModal">
              <svg class="h-5 w-5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2"><path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" /></svg>
            </button>
            <h3 class="mb-4 text-lg font-semibold text-gray-900 dark:text-white">{{ t('payment.selectPlan') }}</h3>
            <div class="space-y-4">
              <SubscriptionPlanCard v-for="plan in renewalPlans" :key="plan.id" :plan="plan" :active-subscriptions="activeSubscriptions" @select="selectPlanFromModal" />
            </div>
          </div>
        </div>
      </Transition>
    </Teleport>
    <!-- Image Preview Overlay -->
    <Teleport to="body">
      <Transition name="modal">
        <div v-if="previewImage" class="fixed inset-0 z-[60] flex items-center justify-center bg-black/70 backdrop-blur-sm" @click="previewImage = ''">
          <img :src="previewImage" alt="" class="max-h-[85vh] max-w-[90vw] rounded-xl object-contain shadow-2xl" />
        </div>
      </Transition>
    </Teleport>
    <BepusdtNetworkDialog
      :show="showBepusdtNetworkDialog"
      :networks="selectedChannel?.network_options || []"
      @cancel="cancelBepusdtNetworkSelection"
      @confirm="confirmBepusdtNetworkSelection"
    />
  </AppLayout>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { usePaymentStore } from '@/stores/payment'
import { useSubscriptionStore } from '@/stores/subscriptions'
import { useAppStore } from '@/stores'
import { paymentAPI } from '@/api/payment'
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/utils/apiError'
import { isMobileDevice } from '@/utils/device'
import { hasPeakRate, formatPeakRateWindow, serverTimezoneLabel, type PeakRateFields } from '@/utils/peak-rate'
import type { SubscriptionPlan, CheckoutInfoResponse, CreateOrderResult, OrderType } from '@/types/payment'
import AppLayout from '@/components/layout/AppLayout.vue'
import AmountInput from '@/components/payment/AmountInput.vue'
import { getPaymentPopupFeatures, isBuiltInAlipayMethod, isBuiltInWxpayMethod } from '@/components/payment/providerConfig'
import {
  PAYMENT_RECOVERY_STORAGE_KEY,
  buildCreateOrderPayload,
  clearPaymentRecoverySnapshot,
  decidePaymentLaunch,
  normalizeVisibleMethod,
  readPaymentRecoverySnapshot,
  type PaymentRecoverySnapshot,
  writePaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import { platformAccentBarClass, platformBadgeLightClass, platformBadgeClass, platformTextClass, platformLabel } from '@/utils/platformColors'
import SubscriptionPlanCard from '@/components/payment/SubscriptionPlanCard.vue'
import PaymentStatusPanel from '@/components/payment/PaymentStatusPanel.vue'
import Icon from '@/components/icons/Icon.vue'
import { planValiditySuffix as validitySuffixOf } from '@/components/payment/validity'
import PaymentChannelSelector from '@/custom/payment-channels/PaymentChannelSelector.vue'
import BepusdtNetworkDialog from '@/custom/payment-channels/BepusdtNetworkDialog.vue'
import {
  ALIPAY_MOBILE_PRECREATE_DEEP_LINK,
  findPaymentChannel,
  paymentChannelSupports,
} from '@/custom/payment-channels/paymentChannels'
import { usePaymentChannelPricing } from '@/custom/payment-channels/usePaymentChannelPricing'
import {
  isPlanPurchasable,
  planAvailabilityError,
  synchronizePlanAvailability,
} from '@/custom/subscription-plan-inventory/inventory'
import {
  usePaymentChannelRecovery,
  type BackupChannelHintContext,
} from '@/custom/payment-channels/usePaymentChannelRecovery'
import { hasWechatResumeQuery } from '@/custom/payment-channels/paymentRecoveryRoute'
import { buildPaymentErrorToastMessage, describePaymentScenarioError } from './paymentUx'

const i18n = useI18n()
const { t } = i18n
const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const paymentStore = usePaymentStore()
const subscriptionStore = useSubscriptionStore()
const appStore = useAppStore()

const user = computed(() => authStore.user)
const activeSubscriptions = computed(() => subscriptionStore.activeSubscriptions)

function getDaysRemaining(expiresAt: string): number {
  const diff = new Date(expiresAt).getTime() - Date.now()
  return Math.max(0, Math.ceil(diff / (1000 * 60 * 60 * 24)))
}

function subscriptionHasPeakRate(sub: { group?: PeakRateFields | null }): boolean {
  return hasPeakRate(sub.group)
}

function subscriptionPeakRateLabel(sub: { group?: PeakRateFields | null }): string {
  return formatPeakRateWindow(sub.group, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
}

const loading = ref(true)
const submitting = ref(false)
const errorMessage = ref('')
const errorHintMessage = ref('')
const activeTab = ref<'recharge' | 'subscription'>('recharge')
const amount = ref<number | null>(null)
const selectedChannelId = ref('')
const selectedPlan = ref<SubscriptionPlan | null>(null)
const previewImage = ref('')

const paymentPhase = ref<'select' | 'paying'>('select')

interface CreateOrderOptions {
  openid?: string
  wechatResumeToken?: string
  paymentType?: string
  providerKey?: string
  paymentChannelId?: string
  isResume?: boolean
  mobileQrFallbackAttempted?: boolean
  paymentNetwork?: string
}

interface WeixinJSBridgeLike {
  invoke(
    action: string,
    payload: Record<string, unknown>,
    callback: (result: Record<string, unknown>) => void,
  ): void
}

function emptyPaymentState(): PaymentRecoverySnapshot {
  return {
    orderId: 0,
    amount: 0,
    qrCode: '',
    expiresAt: '',
    paymentType: '',
    paymentChannelId: '',
    providerKey: '',
    payUrl: '',
    outTradeNo: '',
    clientSecret: '',
    intentId: '',
    currency: '',
    countryCode: '',
    paymentEnv: '',
    payAmount: 0,
    orderType: '',
    paymentMode: '',
    paymentNetwork: '',
    resumeToken: '',
    alipayMobilePrecreateDeepLink: false,
    createdAt: 0,
  }
}

function getWeixinJSBridge(): WeixinJSBridgeLike | undefined {
  return (window as Window & { WeixinJSBridge?: WeixinJSBridgeLike }).WeixinJSBridge
}

function waitForWeixinJSBridge(timeoutMs = 4000): Promise<WeixinJSBridgeLike | null> {
  const existing = getWeixinJSBridge()
  if (existing) return Promise.resolve(existing)

  return new Promise((resolve) => {
    let settled = false
    const finish = (bridge: WeixinJSBridgeLike | null) => {
      if (settled) return
      settled = true
      document.removeEventListener('WeixinJSBridgeReady', handleReady)
      document.removeEventListener('onWeixinJSBridgeReady', handleReady)
      window.clearTimeout(timer)
      resolve(bridge)
    }
    const handleReady = () => finish(getWeixinJSBridge() ?? null)
    const timer = window.setTimeout(() => finish(getWeixinJSBridge() ?? null), timeoutMs)
    document.addEventListener('WeixinJSBridgeReady', handleReady, false)
    document.addEventListener('onWeixinJSBridgeReady', handleReady, false)
  })
}

async function invokeWechatJsapiPayment(payload: Record<string, unknown>): Promise<Record<string, unknown>> {
  const bridge = await waitForWeixinJSBridge()
  if (!bridge) {
    throw new Error('WECHAT_JSAPI_UNAVAILABLE')
  }
  return new Promise((resolve) => {
    bridge.invoke('getBrandWCPayRequest', payload, (result) => resolve(result || {}))
  })
}

const paymentState = ref<PaymentRecoverySnapshot>(emptyPaymentState())

function persistRecoverySnapshot(snapshot: PaymentRecoverySnapshot) {
  if (typeof window === 'undefined' || !snapshot.orderId) return
  writePaymentRecoverySnapshot(window.localStorage, snapshot, PAYMENT_RECOVERY_STORAGE_KEY)
}

function removeRecoverySnapshot() {
  if (typeof window === 'undefined') return
  clearPaymentRecoverySnapshot(window.localStorage, PAYMENT_RECOVERY_STORAGE_KEY)
}

function resetPayment() {
  paymentPhase.value = 'select'
  paymentState.value = emptyPaymentState()
  removeRecoverySnapshot()
}

async function redirectToPaymentResult(state: PaymentRecoverySnapshot): Promise<void> {
  const query: Record<string, string | undefined> = {}
  if (state.orderId > 0) {
    query.order_id = String(state.orderId)
  }
  if (state.outTradeNo) {
    query.out_trade_no = state.outTradeNo
  }
  if (state.resumeToken) {
    query.resume_token = state.resumeToken
  }
  await router.push({
    path: '/payment/result',
    query,
  })
}

function onPaymentDone() {
  const wasSubscription = paymentState.value.orderType === 'subscription'
  resetPayment()
  selectedPlan.value = null
  if (wasSubscription) {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
}

async function onPaymentSuccess() {
  const completedPayment = { ...paymentState.value }
  removeRecoverySnapshot()
  authStore.refreshUser()
  if (paymentState.value.orderType === 'subscription') {
    subscriptionStore.fetchActiveSubscriptions(true).catch(() => {})
  }
  await redirectToPaymentResult(completedPayment)
}

function onPaymentSettled() {
  removeRecoverySnapshot()
}

// All checkout data from single API call
const checkout = ref<CheckoutInfoResponse>({
  methods: {}, method_options: [], global_min: 0, global_max: 0,
  plans: [], balance_disabled: false, balance_recharge_multiplier: 1, subscription_usd_to_cny_rate: 0, recharge_fee_rate: 0, help_text: '', help_image_url: '', stripe_publishable_key: '',
})

const tabs = computed(() => {
  const result: { key: 'recharge' | 'subscription'; label: string }[] = []
  if (!checkout.value.balance_disabled) result.push({ key: 'recharge', label: t('payment.tabTopUp') })
  result.push({ key: 'subscription', label: t('payment.tabSubscribe') })
  return result
})

const {
  channelOptions,
  enabledChannelIds,
  selectedChannel,
  validAmount,
  balanceRechargeMultiplier,
  creditedAmount,
  globalMinAmount,
  globalMaxAmount,
  selectedCurrency,
  methodOptions,
  feeRate,
  feeAmount,
  totalAmount,
  amountError,
  canSubmit,
  subPaymentAmount,
  subFeeAmount,
  subTotalAmount,
  subMethodOptions,
  canSubmitSubscription,
  balanceAmountFitsChannel,
  subscriptionAmountFitsChannel,
  formatSelectedPaymentAmount,
  formatSelectedSubscriptionPaymentAmount,
} = usePaymentChannelPricing({
  checkout,
  activeTab,
  amount,
  selectedPlan,
  selectedChannelId,
  locale: () => i18n.locale,
  t,
})

const {
  appendBackupChannelHint,
  attemptMobileQrFallback,
  buildWechatOAuthAuthorizeUrl,
  resumeWechatPaymentFromQuery,
} = usePaymentChannelRecovery({
  channelOptions,
  selectedChannelId,
  amount,
  selectedPlan,
  paymentState,
  paymentPhase,
  errorMessage,
  errorHintMessage,
  balanceAmountFitsChannel,
  subscriptionAmountFitsChannel,
  createPaymentOrder: payload => paymentStore.createOrder(payload),
  submitOrder: (orderAmount, orderType, planId, options) =>
    createOrder(orderAmount, orderType, planId, options),
  resolveRoute: (path, query) => router.resolve({ path, query }).href,
  replaceRoute: (path, query) => router.replace({ path, query }),
  persistRecoverySnapshot,
  isMobile: isMobileDevice,
  origin: () => typeof window !== 'undefined' ? window.location.origin : '',
  showWarning: message => appStore.showWarning(message),
  t,
})

// Adaptive grid: center single card, 2-col for 2 plans, 3-col for 3+
const planGridClass = computed(() => {
  const n = checkout.value.plans.length
  if (n <= 2) return 'grid grid-cols-1 gap-5 sm:grid-cols-2'
  return 'grid grid-cols-1 gap-5 sm:grid-cols-2 lg:grid-cols-3'
})

// Payment button class: follows selected payment method color
const paymentButtonClass = computed(() => {
  const m = selectedChannel.value?.payment_type || ''
  if (!m) return 'btn-primary'
  if (isBuiltInAlipayMethod(m)) return 'btn-alipay'
  if (isBuiltInWxpayMethod(m)) return 'btn-wxpay'
  if (m === 'stripe') return 'btn-stripe'
  if (m === 'airwallex') return 'btn-airwallex'
  return 'btn-primary'
})

// Subscription confirm: platform accent colors (clean card, no gradient)
const planBadgeClass = computed(() => platformBadgeClass(selectedPlan.value?.group_platform || ''))
const planTextClass = computed(() => platformTextClass(selectedPlan.value?.group_platform || ''))

// Renewal modal state
const showRenewalModal = ref(false)
const renewGroupId = ref<number | null>(null)
const renewalPlans = computed(() => {
  if (renewGroupId.value == null) return []
  return checkout.value.plans.filter(p => p.group_id === renewGroupId.value)
})

const planValiditySuffix = computed(() => {
  if (!selectedPlan.value) return ''
  return validitySuffixOf(selectedPlan.value, t)
})

function planHasPeakRate(plan: SubscriptionPlan): boolean {
  return hasPeakRate(plan)
}

function planPeakRateLabel(plan: SubscriptionPlan): string {
  return formatPeakRateWindow(plan, serverTimezoneLabel(appStore.cachedPublicSettings?.server_utc_offset))
}

function selectPlan(plan: SubscriptionPlan) {
  if (!isPlanPurchasable(plan)) {
    appStore.showWarning(t('payment.soldOut'))
    return
  }
  selectedPlan.value = plan
  errorMessage.value = ''
}

function selectPlanFromModal(plan: SubscriptionPlan) {
  if (!isPlanPurchasable(plan)) {
    appStore.showWarning(t('payment.soldOut'))
    return
  }
  showRenewalModal.value = false
  renewGroupId.value = null
  selectedPlan.value = plan
  errorMessage.value = ''
}

function closeRenewalModal() {
  showRenewalModal.value = false
  renewGroupId.value = null
}

async function handleSubmitRecharge() {
  if (!canSubmit.value || submitting.value) return
  await beginOrder(validAmount.value, 'balance')
}

async function confirmSubscribe() {
  if (!selectedPlan.value || submitting.value) return
  if (!isPlanPurchasable(selectedPlan.value)) {
    appStore.showWarning(t('payment.soldOut'))
    return
  }
  await beginOrder(selectedPlan.value.price, 'subscription', selectedPlan.value.id)
}

const showBepusdtNetworkDialog = ref(false)
const pendingBepusdtOrder = ref<{ amount: number; orderType: OrderType; planId?: number } | null>(null)

function isBepusdtNativeChannel(channel = selectedChannel.value): boolean {
  return channel?.payment_type === 'usdt'
    && channel.provider_key === 'easypay'
    && Array.isArray(channel.network_options)
    && channel.network_options.length > 0
}

async function beginOrder(orderAmount: number, orderType: OrderType, planId?: number) {
  if (isBepusdtNativeChannel()) {
    pendingBepusdtOrder.value = { amount: orderAmount, orderType, planId }
    showBepusdtNetworkDialog.value = true
    return
  }
  await createOrder(orderAmount, orderType, planId)
}

function cancelBepusdtNetworkSelection() {
  showBepusdtNetworkDialog.value = false
  pendingBepusdtOrder.value = null
}

async function confirmBepusdtNetworkSelection(network: string) {
  const pending = pendingBepusdtOrder.value
  showBepusdtNetworkDialog.value = false
  pendingBepusdtOrder.value = null
  if (!pending) return
  await createOrder(pending.amount, pending.orderType, pending.planId, { paymentNetwork: network })
}

async function createOrder(orderAmount: number, orderType: OrderType, planId?: number, options: CreateOrderOptions = {}) {
  if (orderType === 'subscription' && !options.isResume) {
    const plan = checkout.value.plans.find(candidate => candidate.id === planId)
    if (!isPlanPurchasable(plan)) {
      appStore.showWarning(t('payment.soldOut'))
      return
    }
  }
  submitting.value = true
  errorMessage.value = ''
  errorHintMessage.value = ''
  const requestedChannel = findPaymentChannel(
    channelOptions.value,
    options.paymentChannelId || selectedChannelId.value,
    options.paymentType,
    options.providerKey,
  ) || selectedChannel.value
  const requestedPaymentType = options.paymentType || requestedChannel?.payment_type || ''
  const requestType = normalizeVisibleMethod(requestedPaymentType) || requestedPaymentType
  const requestProviderKey = options.providerKey !== undefined
    ? options.providerKey
    : options.isResume
      ? ''
      : requestedChannel?.provider_key || ''
  let attemptedChannelId = requestedChannel?.id
  const mobilePrecreateDeepLink = paymentChannelSupports(
    requestedChannel,
    ALIPAY_MOBILE_PRECREATE_DEEP_LINK,
  )
  try {
    const payload = buildCreateOrderPayload({
      amount: orderAmount,
      paymentType: requestType,
      providerKey: requestProviderKey,
      orderType,
      planId,
      origin: typeof window !== 'undefined' ? window.location.origin : '',
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && normalizeVisibleMethod(requestType) === 'alipay'),
      mobilePrecreateDeepLink,
      paymentNetwork: options.paymentNetwork,
    })
    if (options.openid) {
      payload.openid = options.openid
    }
    if (options.wechatResumeToken) {
      payload.wechat_resume_token = options.wechatResumeToken
    }

    const result = await paymentStore.createOrder(payload) as CreateOrderResult & { resume_token?: string }
    const openWindow = (url: string) => {
      const win = window.open(url, 'paymentPopup', getPaymentPopupFeatures())
      if (!win || win.closed) {
        window.location.href = url
      }
    }
    const visibleMethod = normalizeVisibleMethod(requestType) || requestType
    const actualChannel = findPaymentChannel(
      channelOptions.value,
      result.provider_key ? undefined : requestedChannel?.id,
      visibleMethod,
      result.provider_key || requestProviderKey,
    ) || requestedChannel
    attemptedChannelId = actualChannel?.id || attemptedChannelId
    // When user clicks the dedicated Stripe button, leave method blank so the
    // landing page renders Stripe's full Payment Element (card/link/alipay/wxpay).
    const stripeMethod = visibleMethod === 'stripe'
      ? ''
      : visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
    const stripeRouteUrl = result.client_secret && visibleMethod !== 'airwallex'
      ? router.resolve({
        path: '/payment/stripe',
        query: {
          order_id: String(result.order_id),
          client_secret: result.client_secret,
          method: stripeMethod || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const airwallexRouteUrl = result.client_secret && result.intent_id
      ? router.resolve({
        path: '/payment/airwallex',
        query: {
          order_id: String(result.order_id),
          out_trade_no: result.out_trade_no || undefined,
          resume_token: result.resume_token || undefined,
        },
      }).href
      : ''
    const decision = decidePaymentLaunch(result, {
      visibleMethod,
      paymentChannelId: actualChannel?.id,
      providerKey: result.provider_key || requestProviderKey,
      orderType,
      isMobile: isMobileDevice(),
      isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
      forceQRCode: !!(checkout.value.alipay_force_qrcode && visibleMethod === 'alipay'),
      mobilePrecreateDeepLink: paymentChannelSupports(actualChannel, ALIPAY_MOBILE_PRECREATE_DEEP_LINK),
      stripePopupUrl: stripeRouteUrl,
      stripeRouteUrl,
      airwallexRouteUrl,
    })

    if (decision.kind === 'wechat_oauth' && decision.oauth?.authorize_url) {
      window.location.href = buildWechatOAuthAuthorizeUrl(decision.oauth.authorize_url, {
        paymentType: visibleMethod,
        providerKey: result.provider_key || requestProviderKey,
        orderType,
        planId,
        orderAmount,
      })
      return
    }

    if (decision.kind === 'unhandled') {
      applyScenarioError(
        { reason: 'UNHANDLED_PAYMENT_SCENARIO' },
        visibleMethod,
        { attemptedChannelId: actualChannel?.id, orderType, orderAmount },
      )
      return
    }

    paymentState.value = decision.paymentState
    paymentPhase.value = 'paying'
    persistRecoverySnapshot(decision.recovery)

    if (decision.kind === 'stripe_popup') {
      openWindow(decision.paymentState.payUrl)
      return
    }
    if (decision.kind === 'stripe_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'airwallex_route') {
      window.location.href = decision.paymentState.payUrl
      return
    }
    if (decision.kind === 'wechat_jsapi' && decision.jsapi) {
      try {
        const jsapiResult = await invokeWechatJsapiPayment(decision.jsapi as Record<string, unknown>)
        const errMsg = String(jsapiResult.err_msg || '').toLowerCase()
        if (errMsg.includes('cancel')) {
          appStore.showInfo(t('payment.qr.cancelled'))
          resetPayment()
        } else if (errMsg && !errMsg.includes('ok')) {
          resetPayment()
          const fallbackApplied = await attemptMobileQrFallback(
            { reason: 'WECHAT_JSAPI_FAILED', message: errMsg },
            {
              orderAmount,
              orderType,
              planId,
              paymentType: visibleMethod,
              providerKey: result.provider_key || requestProviderKey,
              paymentChannelId: actualChannel?.id,
              attempted: options.mobileQrFallbackAttempted === true,
            },
          )
          if (!fallbackApplied) {
            applyScenarioError(
              { reason: 'WECHAT_JSAPI_FAILED', message: errMsg },
              visibleMethod,
              { attemptedChannelId: actualChannel?.id, orderType, orderAmount },
            )
          }
        } else {
          const resultState = { ...decision.paymentState }
          resetPayment()
          await redirectToPaymentResult(resultState)
        }
      } catch (err: unknown) {
        resetPayment()
        const fallbackApplied = await attemptMobileQrFallback(err, {
          orderAmount,
          orderType,
          planId,
          paymentType: visibleMethod,
          providerKey: result.provider_key || requestProviderKey,
          paymentChannelId: actualChannel?.id,
          attempted: options.mobileQrFallbackAttempted === true,
        })
        if (!fallbackApplied) {
          throw err
        }
      }
      return
    }
    if (decision.kind === 'redirect_waiting' && decision.paymentState.payUrl) {
      if (isMobileDevice()) {
        window.location.href = decision.paymentState.payUrl
        return
      }
      openWindow(decision.paymentState.payUrl)
    }
  } catch (err: unknown) {
    const apiErr = err as Record<string, unknown>
    const availabilityError = orderType === 'subscription' && planId !== undefined
      ? planAvailabilityError(err)
      : null
    if (availabilityError) {
      const plans = await synchronizePlanAvailability(
        checkout.value.plans,
        planId!,
        availabilityError,
        async () => (await paymentAPI.getCheckoutInfo()).data.plans,
      )
      checkout.value = { ...checkout.value, plans }
      if (selectedPlan.value?.id === planId) selectedPlan.value = null
      errorMessage.value = extractI18nErrorMessage(err, t, 'payment.errors', t('payment.soldOut'))
      errorHintMessage.value = ''
    } else if (apiErr.reason === 'TOO_MANY_PENDING') {
      const metadata = apiErr.metadata as Record<string, unknown> | undefined
      errorMessage.value = t('payment.errors.tooManyPending', { max: metadata?.max || '' })
      errorHintMessage.value = ''
    } else if (apiErr.reason === 'CANCEL_RATE_LIMITED') {
      errorMessage.value = t('payment.errors.cancelRateLimited')
      errorHintMessage.value = ''
    } else if (await attemptMobileQrFallback(err, {
      orderAmount,
      orderType,
      planId,
      paymentType: requestType,
      providerKey: requestProviderKey,
      paymentChannelId: requestedChannel?.id,
      attempted: options.mobileQrFallbackAttempted === true,
    })) {
      return
    } else {
      const handled = applyScenarioError(
        err,
        requestType,
        { attemptedChannelId, orderType, orderAmount },
      )
      if (!handled) {
        errorMessage.value = extractI18nErrorMessage(err, t, 'payment.errors', extractApiErrorMessage(err, t('payment.result.failed')))
        errorHintMessage.value = appendBackupChannelHint(
          err,
          '',
          requestType,
          { attemptedChannelId, orderType, orderAmount },
        )
      }
      if (handled) {
        return
      }
    }
    appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  } finally {
    submitting.value = false
  }
}

function applyScenarioError(
  err: unknown,
  paymentMethod: string,
  context: BackupChannelHintContext,
): boolean {
  const descriptor = describePaymentScenarioError(err, {
    paymentMethod,
    isMobile: isMobileDevice(),
    isWechatBrowser: typeof window !== 'undefined' && /MicroMessenger/i.test(window.navigator.userAgent),
  })
  if (!descriptor) {
    errorMessage.value = ''
    errorHintMessage.value = ''
    return false
  }
  errorMessage.value = t(descriptor.messageKey)
  errorHintMessage.value = appendBackupChannelHint(
    err,
    descriptor.hintKey ? t(descriptor.hintKey) : '',
    paymentMethod,
    context,
  )
  appStore.showError(buildPaymentErrorToastMessage(errorMessage.value, errorHintMessage.value))
  return true
}

onMounted(async () => {
  try {
    const res = await paymentAPI.getCheckoutInfo()
    checkout.value = res.data
    if (enabledChannelIds.value.length) {
      selectedChannelId.value = enabledChannelIds.value[0]
    }
    if (typeof window !== 'undefined') {
      if (hasWechatResumeQuery(route.query)) {
        removeRecoverySnapshot()
      }
      const routeResumeToken = typeof route.query.resume_token === 'string'
        ? route.query.resume_token
        : typeof route.query.wechat_resume_token === 'string'
          ? route.query.wechat_resume_token
          : undefined
      const restored = readPaymentRecoverySnapshot(
        window.localStorage.getItem(PAYMENT_RECOVERY_STORAGE_KEY),
        { resumeToken: routeResumeToken },
      )
      if (restored) {
        paymentState.value = restored
        paymentPhase.value = 'paying'
        const restoredChannel = findPaymentChannel(
          channelOptions.value,
          restored.paymentChannelId,
          restored.paymentType,
          restored.providerKey,
        )
        if (restoredChannel) {
          selectedChannelId.value = restoredChannel.id
        }
      } else {
        removeRecoverySnapshot()
      }
    }
    await resumeWechatPaymentFromQuery(
      { path: route.path, query: route.query },
      checkout.value.plans,
      validAmount.value,
    )
    if (checkout.value.balance_disabled) {
      activeTab.value = 'subscription'
    }
    // Handle renewal navigation: ?tab=subscription&group=123
    if (route.query.tab === 'subscription') {
      activeTab.value = 'subscription'
      if (route.query.group) {
        const groupId = Number(route.query.group)
        const groupPlans = checkout.value.plans.filter(p => p.group_id === groupId)
        const purchasablePlans = groupPlans.filter(isPlanPurchasable)
        if (purchasablePlans.length === 1) {
          selectedPlan.value = purchasablePlans[0]
        } else if (groupPlans.length > 0) {
          renewGroupId.value = groupId
          showRenewalModal.value = true
        }
      }
    }
  } catch (err: unknown) { appStore.showError(extractI18nErrorMessage(err, t, 'payment.errors', t('common.error'))) }
  finally { loading.value = false }
  // Fetch active subscriptions (uses cache, non-blocking)
  subscriptionStore.fetchActiveSubscriptions().catch(() => {})
})
</script>
