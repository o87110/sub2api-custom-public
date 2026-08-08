import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'

import PlanEditDialog from '../PlanEditDialog.vue'
import type { AdminGroup } from '@/types'
import type { SubscriptionPlan } from '@/types/payment'

const { createPlan, updatePlan, showError, showSuccess } = vi.hoisted(() => ({
  createPlan: vi.fn(),
  updatePlan: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    t: (key: string, params?: Record<string, unknown>) => {
      if (key === 'payment.admin.subscriptionCnyPayPreview') return `preview ${params?.amount}`
      if (key === 'payment.admin.subscriptionCnyPayPreviewWithFee') return `fee ${params?.feeRate} ${params?.total}`
      return key
    },
  }),
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    createPlan,
    updatePlan,
  },
}))

const BaseDialogStub = defineComponent({
  name: 'BaseDialog',
  props: {
    show: Boolean,
    title: String,
    width: String,
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

const SelectStub = defineComponent({
  name: 'SelectStub',
  props: {
    modelValue: [String, Number],
    options: {
      type: Array,
      default: () => [],
    },
    placeholder: String,
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const onChange = (event: Event) => {
      const value = (event.target as HTMLSelectElement).value
      const option = (props.options as Array<{ value: string | number }>).find(candidate => String(candidate.value) === value)
      emit('update:modelValue', option?.value ?? null)
    }
    return { onChange }
  },
  template: `
    <select
      :value="modelValue ?? ''"
      @change="onChange"
    >
      <option value="">{{ placeholder }}</option>
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :data-platform="option.platform"
      >
        {{ option.label }}
      </option>
    </select>
  `,
})

const groupFixture = (overrides: Partial<AdminGroup>): AdminGroup => ({
  id: 1,
  name: 'OpenAI',
  description: null,
  platform: 'openai',
  rate_multiplier: 1,
  rpm_limit: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'subscription',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: false,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  peak_rate_enabled: false,
  peak_start: '',
  peak_end: '',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  allow_messages_dispatch: false,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-01T00:00:00Z',
  updated_at: '2026-07-01T00:00:00Z',
  model_routing: null,
  model_routing_enabled: false,
  mcp_xml_inject: false,
  sort_order: 0,
  ...overrides,
})

function mountDialog({
  groups = [],
  paymentConfig = null,
  plan = null,
}: {
  groups?: AdminGroup[]
  paymentConfig?: Record<string, unknown> | null
  plan?: SubscriptionPlan | null
} = {}) {
  return mount(PlanEditDialog, {
    props: {
      show: true,
      plan,
      groups,
      paymentConfig,
    },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Select: SelectStub,
        Icon: true,
        GroupBadge: true,
      },
    },
  })
}

describe('PlanEditDialog', () => {
  beforeEach(() => {
    createPlan.mockReset()
    updatePlan.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
  })

  it('shows CNY channel charge using the configured subscription rate and fee', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 7.15,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).toContain('preview')
    expect(wrapper.text()).toContain('¥71.43')
    expect(wrapper.text()).toContain('fee 2.5')
    expect(wrapper.text()).toContain('¥73.22')
  })

  it('hides the preview when the subscription rate is not configured', async () => {
    const wrapper = mountDialog({
      paymentConfig: {
        subscription_usd_to_cny_rate: 0,
        recharge_fee_rate: 2.5,
      },
    })

    await wrapper.find('input[type="number"]').setValue('9.99')

    expect(wrapper.text()).not.toContain('preview')
    expect(wrapper.text()).not.toContain('¥71.43')
  })

  it('allows composite subscription groups for payment plans', () => {
    const wrapper = mountDialog({
      groups: [
        groupFixture({
          id: 10,
          name: 'OpenAI + Claude + Gemini + Grok',
          platform: 'composite',
          rate_multiplier: 1.2,
          subscription_type: 'subscription',
        }),
        groupFixture({
          id: 11,
          name: 'Standard OpenAI',
          platform: 'openai',
          subscription_type: 'standard',
        }),
      ],
    })

    const options = wrapper.findAll('option').map(option => option.text())

    expect(options).toContain('OpenAI + Claude + Gemini + Grok — composite (1.2x)')
    expect(options).not.toContain('Standard OpenAI — openai (1x)')
  })

  it('submits blank inventory as unlimited and a positive integer as limited', async () => {
    const group = groupFixture({})
    const wrapper = mountDialog({ groups: [group] })
    await wrapper.find('input[type="text"]').setValue('Inventory plan')
    await wrapper.find('select').setValue(String(group.id))
    await wrapper.find('textarea').setValue('Inventory plan description')
    await wrapper.findAll('input[type="number"]')[0].setValue('10')
    await wrapper.find('form').trigger('submit')

    expect(createPlan).toHaveBeenCalledOnce()
    expect(createPlan.mock.calls[0][0]).toMatchObject({
      remaining_quantity: null,
      sold_out_action: 'delist',
    })

    createPlan.mockReset()
    await wrapper.find('#subscription-plan-remaining-quantity').setValue('8')
    await wrapper.find('form').trigger('submit')
    expect(createPlan.mock.calls[0][0]).toMatchObject({ remaining_quantity: 8 })
  })

  it('defaults bulk reset eligibility off and submits the selected value', async () => {
    const group = groupFixture({})
    const wrapper = mountDialog({ groups: [group] })
    const toggle = wrapper.get('#subscription-plan-bulk-quota-reset')

    expect((toggle.element as HTMLInputElement).checked).toBe(false)
    await toggle.setValue(true)
    await wrapper.find('input[type="text"]').setValue('Bulk reset plan')
    await wrapper.find('select').setValue(String(group.id))
    await wrapper.find('textarea').setValue('Bulk reset plan description')
    await wrapper.findAll('input[type="number"]')[0].setValue('10')
    await wrapper.find('form').trigger('submit')

    expect(createPlan.mock.calls[0][0]).toMatchObject({ allow_bulk_quota_reset: true })
  })

  it('rejects zero, negative, decimal, and unsafe inventory values', async () => {
    const group = groupFixture({})
    const wrapper = mountDialog({ groups: [group] })
    await wrapper.find('input[type="text"]').setValue('Invalid inventory plan')
    await wrapper.find('select').setValue(String(group.id))
    await wrapper.find('textarea').setValue('Invalid inventory plan description')
    await wrapper.findAll('input[type="number"]')[0].setValue('10')
    const input = wrapper.find('#subscription-plan-remaining-quantity')

    for (const value of ['0', '-1', '1.5', '9007199254740992']) {
      await input.setValue(value)
      await wrapper.find('form').trigger('submit')
    }

    expect(createPlan).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledTimes(4)
  })

  it('allows zero only after selecting disable purchase', async () => {
    const group = groupFixture({})
    const wrapper = mountDialog({ groups: [group] })
    await wrapper.find('input[type="text"]').setValue('Visible sold-out plan')
    await wrapper.find('select').setValue(String(group.id))
    await wrapper.find('textarea').setValue('Visible sold-out description')
    await wrapper.findAll('input[type="number"]')[0].setValue('10')
    await wrapper.find('[data-testid="sold-out-action-select"]').setValue('disable_purchase')
    await wrapper.find('#subscription-plan-remaining-quantity').setValue('0')
    await wrapper.find('form').trigger('submit')

    expect(createPlan).toHaveBeenCalledOnce()
    expect(createPlan.mock.calls[0][0]).toMatchObject({
      remaining_quantity: 0,
      sold_out_action: 'disable_purchase',
    })
  })

  it('does not submit an unchanged sold-out zero when editing other fields', async () => {
    const plan: SubscriptionPlan = {
      id: 7,
      group_id: 1,
      name: 'Sold-out plan',
      description: 'Sold-out description',
      price: 10,
      currency: '',
      validity_days: 30,
      validity_unit: 'days',
      features: [],
      for_sale: false,
      remaining_quantity: 0,
      inventory_auto_delisted: true,
      sort_order: 0,
    }
    const wrapper = mountDialog({ groups: [groupFixture({})], plan })
    await wrapper.find('input[type="text"]').setValue('Renamed sold-out plan')
    await wrapper.find('form').trigger('submit')

    expect(updatePlan).toHaveBeenCalledOnce()
    const payload = updatePlan.mock.calls[0][1]
    expect(payload).not.toHaveProperty('remaining_quantity')
    expect(payload).not.toHaveProperty('for_sale')
  })

  it('submits restock without forcing automatic delisting to remain off sale', async () => {
    const plan: SubscriptionPlan = {
      id: 8,
      group_id: 1,
      name: 'Auto-delisted plan',
      description: 'Auto-delisted description',
      price: 10,
      currency: '',
      validity_days: 30,
      validity_unit: 'days',
      features: [],
      for_sale: false,
      remaining_quantity: 0,
      inventory_auto_delisted: true,
      sort_order: 0,
    }
    const wrapper = mountDialog({ groups: [groupFixture({})], plan })
    await wrapper.find('#subscription-plan-remaining-quantity').setValue('5')
    await wrapper.find('form').trigger('submit')

    const payload = updatePlan.mock.calls[0][1]
    expect(payload).toMatchObject({ remaining_quantity: 5 })
    expect(payload).not.toHaveProperty('for_sale')
  })

  it('submits a sold-out strategy change without resubmitting an unchanged zero', async () => {
    const plan: SubscriptionPlan = {
      id: 9,
      group_id: 1,
      name: 'Sold-out disabled plan',
      description: 'Visible but unavailable',
      price: 10,
      currency: '',
      validity_days: 30,
      validity_unit: 'days',
      features: [],
      for_sale: true,
      remaining_quantity: 0,
      inventory_auto_delisted: false,
      sold_out_action: 'disable_purchase',
      sort_order: 0,
    }
    const wrapper = mountDialog({ groups: [groupFixture({})], plan })
    await wrapper.find('[data-testid="sold-out-action-select"]').setValue('delist')
    await wrapper.find('form').trigger('submit')

    expect(updatePlan).toHaveBeenCalledOnce()
    const payload = updatePlan.mock.calls[0][1]
    expect(payload).toMatchObject({ sold_out_action: 'delist' })
    expect(payload).not.toHaveProperty('remaining_quantity')
    expect(payload).not.toHaveProperty('for_sale')
  })
})
