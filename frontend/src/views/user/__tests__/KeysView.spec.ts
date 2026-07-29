import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { nextTick, ref } from 'vue'

import type { ApiKey } from '@/types'
import KeysView from '../KeysView.vue'

const {
  listKeys,
  getPublicSettings,
  getDashboardApiKeysUsage,
  getAvailableGroups,
  getUserGroupRates,
  showError,
  showSuccess,
  copyToClipboard,
  isCurrentStep,
  nextStep,
} = vi.hoisted(() => ({
  listKeys: vi.fn(),
  getPublicSettings: vi.fn(),
  getDashboardApiKeysUsage: vi.fn(),
  getAvailableGroups: vi.fn(),
  getUserGroupRates: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
  copyToClipboard: vi.fn(),
  isCurrentStep: vi.fn(),
  nextStep: vi.fn(),
}))

const messages: Record<string, string> = {
  'common.actions': 'Actions',
  'common.name': 'Name',
  'common.refresh': 'Refresh',
  'common.status': 'Status',
  'keys.apiKey': 'API Key',
  'keys.allGroups': 'All Groups',
  'keys.allStatus': 'All Status',
  'keys.columnSettings': 'Column Settings',
  'keys.createKey': 'Create API Key',
  'keys.created': 'Created',
  'keys.expiresAt': 'Expires',
  'keys.group': 'Group',
  'keys.id': 'ID',
  'keys.currentConcurrency': 'Current Concurrency',
  'keys.lastUsedAt': 'Last Used',
  'keys.lastUsedIP': 'Last Used IP',
  'keys.rateLimitColumn': 'Rate Limit',
  'keys.searchPlaceholder': 'Search name or key...',
  'keys.status.active': 'Active',
  'keys.status.expired': 'Expired',
  'keys.status.inactive': 'Inactive',
  'keys.status.quota_exhausted': 'Quota exhausted',
  'keys.usage': 'Usage',
}

vi.mock('@/api', () => ({
  keysAPI: {
    list: listKeys,
    create: vi.fn(),
    update: vi.fn(),
    delete: vi.fn(),
    toggleStatus: vi.fn(),
  },
  authAPI: {
    getPublicSettings,
  },
  usageAPI: {
    getDashboardApiKeysUsage,
  },
  userGroupsAPI: {
    getAvailable: getAvailableGroups,
    getUserGroupRates,
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/stores/onboarding', () => ({
  useOnboardingStore: () => ({
    isCurrentStep,
    nextStep,
  }),
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({
    copyToClipboard,
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: ref('en'),
      t: (key: string) => messages[key] ?? key,
    }),
  }
})

const createApiKey = (): ApiKey => ({
  id: 1,
  user_id: 1,
  key: 'sk-test-key',
  name: 'test-key',
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-06-27T00:00:00Z',
  updated_at: '2026-06-27T00:00:00Z',
  current_concurrency: 3,
  rate_limit_5h: 0,
  rate_limit_1d: 0,
  rate_limit_7d: 0,
  usage_5h: 0,
  usage_1d: 0,
  usage_7d: 0,
  window_5h_start: null,
  window_1d_start: null,
  window_7d_start: null,
  reset_5h_at: null,
  reset_1d_at: null,
  reset_7d_at: null,
})

const AppLayoutStub = {
  template: '<div><slot /></div>',
}

const TablePageLayoutStub = {
  template: `
    <div>
      <slot name="filters" />
      <slot name="actions" />
      <slot name="table" />
      <slot name="pagination" />
    </div>
  `,
}

const DataTableStub = {
  name: 'DataTable',
  props: ['columns', 'data', 'selectable', 'rowKey', 'selectedKeys', 'selectionLabel'],
  emits: ['sort', 'update:selectedKeys'],
  template: `
    <div>
      <div data-test="columns">{{ columns.map((col) => col.key).join(',') }}</div>
      <div data-test="selection-config">{{ String(selectable) }}|{{ rowKey }}|{{ selectedKeys.join(',') }}</div>
      <button
        v-if="data.length > 0"
        data-test="select-first-key"
        @click="$emit('update:selectedKeys', [data[0].id])"
      >
        Select first
      </button>
      <div data-test="columns-meta">{{ JSON.stringify(columns.map((col) => ({ key: col.key, sortable: !!col.sortable }))) }}</div>
      <button data-test="sort-current-concurrency" @click="$emit('sort', 'current_concurrency', 'asc')">
        Sort Current Concurrency
      </button>
      <div v-for="row in data" :key="row.id">
        <div
          v-if="columns.some((col) => col.key === 'id')"
          data-test="key-id"
        >
          <slot name="cell-id" :value="row.id" :row="row" />
        </div>
        <slot name="cell-name" :value="row.name" :row="row" />
        <div
          v-if="columns.some((col) => col.key === 'group')"
          data-test="group-cell"
        >
          <slot name="cell-group" :value="row.group" :row="row" />
        </div>
        <div data-test="current-concurrency">
          <slot name="cell-current_concurrency" :value="row.current_concurrency" :row="row" />
        </div>
        <div
          v-if="columns.some((col) => col.key === 'last_used_ip')"
          data-test="last-used-ip"
        >
          <slot name="cell-last_used_ip" :value="row.last_used_ip" :row="row" />
        </div>
      </div>
      <slot name="empty" />
    </div>
  `,
}

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options'],
  emits: ['update:modelValue'],
  template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"></select>',
}

const SearchInputStub = {
  name: 'SearchInput',
  props: ['modelValue'],
  emits: ['update:modelValue', 'search'],
  template: '<input :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />',
}

const PaginationStub = {
  name: 'Pagination',
  props: ['page', 'total', 'pageSize'],
  emits: ['update:page', 'update:pageSize'],
  template: `
    <div>
      <button data-test="page-2" @click="$emit('update:page', 2)">2</button>
      <button data-test="page-size-50" @click="$emit('update:pageSize', 50)">50</button>
    </div>
  `,
}

const ApiKeyBulkActionsStub = {
  name: 'ApiKeyBulkActions',
  props: ['rows', 'selectedIds', 'groups', 'userGroupRates'],
  emits: ['update:selectedIds', 'busy-change', 'completed'],
  template: `
    <div v-if="selectedIds.length > 0" data-test="bulk-actions-stub">
      <span data-test="bulk-selected-ids">{{ selectedIds.join(',') }}</span>
      <button data-test="bulk-clear-stub" @click="$emit('update:selectedIds', [])">Clear</button>
    </div>
  `,
}

const IconStub = {
  props: ['name'],
  template: '<span data-test="icon">{{ name }}</span>',
}

const mountView = async () => {
  const wrapper = mount(KeysView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        TablePageLayout: TablePageLayoutStub,
        DataTable: DataTableStub,
        ApiKeyBulkActions: ApiKeyBulkActionsStub,
        Pagination: PaginationStub,
        BaseDialog: true,
        ConfirmDialog: true,
        EmptyState: true,
        Select: SelectStub,
        SearchInput: SearchInputStub,
        Icon: IconStub,
        UseKeyModal: true,
        EndpointPopover: true,
        GroupBadge: true,
        GroupOptionItem: true,
        Teleport: true,
      },
    },
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

const visibleColumnKeys = (wrapper: VueWrapper) =>
  wrapper.get('[data-test="columns"]').text().split(',').filter(Boolean)

const visibleColumnMeta = (wrapper: VueWrapper): Array<{ key: string; sortable: boolean }> =>
  JSON.parse(wrapper.get('[data-test="columns-meta"]').text())

const getButtonByText = (wrapper: VueWrapper, text: string) => {
  const button = wrapper.findAll('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`Button not found: ${text}`)
  }
  return button
}

describe('user KeysView column settings', () => {
  beforeEach(() => {
    localStorage.clear()

    listKeys.mockReset()
    getPublicSettings.mockReset()
    getDashboardApiKeysUsage.mockReset()
    getAvailableGroups.mockReset()
    getUserGroupRates.mockReset()
    showError.mockReset()
    showSuccess.mockReset()
    copyToClipboard.mockReset()
    isCurrentStep.mockReset()
    nextStep.mockReset()

    listKeys.mockResolvedValue({
      items: [createApiKey()],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    getPublicSettings.mockResolvedValue({})
    getDashboardApiKeysUsage.mockResolvedValue({ stats: {} })
    getAvailableGroups.mockResolvedValue([])
    getUserGroupRates.mockResolvedValue({})
    isCurrentStep.mockReturnValue(false)
  })

  it('uses the default API key columns with low-frequency columns hidden', async () => {
    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'group',
      'current_concurrency',
      'usage',
      'expires_at',
      'status',
      'created_at',
      'actions',
    ])
    expect(visibleColumnKeys(wrapper)).not.toContain('rate_limit')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_at')
    expect(visibleColumnKeys(wrapper)).not.toContain('last_used_ip')
    expect(visibleColumnKeys(wrapper)).not.toContain('id')
  })

  it('shows a hidden column when toggled and persists the preference', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Rate Limit').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('rate_limit')
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['id', 'last_used_at', 'last_used_ip'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('shows the API key ID column when toggled', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'ID').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('id')
    expect(wrapper.get('[data-test="key-id"]').text()).toBe('#1')
    expect(visibleColumnMeta(wrapper).find((column) => column.key === 'id')?.sortable).toBe(true)
  })

  it('shows the last used IP column when toggled', async () => {
    listKeys.mockResolvedValueOnce({
      items: [{ ...createApiKey(), last_used_ip: '203.0.113.10' }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await getButtonByText(wrapper, 'Last Used IP').trigger('click')
    await nextTick()

    expect(visibleColumnKeys(wrapper)).toContain('last_used_ip')
    expect(wrapper.get('[data-test="last-used-ip"]').text()).toBe('203.0.113.10')
  })

  it('restores column preferences from localStorage on mount', async () => {
    localStorage.setItem('api-key-hidden-columns', JSON.stringify(['group', 'created_at']))
    localStorage.setItem('api-key-column-settings-version', '1')

    const wrapper = await mountView()

    expect(visibleColumnKeys(wrapper)).toEqual([
      'name',
      'key',
      'current_concurrency',
      'usage',
      'rate_limit',
      'expires_at',
      'status',
      'last_used_at',
      'actions',
    ])
    expect(localStorage.getItem('api-key-hidden-columns')).toBe(
      JSON.stringify(['group', 'created_at', 'last_used_ip', 'id'])
    )
    expect(localStorage.getItem('api-key-column-settings-version')).toBe('3')
  })

  it('does not include always-visible columns in the toggleable menu', async () => {
    const wrapper = await mountView()

    await wrapper.get('button[title="Column Settings"]').trigger('click')
    await nextTick()

    const columnMenuText = wrapper.text()
    expect(columnMenuText).toContain('API Key')
    expect(columnMenuText).toContain('ID')
    expect(columnMenuText).toContain('Current Concurrency')
    expect(columnMenuText).toContain('Rate Limit')
    expect(columnMenuText).toContain('Last Used IP')
    expect(columnMenuText).not.toContain('Name')
    expect(columnMenuText).not.toContain('Actions')
  })

  it('renders the current concurrency value', async () => {
    const wrapper = await mountView()

    expect(wrapper.get('[data-test="current-concurrency"]').text()).toBe('3')
  })

  it('shows the compact balance warning only for a bound group that is currently ineligible', async () => {
    const blockedGroup = {
      id: 42,
      name: 'Threshold Group With A Very Long Display Name',
      minimum_balance: 100,
      current_balance: 80,
      usable_balance: 0,
      balance_gap: 20,
      balance_eligible: false,
    }
    getAvailableGroups.mockResolvedValue([blockedGroup])
    listKeys.mockResolvedValue({
      items: [{ ...createApiKey(), group_id: 42, group: blockedGroup }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const blocked = await mountView()
    expect(blocked.find('[data-test="group-balance-warning"]').exists()).toBe(true)
    expect(blocked.get('[data-test="api-key-group-cell-content"]').classes()).toEqual(
      expect.arrayContaining(['min-w-0', 'max-w-full', 'flex-col', 'items-end', 'md:flex-row'])
    )
    expect(blocked.get('[data-test="api-key-group-selector"]').classes()).toEqual(
      expect.arrayContaining(['min-w-0', 'max-w-full', 'md:justify-start'])
    )
    expect(blocked.get('group-badge-stub').classes()).toEqual(
      expect.arrayContaining(['min-w-0', 'max-w-full'])
    )
    blocked.unmount()

    const healthyGroup = {
      ...blockedGroup,
      current_balance: 120,
      usable_balance: 20,
      balance_gap: 0,
      balance_eligible: true,
    }
    getAvailableGroups.mockResolvedValue([healthyGroup])
    listKeys.mockResolvedValue({
      items: [{ ...createApiKey(), group_id: 42, group: healthyGroup }],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const healthy = await mountView()
    expect(healthy.find('[data-test="group-balance-warning"]').exists()).toBe(false)
  })

  it('marks current concurrency as sortable', async () => {
    const wrapper = await mountView()

    const currentConcurrencyColumn = visibleColumnMeta(wrapper).find(
      (column) => column.key === 'current_concurrency'
    )
    expect(currentConcurrencyColumn?.sortable).toBe(true)
  })

  it('keeps filters and selected page size when sorting by current concurrency', async () => {
    getAvailableGroups.mockResolvedValue([{ id: 42, name: 'OpenAI' }])
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()

    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('update:modelValue', 'target')
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()

    const selects = wrapper.findAllComponents({ name: 'Select' })
    await selects[0].vm.$emit('update:modelValue', 42)
    await flushPromises()
    await selects[1].vm.$emit('update:modelValue', 'active')
    await flushPromises()

    listKeys.mockClear()

    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      50,
      {
        search: 'target',
        status: 'active',
        group_id: 42,
        sort_by: 'current_concurrency',
        sort_order: 'asc',
      },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('wires current-page row selection into the custom bulk actions bridge', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })

    expect(table.props('selectable')).toBe(true)
    expect(table.props('rowKey')).toBe('id')
    expect(table.props('selectedKeys')).toEqual([])
    expect(table.props('selectionLabel')(createApiKey())).toBe('Select API key test-key')

    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()

    expect(wrapper.get('[data-test="bulk-selected-ids"]').text()).toBe('1')
    await wrapper.get('[data-test="bulk-clear-stub"]').trigger('click')
    expect(table.props('selectedKeys')).toEqual([])
  })

  it('keeps the desktop table region shrinkable so every current-page row remains scrollable', async () => {
    listKeys.mockResolvedValueOnce({
      items: Array.from({ length: 9 }, (_, index) => ({
        ...createApiKey(),
        id: index + 1,
        key: `sk-test-key-${index + 1}`,
        name: `test-key-${index + 1}`,
      })),
      total: 9,
      page: 1,
      page_size: 20,
      pages: 1,
    })

    const wrapper = await mountView()
    const tableRegion = wrapper.get('[data-test="api-key-table-region"]')
    const table = wrapper.findComponent({ name: 'DataTable' })

    expect(table.props('data')).toHaveLength(9)
    expect(tableRegion.classes()).toEqual(
      expect.arrayContaining(['lg:flex', 'lg:min-h-0', 'lg:flex-1', 'lg:flex-col'])
    )
  })

  it('clears current-page selection when the table query changes or refreshes', async () => {
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })

    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    await wrapper.get('[data-test="page-size-50"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])

    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    await wrapper.findComponent({ name: 'SearchInput' }).vm.$emit('search')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])

    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    await wrapper.get('[data-test="sort-current-concurrency"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])

    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()
    await wrapper.get('button[title="Refresh"]').trigger('click')
    await flushPromises()
    expect(table.props('selectedKeys')).toEqual([])
  })

  it('reloads after a partial bulk action and keeps failed current-page ids selected', async () => {
    listKeys.mockResolvedValue({
      items: [createApiKey(), { ...createApiKey(), id: 2, key: 'sk-test-key-2', name: 'test-key-2' }],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = await mountView()
    const table = wrapper.findComponent({ name: 'DataTable' })

    table.vm.$emit('update:selectedKeys', [1, 2])
    await nextTick()
    wrapper.findComponent({ name: 'ApiKeyBulkActions' }).vm.$emit('completed', {
      action: 'group',
      succeededIds: [1],
      failedIds: [2],
      skippedIds: [],
    })
    await flushPromises()

    expect(table.props('selectedKeys')).toEqual([2])
    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      20,
      expect.any(Object),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('moves to the previous page after deleting every row on a non-first page', async () => {
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()

    wrapper.findComponent({ name: 'ApiKeyBulkActions' }).vm.$emit('completed', {
      action: 'delete',
      succeededIds: [1],
      failedIds: [],
      skippedIds: [],
    })
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      1,
      20,
      expect.any(Object),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
  })

  it('stays on the current page when a bulk deletion partially fails', async () => {
    const wrapper = await mountView()

    await wrapper.get('[data-test="page-2"]').trigger('click')
    await flushPromises()
    const table = wrapper.findComponent({ name: 'DataTable' })
    table.vm.$emit('update:selectedKeys', [1])
    await nextTick()

    wrapper.findComponent({ name: 'ApiKeyBulkActions' }).vm.$emit('completed', {
      action: 'delete',
      succeededIds: [],
      failedIds: [1],
      skippedIds: [],
    })
    await flushPromises()

    expect(listKeys).toHaveBeenLastCalledWith(
      2,
      20,
      expect.any(Object),
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )
    expect(table.props('selectedKeys')).toEqual([1])
  })
})
