import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AdminPaymentPlansView from '../AdminPaymentPlansView.vue'

const { getPlans, getConfig, getGroups } = vi.hoisted(() => ({
  getPlans: vi.fn(),
  getConfig: vi.fn(),
  getGroups: vi.fn(),
}))

vi.mock('@/api/admin/payment', () => ({
  adminPaymentAPI: {
    getPlans,
    getConfig,
  },
}))

vi.mock('@/api/admin', () => ({
  default: {
    groups: {
      getAll: getGroups,
    },
  },
}))

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key,
    }),
  }
})

const DataTableStub = {
  props: ['data'],
  template: `
    <div>
      <div v-for="row in data" :key="row.id">
        <slot name="cell-price" :value="row.price" :row="row" />
        <slot name="cell-remaining_quantity" :value="row.remaining_quantity" :row="row" />
        <slot name="cell-sold_out_action" :value="row.sold_out_action" :row="row" />
        <slot name="cell-renewal_policy" :row="row" />
        <slot name="cell-for_sale" :value="row.for_sale" :row="row" />
      </div>
    </div>
  `,
}

describe('AdminPaymentPlansView', () => {
  beforeEach(() => {
    getGroups.mockResolvedValue([])
    getConfig.mockResolvedValue({ data: {} })
    getPlans.mockResolvedValue({
      data: [
        {
          id: 1,
          name: 'CNY plan',
          group_id: 1,
          price: 499,
          original_price: 599,
          currency: 'CNY',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: true,
          remaining_quantity: null,
          inventory_auto_delisted: false,
          sold_out_action: 'delist',
          features: [],
        },
        {
          id: 2,
          name: 'Legacy plan',
          group_id: 1,
          price: 10,
          original_price: 0,
          currency: '',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: true,
          remaining_quantity: 4,
          inventory_auto_delisted: false,
          sold_out_action: 'delist',
          features: [],
        },
        {
          id: 3,
          name: 'Sold-out plan',
          group_id: 1,
          price: 20,
          original_price: 0,
          currency: '',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: false,
          remaining_quantity: 0,
          inventory_auto_delisted: true,
          sold_out_action: 'delist',
          features: [],
        },
        {
          id: 4,
          name: 'Visible sold-out plan',
          group_id: 1,
          price: 20,
          original_price: 0,
          currency: '',
          validity_days: 30,
          validity_unit: 'day',
          sort_order: 0,
          for_sale: true,
          remaining_quantity: 0,
          inventory_auto_delisted: false,
          sold_out_action: 'disable_purchase',
          allow_existing_user_renewal: true,
          renewal_grace_days: 5,
          features: [],
        },
      ],
    })
  })

  it('uses the configured currency symbol and keeps legacy prices in USD', async () => {
    const wrapper = mount(AdminPaymentPlansView, {
      global: {
        plugins: [createPinia()],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          DataTable: DataTableStub,
          ConfirmDialog: true,
          GroupBadge: true,
          Icon: true,
          PlanEditDialog: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('¥499.00CNY')
    expect(wrapper.text()).toContain('¥599.00')
    expect(wrapper.text()).toContain('$10.00')
  })

  it('shows unlimited, limited, and automatically delisted sold-out inventory states', async () => {
    const wrapper = mount(AdminPaymentPlansView, {
      global: {
        plugins: [createPinia()],
        stubs: {
          AppLayout: { template: '<div><slot /></div>' },
          DataTable: DataTableStub,
          ConfirmDialog: true,
          GroupBadge: true,
          Icon: true,
          PlanEditDialog: true,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('payment.admin.inventoryUnlimited')
    expect(wrapper.text()).toContain('4')
    expect(wrapper.text()).toContain('payment.admin.inventorySoldOut')
    expect(wrapper.text()).toContain('payment.admin.inventoryAutoDelisted')
    expect(wrapper.text()).toContain('payment.admin.inventoryPurchaseDisabled')
    expect(wrapper.text()).toContain('payment.admin.soldOutActionDelist')
    expect(wrapper.text()).toContain('payment.admin.soldOutActionDisablePurchase')
    expect(wrapper.text()).toContain('payment.admin.renewalEnabled')
    expect(wrapper.text()).toContain('payment.admin.renewalGraceDaysValue')
    expect(wrapper.find('button[disabled]').exists()).toBe(true)
  })
})
