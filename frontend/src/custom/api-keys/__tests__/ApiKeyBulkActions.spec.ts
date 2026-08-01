import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ApiKey, Group } from '@/types'
import ApiKeyBulkActions from '../ApiKeyBulkActions.vue'
import { customApiKeyBulkText } from '../i18n'

const {
  updateKey,
  toggleStatus,
  deleteKey,
  showSuccess,
  showWarning,
  showError,
  showInfo
} = vi.hoisted(() => ({
  updateKey: vi.fn(),
  toggleStatus: vi.fn(),
  deleteKey: vi.fn(),
  showSuccess: vi.fn(),
  showWarning: vi.fn(),
  showError: vi.fn(),
  showInfo: vi.fn()
}))

vi.mock('@/api', () => ({
  keysAPI: {
    update: updateKey,
    toggleStatus,
    delete: deleteKey
  }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showSuccess,
    showWarning,
    showError,
    showInfo
  })
}))

vi.mock('vue-i18n', () => ({
  useI18n: () => ({
    locale: ref('zh-CN'),
    t: (key: string) => key
  })
}))

const createApiKey = (id: number, overrides: Partial<ApiKey> = {}): ApiKey => ({
  id,
  user_id: 1,
  key: `sk-test-${id}`,
  name: `key-${id}`,
  group_id: null,
  status: 'active',
  ip_whitelist: [],
  ip_blacklist: [],
  last_used_at: null,
  last_used_ip: null,
  quota: 0,
  quota_used: 0,
  expires_at: null,
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z',
  current_concurrency: 0,
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
  ...overrides
})

const createGroup = (id: number): Group => ({
  id,
  name: `group-${id}`,
  description: `group ${id}`,
  platform: 'openai',
  rate_multiplier: 1,
  minimum_balance: 0,
  is_exclusive: false,
  status: 'active',
  subscription_type: 'standard',
  daily_limit_usd: null,
  weekly_limit_usd: null,
  monthly_limit_usd: null,
  allow_image_generation: true,
  allow_batch_image_generation: true,
  image_rate_independent: false,
  image_rate_multiplier: 1,
  batch_image_discount_multiplier: 1,
  batch_image_hold_multiplier: 1,
  image_price_1k: null,
  image_price_2k: null,
  image_price_4k: null,
  video_rate_independent: false,
  video_rate_multiplier: 1,
  video_price_480p: null,
  video_price_720p: null,
  video_price_1080p: null,
  web_search_price_per_call: null,
  peak_rate_enabled: false,
  peak_start: '00:00',
  peak_end: '00:00',
  peak_rate_multiplier: 1,
  claude_code_only: false,
  fallback_group_id: null,
  fallback_group_id_on_invalid_request: null,
  require_oauth_only: false,
  require_privacy_set: false,
  created_at: '2026-07-25T00:00:00Z',
  updated_at: '2026-07-25T00:00:00Z'
})

const SelectStub = {
  name: 'Select',
  props: ['modelValue', 'options', 'disabled'],
  emits: ['update:modelValue'],
  template: `
    <select
      data-test="bulk-group-select-stub"
      :value="modelValue ?? ''"
      :disabled="disabled"
      @change="$emit('update:modelValue', Number($event.target.value))"
    >
      <option value="">select</option>
      <option v-for="option in options" :key="option.value" :value="option.value">
        {{ option.label }}
      </option>
    </select>
  `
}

const ConfirmDialogStub = {
  name: 'ConfirmDialog',
  props: ['show', 'title', 'message', 'confirmText', 'danger'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="bulk-confirm-dialog" :data-danger="String(danger)">
      <span data-test="confirm-title">{{ title }}</span>
      <span data-test="confirm-message">{{ message }}</span>
      <button data-test="confirm-action" @click="$emit('confirm')">{{ confirmText }}</button>
      <button data-test="cancel-action" @click="$emit('cancel')">cancel</button>
    </div>
  `
}

const mountActions = (
  rows: ApiKey[],
  selectedIds: number[],
  groups: Group[] = [createGroup(7)]
) => mount(ApiKeyBulkActions, {
  props: {
    rows,
    selectedIds,
    groups,
    userGroupRates: {}
  },
  global: {
    stubs: {
      Select: SelectStub,
      ConfirmDialog: ConfirmDialogStub,
      GroupBadge: true,
      GroupOptionItem: true,
      Icon: true
    }
  }
})

describe('ApiKeyBulkActions', () => {
  beforeEach(() => {
    updateKey.mockReset()
    toggleStatus.mockReset()
    deleteKey.mockReset()
    showSuccess.mockReset()
    showWarning.mockReset()
    showError.mockReset()
    showInfo.mockReset()
    updateKey.mockResolvedValue({})
    toggleStatus.mockResolvedValue({})
    deleteKey.mockResolvedValue({})
  })

  it('only renders for selected current-page rows and can clear the selection', async () => {
    const empty = mountActions([createApiKey(1)], [])
    expect(empty.find('[data-test="api-key-bulk-actions"]').exists()).toBe(false)

    const wrapper = mountActions([createApiKey(1)], [1])
    expect(wrapper.get('[data-test="bulk-selected-count"]').text()).toContain('1')

    await wrapper.get('[data-test="bulk-clear"]').trigger('click')
    expect(wrapper.emitted('update:selectedIds')?.at(-1)?.[0]).toEqual([])
  })

  it('requires a target group before applying a group change', async () => {
    const wrapper = mountActions([createApiKey(1)], [1])

    expect(wrapper.get('[data-test="bulk-apply-group"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="bulk-apply-group"]').trigger('click')

    expect(updateKey).not.toHaveBeenCalled()
  })

  it('disables only groups that currently fail the minimum-balance gate', () => {
    const healthy = createGroup(7)
    const blocked = {
      ...createGroup(8),
      minimum_balance: 100,
      current_balance: 80,
      usable_balance: 0,
      balance_gap: 20,
      balance_eligible: false
    }
    const wrapper = mountActions([createApiKey(1)], [1], [healthy, blocked])
    const options = wrapper.findComponent({ name: 'Select' }).props('options') as Array<{
      value: number
      disabled: boolean
      balanceRequirement: unknown
    }>

    expect(options.find((option) => option.value === 7)).toMatchObject({
      disabled: false,
      balanceRequirement: null
    })
    expect(options.find((option) => option.value === 8)).toMatchObject({
      disabled: true,
      balanceRequirement: expect.objectContaining({
        minimumBalance: 100,
        currentBalance: 80,
        balanceGap: 20
      })
    })
  })

  it('changes groups without confirmation and skips rows already in the target group', async () => {
    const wrapper = mountActions([
      createApiKey(1),
      createApiKey(2, { group_id: 7 })
    ], [1, 2])

    await wrapper.get('[data-test="bulk-group-select"]').setValue('7')
    await wrapper.get('[data-test="bulk-apply-group"]').trigger('click')
    await flushPromises()

    expect(updateKey).toHaveBeenCalledTimes(1)
    expect(updateKey).toHaveBeenCalledWith(1, { group_id: 7 })
    expect(wrapper.find('[data-test="bulk-confirm-dialog"]').exists()).toBe(false)
    expect(wrapper.emitted('completed')?.at(-1)?.[0]).toEqual({
      action: 'group',
      succeededIds: [1],
      failedIds: [],
      skippedIds: [2]
    })
    expect(wrapper.emitted('update:selectedIds')?.at(-1)?.[0]).toEqual([])
  })

  it('retains only failed group updates for retry', async () => {
    updateKey.mockImplementation((id: number) =>
      id === 2 ? Promise.reject(new Error('failed')) : Promise.resolve({})
    )
    const wrapper = mountActions([createApiKey(1), createApiKey(2)], [1, 2])

    await wrapper.get('[data-test="bulk-group-select"]').setValue('7')
    await wrapper.get('[data-test="bulk-apply-group"]').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:selectedIds')?.at(-1)?.[0]).toEqual([2])
    expect(showWarning).toHaveBeenCalledOnce()
  })

  it('shows minimum-balance details when a stale bulk group selection is rejected', async () => {
    updateKey.mockRejectedValue({
      reason: 'GROUP_MINIMUM_BALANCE_NOT_MET',
      metadata: {
        group_name: '分组 A',
        current_balance: '80',
        minimum_balance: '100',
        balance_gap: '20'
      }
    })
    const wrapper = mountActions([createApiKey(1)], [1])

    await wrapper.get('[data-test="bulk-group-select"]').setValue('7')
    await wrapper.get('[data-test="bulk-apply-group"]').trigger('click')
    await flushPromises()

    expect(showError).toHaveBeenCalledWith(
      '无法使用“分组 A”：当前余额 $80.00，余额需高于 $100.00，还需增加超过 $20.00。'
    )
    expect(wrapper.emitted('update:selectedIds')?.at(-1)?.[0]).toEqual([1])
  })

  it('requires confirmation before disabling and skips inactive rows', async () => {
    const wrapper = mountActions([
      createApiKey(1),
      createApiKey(2, { status: 'inactive' })
    ], [1, 2])

    await wrapper.get('[data-test="bulk-disable"]').trigger('click')
    expect(toggleStatus).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('1 个 API 密钥')
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('1 项会跳过')
    expect(wrapper.get('[data-test="bulk-confirm-dialog"]').attributes('data-danger')).toBe('true')

    await wrapper.get('[data-test="confirm-action"]').trigger('click')
    await flushPromises()

    expect(toggleStatus).toHaveBeenCalledTimes(1)
    expect(toggleStatus).toHaveBeenCalledWith(1, 'inactive')
    expect(wrapper.emitted('completed')?.at(-1)?.[0]).toEqual({
      action: 'disable',
      succeededIds: [1],
      failedIds: [],
      skippedIds: [2]
    })
  })

  it('describes skipped disable rows without assuming their status', () => {
    const zhMessage = customApiKeyBulkText('zh-CN', 'disableConfirmMessage', { count: 1, skipped: 3 })
    const enMessage = customApiKeyBulkText('en', 'disableConfirmMessage', { count: 1, skipped: 3 })

    expect(zhMessage).toContain('另有 3 项会跳过')
    expect(zhMessage).not.toContain('已禁用项')
    expect(enMessage).toContain('skip 3 other items')
    expect(enMessage).not.toContain('already disabled')
  })

  it('disables every selected key when all rows are active', async () => {
    const wrapper = mountActions([createApiKey(1), createApiKey(2)], [1, 2])

    await wrapper.get('[data-test="bulk-disable"]').trigger('click')
    await wrapper.get('[data-test="confirm-action"]').trigger('click')
    await flushPromises()

    expect(toggleStatus).toHaveBeenCalledTimes(2)
    expect(toggleStatus).toHaveBeenCalledWith(1, 'inactive')
    expect(toggleStatus).toHaveBeenCalledWith(2, 'inactive')
  })

  it('disables only active keys and preserves recoverable runtime states', async () => {
    const wrapper = mountActions([
      createApiKey(1),
      createApiKey(2, { status: 'inactive' }),
      createApiKey(3, { status: 'expired' }),
      createApiKey(4, { status: 'quota_exhausted' })
    ], [1, 2, 3, 4])

    await wrapper.get('[data-test="bulk-disable"]').trigger('click')
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('1 个 API 密钥')
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('3 项会跳过')
    expect(wrapper.get('[data-test="confirm-message"]').text()).not.toContain('已禁用项')

    await wrapper.get('[data-test="confirm-action"]').trigger('click')
    await flushPromises()

    expect(toggleStatus).toHaveBeenCalledOnce()
    expect(toggleStatus).toHaveBeenCalledWith(1, 'inactive')
    expect(wrapper.emitted('completed')?.at(-1)?.[0]).toEqual({
      action: 'disable',
      succeededIds: [1],
      failedIds: [],
      skippedIds: [2, 3, 4]
    })
  })

  it('does not send disable requests for recoverable runtime states', async () => {
    const wrapper = mountActions([
      createApiKey(1, { status: 'expired' }),
      createApiKey(2, { status: 'quota_exhausted' })
    ], [1, 2])

    await wrapper.get('[data-test="bulk-disable"]').trigger('click')

    expect(toggleStatus).not.toHaveBeenCalled()
    expect(wrapper.find('[data-test="bulk-confirm-dialog"]').exists()).toBe(false)
    expect(wrapper.emitted('completed')?.at(-1)?.[0]).toEqual({
      action: 'disable',
      succeededIds: [],
      failedIds: [],
      skippedIds: [1, 2]
    })
  })

  it('changes the status action to enable when all selected rows are inactive', async () => {
    const wrapper = mountActions([
      createApiKey(1, { status: 'inactive' }),
      createApiKey(2, { status: 'inactive' })
    ], [1, 2])

    expect(wrapper.find('[data-test="bulk-disable"]').exists()).toBe(false)
    expect(wrapper.get('[data-test="bulk-enable"]').text()).toContain('批量启用')

    await wrapper.get('[data-test="bulk-enable"]').trigger('click')

    expect(toggleStatus).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('立即恢复调用能力')
    expect(wrapper.get('[data-test="bulk-confirm-dialog"]').attributes('data-danger')).toBe('false')

    await wrapper.get('[data-test="confirm-action"]').trigger('click')
    await flushPromises()

    expect(toggleStatus).toHaveBeenCalledTimes(2)
    expect(toggleStatus).toHaveBeenCalledWith(1, 'active')
    expect(toggleStatus).toHaveBeenCalledWith(2, 'active')
    expect(wrapper.emitted('completed')?.at(-1)?.[0]).toEqual({
      action: 'enable',
      succeededIds: [1, 2],
      failedIds: [],
      skippedIds: []
    })
  })

  it('retains failed enables selected for retry', async () => {
    toggleStatus.mockImplementation((id: number) =>
      id === 2 ? Promise.reject(new Error('failed')) : Promise.resolve({})
    )
    const wrapper = mountActions([
      createApiKey(1, { status: 'inactive' }),
      createApiKey(2, { status: 'inactive' })
    ], [1, 2])

    await wrapper.get('[data-test="bulk-enable"]').trigger('click')
    await wrapper.get('[data-test="confirm-action"]').trigger('click')
    await flushPromises()

    expect(wrapper.emitted('update:selectedIds')?.at(-1)?.[0]).toEqual([2])
    expect(showWarning).toHaveBeenCalledOnce()
  })

  it('requires confirmation before deletion and preserves failed deletions', async () => {
    deleteKey.mockImplementation((id: number) =>
      id === 2 ? Promise.reject(new Error('failed')) : Promise.resolve({})
    )
    const wrapper = mountActions([createApiKey(1), createApiKey(2)], [1, 2])

    await wrapper.get('[data-test="bulk-delete"]').trigger('click')
    expect(deleteKey).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('2 个 API 密钥')

    await wrapper.get('[data-test="confirm-action"]').trigger('click')
    await flushPromises()

    expect(deleteKey).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('update:selectedIds')?.at(-1)?.[0]).toEqual([2])
    expect(showWarning).toHaveBeenCalledOnce()
  })

  it('blocks duplicate submissions while an action is running', async () => {
    let resolveUpdate: (() => void) | undefined
    updateKey.mockImplementation(() => new Promise<void>((resolve) => {
      resolveUpdate = resolve
    }))
    const wrapper = mountActions([createApiKey(1)], [1])

    await wrapper.get('[data-test="bulk-group-select"]').setValue('7')
    await wrapper.get('[data-test="bulk-apply-group"]').trigger('click')
    await wrapper.get('[data-test="bulk-apply-group"]').trigger('click')

    expect(updateKey).toHaveBeenCalledTimes(1)
    expect(wrapper.get('[data-test="bulk-apply-group"]').attributes('disabled')).toBeDefined()

    resolveUpdate?.()
    await flushPromises()
  })
})
