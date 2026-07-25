import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { ApiKey, Group } from '@/types'
import ApiKeyBulkActions from '../ApiKeyBulkActions.vue'

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
  props: ['show', 'title', 'message', 'confirmText'],
  emits: ['confirm', 'cancel'],
  template: `
    <div v-if="show" data-test="bulk-confirm-dialog">
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

  it('requires confirmation before disabling and skips inactive rows', async () => {
    const wrapper = mountActions([
      createApiKey(1),
      createApiKey(2, { status: 'inactive' })
    ], [1, 2])

    await wrapper.get('[data-test="bulk-disable"]').trigger('click')
    expect(toggleStatus).not.toHaveBeenCalled()
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('1 个 API 密钥')
    expect(wrapper.get('[data-test="confirm-message"]').text()).toContain('1 个已禁用项')

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

  it('does not open a confirmation or send requests when all rows are already disabled', async () => {
    const wrapper = mountActions([
      createApiKey(1, { status: 'inactive' }),
      createApiKey(2, { status: 'inactive' })
    ], [1, 2])

    await wrapper.get('[data-test="bulk-disable"]').trigger('click')

    expect(wrapper.find('[data-test="bulk-confirm-dialog"]').exists()).toBe(false)
    expect(toggleStatus).not.toHaveBeenCalled()
    expect(showInfo).toHaveBeenCalledOnce()
    expect(wrapper.emitted('update:selectedIds')?.at(-1)?.[0]).toEqual([])
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
