import type { ComputedRef, Ref } from 'vue'
import type { LocationQuery, LocationQueryRaw } from 'vue-router'

import { extractApiErrorCode } from '@/utils/apiError'
import {
  buildCreateOrderPayload,
  decidePaymentLaunch,
  normalizeVisibleMethod,
  type PaymentRecoverySnapshot,
} from '@/components/payment/paymentFlow'
import type { CreateOrderResult, OrderType, SubscriptionPlan } from '@/types/payment'
import {
  findBackupPaymentChannel,
  findPaymentChannel,
  isGatewayChannelFailureCode,
  paymentChannelLabel,
  type PaymentChannelOption,
} from './paymentChannels'
import { parseWechatResumeRoute, stripWechatResumeQuery } from './paymentRecoveryRoute'

type Translate = (key: string, params?: string | Record<string, unknown>) => string
type CreateOrderPayload = ReturnType<typeof buildCreateOrderPayload>

export interface BackupChannelHintContext {
  attemptedChannelId?: string
  orderType: OrderType
  orderAmount: number
}

export interface MobileQrFallbackContext {
  orderAmount: number
  orderType: OrderType
  planId?: number
  paymentType: string
  providerKey?: string
  paymentChannelId?: string
  attempted: boolean
}

export interface PaymentResumeSubmitOptions {
  openid?: string
  wechatResumeToken?: string
  paymentType?: string
  providerKey?: string
  isResume?: boolean
}

export interface PaymentChannelRecoveryOptions {
  channelOptions: ComputedRef<PaymentChannelOption[]>
  selectedChannelId: Ref<string>
  amount: Ref<number | null>
  selectedPlan: Ref<SubscriptionPlan | null>
  paymentState: Ref<PaymentRecoverySnapshot>
  paymentPhase: Ref<'select' | 'paying'>
  errorMessage: Ref<string>
  errorHintMessage: Ref<string>
  balanceAmountFitsChannel: (channel: PaymentChannelOption, value?: number) => boolean
  subscriptionAmountFitsChannel: (channel: PaymentChannelOption, price?: number) => boolean
  createPaymentOrder: (payload: CreateOrderPayload) => Promise<CreateOrderResult & { resume_token?: string }>
  submitOrder: (
    amount: number,
    orderType: OrderType,
    planId?: number,
    options?: PaymentResumeSubmitOptions,
  ) => Promise<void>
  resolveRoute: (path: string, query: Record<string, string | undefined>) => string
  replaceRoute: (path: string, query: LocationQueryRaw) => Promise<unknown>
  persistRecoverySnapshot: (snapshot: PaymentRecoverySnapshot) => void
  isMobile: () => boolean
  origin: () => string
  showWarning: (message: string) => void
  t: Translate
}

export function shouldFallbackToDesktopQr(
  err: unknown,
  paymentMethod: string,
  attempted: boolean,
  isMobile: boolean,
): boolean {
  if (attempted || !isMobile) return false

  const normalizedMethod = normalizeVisibleMethod(paymentMethod) || paymentMethod
  const reason = typeof err === 'object' && err && 'reason' in err && typeof err.reason === 'string'
    ? err.reason
    : ''
  const message = err instanceof Error
    ? err.message
    : (typeof err === 'object' && err && 'message' in err && typeof err.message === 'string'
      ? err.message
      : '')
  const normalizedMessage = message.toLowerCase()

  if (normalizedMethod === 'wxpay') {
    return reason === 'WECHAT_H5_NOT_AUTHORIZED'
      || reason === 'WECHAT_PAYMENT_MP_NOT_CONFIGURED'
      || reason === 'WECHAT_JSAPI_FAILED'
      || reason === 'PAYMENT_GATEWAY_ERROR'
      || reason === 'UNHANDLED_PAYMENT_SCENARIO'
      || normalizedMessage.includes('weixinjsbridge is unavailable')
      || normalizedMessage.includes('wechat_jsapi_unavailable')
  }
  if (normalizedMethod === 'alipay') {
    return reason === 'PAYMENT_GATEWAY_ERROR' || reason === 'UNHANDLED_PAYMENT_SCENARIO'
  }
  return false
}

export function buildWechatOAuthAuthorizeUrl(
  authorizeUrl: string,
  context: {
    paymentType: string
    providerKey?: string
    orderType: OrderType
    planId?: number
    orderAmount: number
  },
  origin: string,
): string {
  const normalizedUrl = authorizeUrl.trim()
  if (!normalizedUrl || !origin) return normalizedUrl

  try {
    const targetUrl = new URL(normalizedUrl, origin)
    const redirectPath = targetUrl.searchParams.get('redirect') || '/purchase'
    const redirectUrl = new URL(redirectPath, origin)
    const paymentType = normalizeVisibleMethod(context.paymentType) || context.paymentType.trim() || 'wxpay'

    redirectUrl.searchParams.set('payment_type', paymentType)
    redirectUrl.searchParams.set('order_type', context.orderType)
    if (context.planId) redirectUrl.searchParams.set('plan_id', String(context.planId))
    else redirectUrl.searchParams.delete('plan_id')
    if (context.orderAmount > 0) redirectUrl.searchParams.set('amount', String(context.orderAmount))
    else redirectUrl.searchParams.delete('amount')

    if (context.providerKey?.trim()) {
      const providerKey = context.providerKey.trim().toLowerCase()
      targetUrl.searchParams.set('provider_key', providerKey)
      redirectUrl.searchParams.set('provider_key', providerKey)
    }
    targetUrl.searchParams.set('redirect', `${redirectUrl.pathname}${redirectUrl.search}`)
    return targetUrl.toString()
  } catch {
    return normalizedUrl
  }
}

export function usePaymentChannelRecovery(options: PaymentChannelRecoveryOptions) {
  async function attemptMobileQrFallback(
    err: unknown,
    context: MobileQrFallbackContext,
  ): Promise<boolean> {
    if (!shouldFallbackToDesktopQr(err, context.paymentType, context.attempted, options.isMobile())) {
      return false
    }

    try {
      const visibleMethod = normalizeVisibleMethod(context.paymentType) || context.paymentType
      const payload = buildCreateOrderPayload({
        amount: context.orderAmount,
        paymentType: visibleMethod,
        providerKey: context.providerKey,
        orderType: context.orderType,
        planId: context.planId,
        origin: options.origin(),
        isMobile: false,
        isWechatBrowser: false,
      })
      const result = await options.createPaymentOrder(payload)
      const stripeMethod = visibleMethod === 'wxpay' ? 'wechat_pay' : 'alipay'
      const stripeRouteUrl = result.client_secret
        ? options.resolveRoute('/payment/stripe', {
            order_id: String(result.order_id),
            client_secret: result.client_secret,
            method: stripeMethod,
            resume_token: result.resume_token || undefined,
          })
        : ''
      const decision = decidePaymentLaunch(result, {
        visibleMethod,
        paymentChannelId: context.paymentChannelId,
        providerKey: context.providerKey,
        orderType: context.orderType,
        isMobile: false,
        isWechatBrowser: false,
        stripePopupUrl: stripeRouteUrl,
        stripeRouteUrl,
      })

      if (decision.kind !== 'qr_waiting' || !decision.paymentState.qrCode) return false

      options.errorMessage.value = ''
      options.errorHintMessage.value = ''
      options.paymentState.value = decision.paymentState
      options.paymentPhase.value = 'paying'
      options.persistRecoverySnapshot(decision.recovery)
      options.showWarning(options.t('payment.errors.mobilePaymentFallbackToQr'))
      return true
    } catch {
      return false
    }
  }

  function appendBackupChannelHint(
    err: unknown,
    currentHint: string,
    paymentMethod: string,
    context: BackupChannelHintContext,
  ): string {
    if (!isGatewayChannelFailureCode(extractApiErrorCode(err))) return currentHint
    const backup = findBackupPaymentChannel(
      options.channelOptions.value,
      context.attemptedChannelId || '',
      paymentMethod,
      channel => context.orderType === 'subscription'
        ? options.subscriptionAmountFitsChannel(channel, context.orderAmount)
        : options.balanceAmountFitsChannel(channel, context.orderAmount),
    )
    if (!backup) return currentHint
    const switchHint = options.t('payment.errors.switchChannelHint', {
      channel: paymentChannelLabel(backup, options.t),
    })
    return currentHint ? `${currentHint} ${switchHint}` : switchHint
  }

  async function resumeWechatPaymentFromQuery(
    route: { path: string; query: LocationQuery },
    plans: SubscriptionPlan[],
    fallbackBalanceAmount: number,
  ): Promise<void> {
    const resume = parseWechatResumeRoute(route.query, plans, fallbackBalanceAmount)
    if (!resume) return

    const resumeChannel = findPaymentChannel(
      options.channelOptions.value,
      '',
      resume.paymentType,
      resume.providerKey,
    )
    if (resumeChannel) options.selectedChannelId.value = resumeChannel.id
    if (resume.orderType === 'balance' && resume.orderAmount > 0) {
      options.amount.value = resume.orderAmount
    }
    if (resume.orderType === 'subscription' && resume.planId) {
      options.selectedPlan.value = plans.find(plan => plan.id === resume.planId) ?? null
    }

    await options.replaceRoute(route.path, stripWechatResumeQuery(route.query))
    if (resume.wechatResumeToken) {
      await options.submitOrder(0, resume.orderType, resume.planId, {
        wechatResumeToken: resume.wechatResumeToken,
        paymentType: resume.paymentType,
        providerKey: resume.providerKey,
        isResume: true,
      })
      return
    }
    if (resume.orderAmount > 0 && resume.openid) {
      await options.submitOrder(resume.orderAmount, resume.orderType, resume.planId, {
        openid: resume.openid,
        paymentType: resume.paymentType,
        providerKey: resume.providerKey,
        isResume: true,
      })
    }
  }

  return {
    appendBackupChannelHint,
    attemptMobileQrFallback,
    buildWechatOAuthAuthorizeUrl: (
      authorizeUrl: string,
      context: Parameters<typeof buildWechatOAuthAuthorizeUrl>[1],
    ) => buildWechatOAuthAuthorizeUrl(authorizeUrl, context, options.origin()),
    resumeWechatPaymentFromQuery,
  }
}
