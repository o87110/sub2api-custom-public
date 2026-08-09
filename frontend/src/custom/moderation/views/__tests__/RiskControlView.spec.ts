import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import type { DOMWrapper, VueWrapper } from '@vue/test-utils'

import RiskControlView from '../RiskControlView.vue'
import type {
  ContentModerationLog,
} from '@/api/admin/riskControl'
import type {
  CustomContentModerationConfig,
  CustomUpdateContentModerationConfig,
} from '@/custom/moderation/api'
import type { AdminGroup } from '@/types'

const {
  getConfig,
  updateConfig,
  getStatus,
  listLogs,
  testAPIKeys,
  getGroups,
  getProxies,
  showError,
  showSuccess,
} = vi.hoisted(() => ({
  getConfig: vi.fn(),
  updateConfig: vi.fn(),
  getStatus: vi.fn(),
  listLogs: vi.fn(),
  testAPIKeys: vi.fn(),
  getGroups: vi.fn(),
  getProxies: vi.fn(),
  showError: vi.fn(),
  showSuccess: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    riskControl: {
      getConfig,
      updateConfig,
      getStatus,
      listLogs,
      testAPIKeys,
      deleteFlaggedHash: vi.fn(),
      clearFlaggedHashes: vi.fn(),
      unbanUser: vi.fn(),
    },
    groups: {
      getAllIncludingInactive: getGroups,
    },
    proxies: {
      getAll: getProxies,
    },
  },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError,
    showSuccess,
  }),
}))

vi.mock('@/utils/apiError', () => ({
  extractApiErrorMessage: (_err: unknown, fallback: string) => fallback,
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'zh-CN' },
      t: (key: string, params?: Record<string, string | number>) => {
        if (key === 'admin.riskControl.preBlockAPIKeyLoadSummary') {
          return `同步并发 ${params?.active} / 可用 Key ${params?.available}，累计 ${params?.total} 次，worker：${params?.workerActive} / ${params?.workerTotal}`
        }
        return key.replace(/\{(\w+)\}/g, (_, token) => String(params?.[token] ?? `{${token}}`))
      },
    }),
  }
})

const baseConfig = (): CustomContentModerationConfig => ({
  enabled: true,
  mode: 'pre_block',
  base_url: 'https://api.openai.com',
  model: 'omni-moderation-latest',
  proxy_id: null,
  api_key_configured: false,
  api_key_masked: '',
  api_key_count: 0,
  api_key_masks: [],
  api_key_statuses: [],
  timeout_ms: 3000,
  sample_rate: 100,
  all_groups: true,
  group_ids: [],
  record_non_hits: false,
  worker_count: 4,
  queue_size: 32768,
  block_status: 403,
  block_message: '内容审计命中风险规则，请调整输入后重试',
  email_on_hit: true,
  auto_ban_enabled: true,
  ban_threshold: 10,
  user_ban_thresholds: [],
  violation_window_hours: 720,
  retry_count: 2,
  hit_retention_days: 180,
  non_hit_retention_days: 3,
  pre_hash_check_enabled: false,
  blocked_keywords: [],
  keyword_blocking_mode: 'keyword_and_api',
  thresholds: {
    harassment: 0.98,
    sexual: 0.65,
  },
  model_filter: {
    type: 'all',
    models: [],
  },
})

const groupFixture = (id: number, name: string): AdminGroup => ({
  id,
  name,
  platform: 'openai',
  status: 'active',
} as AdminGroup)

const runtimeStatus = () => ({
  enabled: true,
  risk_control_enabled: true,
  mode: 'pre_block',
  worker_count: 4,
  max_workers: 32,
  active_workers: 0,
  idle_workers: 4,
  queue_size: 32768,
  queue_length: 0,
  queue_usage_percent: 0,
  enqueued: 0,
  dropped: 0,
  processed: 0,
  errors: 0,
  pre_block_active: 0,
  pre_block_checked: 0,
  pre_block_allowed: 0,
  pre_block_blocked: 0,
  pre_block_errors: 0,
  pre_block_avg_latency_ms: 0,
  pre_block_api_key_active: 0,
  pre_block_api_key_available_count: 0,
  pre_block_api_key_total_calls: 0,
  pre_block_api_key_loads: [],
  api_key_statuses: [],
  flagged_hash_count: 0,
  last_cleanup_deleted_hit: 0,
  last_cleanup_deleted_non_hit: 0,
})

const contentModerationLog = (overrides: Partial<ContentModerationLog> = {}): ContentModerationLog => ({
  id: 1,
  request_id: 'req-1',
  user_id: 1,
  user_email: 'user@example.com',
  api_key_id: 1,
  api_key_name: 'test-key',
  group_id: 1,
  group_name: 'default',
  endpoint: '/v1/responses',
  provider: 'openai',
  model: 'gpt-test',
  mode: 'pre_block',
  action: 'allow',
  flagged: false,
  highest_category: '',
  highest_score: 0,
  matched_keyword: '',
  category_scores: {},
  threshold_snapshot: {},
  input_excerpt: '普通输入摘要',
  upstream_latency_ms: null,
  error: '',
  violation_count: 0,
  auto_banned: false,
  email_sent: false,
  user_status: 'active',
  queue_delay_ms: null,
  created_at: '2026-07-19T09:00:00Z',
  ...overrides,
})

const AppLayoutStub = { template: '<div><slot /></div>' }
const BaseDialogStub = defineComponent({
  props: {
    show: {
      type: Boolean,
      default: false,
    },
  },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})
const ModelWhitelistSelectorStub = defineComponent({
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
  },
  emits: ['update:modelValue'],
  setup(props, { emit }) {
    const onInput = (event: Event) => {
      const value = (event.target as HTMLInputElement).value
      emit(
        'update:modelValue',
        value
          .split(/[,\n]/)
          .map((item) => item.trim())
          .filter(Boolean)
      )
    }
    return () =>
      h('input', {
        'data-test': 'model-filter-input',
        value: (props.modelValue as string[]).join('\n'),
        onInput,
      })
  },
})
const UserBanThresholdOverridesStub = defineComponent({
  name: 'UserBanThresholdOverrides',
  props: {
    modelValue: {
      type: Array,
      default: () => [],
    },
    defaultThreshold: {
      type: Number,
      default: 10,
    },
  },
  emits: ['update:modelValue'],
  template: '<div data-test="user-ban-threshold-overrides" />',
})
const ProxySelectorStub = defineComponent({
  props: {
    modelValue: {
      type: Number,
      default: null,
    },
    proxies: {
      type: Array,
      default: () => [],
    },
  },
  template: '<div data-test="proxy-selector">{{ modelValue }}:{{ proxies.length }}</div>',
})

function findButtonByText(wrapper: VueWrapper, text: string): DOMWrapper<HTMLButtonElement> {
  const button = wrapper.findAll<HTMLButtonElement>('button').find((item) => item.text().includes(text))
  if (!button) {
    throw new Error(`button not found: ${text}`)
  }
  return button
}

function mountRiskControlView(): VueWrapper {
  return mount(RiskControlView, {
    global: {
      stubs: {
        AppLayout: AppLayoutStub,
        BaseDialog: BaseDialogStub,
        Icon: true,
        Select: true,
        Toggle: true,
        Pagination: true,
        ModelWhitelistSelector: ModelWhitelistSelectorStub,
        UserBanThresholdOverrides: UserBanThresholdOverridesStub,
        ProxySelector: ProxySelectorStub,
      },
    },
  })
}

describe('admin RiskControlView', () => {
  beforeEach(() => {
    getConfig.mockReset()
    updateConfig.mockReset()
    getStatus.mockReset()
    listLogs.mockReset()
    testAPIKeys.mockReset()
    getGroups.mockReset()
    getProxies.mockReset()
    showError.mockReset()
    showSuccess.mockReset()

    getConfig.mockResolvedValue(baseConfig())
    getStatus.mockResolvedValue(runtimeStatus())
    listLogs.mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20, pages: 1 })
    getGroups.mockResolvedValue([])
    getProxies.mockResolvedValue([])
    testAPIKeys.mockResolvedValue({ items: [], audit_result: null })
    updateConfig.mockImplementation(async (payload: CustomUpdateContentModerationConfig) => ({
      ...baseConfig(),
      ...payload,
      model_filter: payload.model_filter ?? baseConfig().model_filter,
      api_key_configured: false,
      api_key_masked: '',
      api_key_count: 0,
      api_key_masks: [],
      api_key_statuses: [],
    }))
  })

  it('loads and preserves the selected moderation proxy for save and API key tests', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      proxy_id: 7,
      api_key_configured: true,
      api_key_count: 1,
    })
    getProxies.mockResolvedValue([{ id: 7, name: 'Proxy Seven' }])
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    expect(wrapper.get('[data-test="proxy-selector"]').text()).toBe('7:1')

    await findButtonByText(wrapper, 'admin.riskControl.testStoredApiKeys').trigger('click')
    await flushPromises()
    expect(testAPIKeys).toHaveBeenCalledWith(expect.objectContaining({ proxy_id: 7 }))

    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({ proxy_id: 7 }))
  })

  it('keeps the moderation page available when loading proxies fails', async () => {
    getProxies.mockRejectedValue(new Error('proxy list unavailable'))
    const wrapper = mountRiskControlView()

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.openSettings')
    expect(showError).not.toHaveBeenCalled()
  })

  it('preserves legacy API audit behavior when the response omits api_audit_scope', async () => {
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      api_audit_scope: {
        all_in_scope: true,
        group_ids: [],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('loads and saves user-specific ban threshold overrides', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      user_ban_thresholds: [{ user_id: 1001, ban_threshold: 25 }],
    })
    const wrapper = mountRiskControlView()
    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.response').trigger('click')
    const overrides = wrapper.getComponent(UserBanThresholdOverridesStub)
    expect(overrides.props('modelValue')).toEqual([{ user_id: 1001, ban_threshold: 25 }])
    overrides.vm.$emit('update:modelValue', [{ user_id: 1002, ban_threshold: 40 }])
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      user_ban_thresholds: [{ user_id: 1002, ban_threshold: 40 }],
    }))
  })

  it('blocks saving invalid user-specific ban thresholds', async () => {
    const wrapper = mountRiskControlView()
    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.response').trigger('click')
    wrapper.getComponent(UserBanThresholdOverridesStub).vm.$emit('update:modelValue', [
      { user_id: 1001, ban_threshold: 0 },
    ])
    await flushPromises()
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledWith('请先修正用户专属封禁阈值。')
  })

  it('allows saving a legacy empty overall audit scope', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      all_groups: false,
      group_ids: [],
    })
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      all_groups: false,
      group_ids: [],
      api_audit_scope: {
        all_in_scope: true,
        group_ids: [],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('saves an explicit API audit group subset', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      all_groups: false,
      group_ids: [1, 2],
      api_audit_scope: {
        all_in_scope: true,
        group_ids: [],
      },
    })
    getGroups.mockResolvedValue([
      groupFixture(1, 'Group One'),
      groupFixture(2, 'Group Two'),
      groupFixture(3, 'Group Three'),
    ])
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')
    await wrapper.get('[data-test="api-audit-selected-in-scope"]').trigger('click')
    await wrapper.get('[data-test="api-audit-group-2"]').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(wrapper.find('[data-test="api-audit-group-3"]').exists()).toBe(false)
    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [2],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('removes API audit groups that leave the overall audit scope', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      all_groups: false,
      group_ids: [1, 2],
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [1, 2],
      },
    })
    getGroups.mockResolvedValue([
      groupFixture(1, 'Group One'),
      groupFixture(2, 'Group Two'),
    ])
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')
    await wrapper.get('[data-test="overall-audit-group-2"]').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      group_ids: [1],
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [1],
      },
    }))
  })

  it('removes deleted persisted groups before saving moderation settings', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      all_groups: false,
      group_ids: [1, 71],
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [1, 71],
      },
    })
    getGroups.mockResolvedValue([groupFixture(1, 'Group One')])
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      group_ids: [1],
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [1],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('blocks saving when an active explicit API audit scope is empty', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [],
      },
    })
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).not.toHaveBeenCalled()
    expect(showError).toHaveBeenCalledTimes(1)
  })

  it('disables API audit controls for keyword-only pre-blocking and preserves the scope', async () => {
    getConfig.mockResolvedValue({
      ...baseConfig(),
      keyword_blocking_mode: 'keyword_only',
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [1],
      },
    })
    getGroups.mockResolvedValue([groupFixture(1, 'Group One')])
    const wrapper = mountRiskControlView()

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')

    expect(wrapper.get('[data-test="api-audit-all-in-scope"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="api-audit-selected-in-scope"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="api-audit-group-1"]').attributes('disabled')).toBeDefined()

    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      api_audit_scope: {
        all_in_scope: false,
        group_ids: [1],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('saves the selected model filter mode and models', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.scope').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.modelFilterInclude').trigger('click')
    await wrapper.get('[data-test="model-filter-input"]').setValue('gpt-5.5, gpt-5.4')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      model_filter: {
        type: 'include',
        models: ['gpt-5.5', 'gpt-5.4'],
      },
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('submits edited risk control thresholds when saving moderation config', async () => {
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    await findButtonByText(wrapper, 'admin.riskControl.openSettings').trigger('click')
    await findButtonByText(wrapper, 'admin.riskControl.tabs.riskThresholds').trigger('click')
    await wrapper.get('[data-test="risk-threshold-sexual"]').setValue('72')
    await wrapper.get('[data-test="risk-threshold-harassment"]').setValue('99')
    await findButtonByText(wrapper, 'admin.riskControl.saveConfig').trigger('click')
    await flushPromises()

    expect(updateConfig).toHaveBeenCalledWith(expect.objectContaining({
      thresholds: expect.objectContaining({
        sexual: 0.72,
        harassment: 0.99,
      }),
    }))
    expect(showError).not.toHaveBeenCalled()
  })

  it('describes worker runtime as async audit and pre-block record processing', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      mode: 'observe',
      processed: 12,
      queue_length: 2,
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.workerStatusHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('12')
    expect(wrapper.text()).toContain('2 / 32,768')
  })

  it('shows pre-block synchronous moderation metrics separately from worker queue', async () => {
    getStatus.mockResolvedValue({
      ...runtimeStatus(),
      pre_block_active: 2,
      pre_block_checked: 128,
      pre_block_allowed: 120,
      pre_block_blocked: 8,
      pre_block_errors: 1,
      pre_block_avg_latency_ms: 86,
      pre_block_api_key_active: 2,
      pre_block_api_key_available_count: 2,
      pre_block_api_key_total_calls: 128,
      active_workers: 3,
      worker_count: 7,
      pre_block_api_key_loads: [
        {
          index: 0,
          key_hash: 'hash-one',
          masked: 'sk-...one',
          status: 'ok',
          active: 1,
          total: 72,
          success: 70,
          errors: 2,
          avg_latency_ms: 84,
          last_latency_ms: 80,
          last_http_status: 200,
        },
        {
          index: 1,
          key_hash: 'hash-two',
          masked: 'sk-...two',
          status: 'ok',
          active: 1,
          total: 56,
          success: 56,
          errors: 0,
          avg_latency_ms: 90,
          last_latency_ms: 92,
          last_http_status: 200,
        },
      ],
    })

    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()

    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncStatus')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(wrapper.text()).not.toContain('admin.riskControl.workerStatus')
    expect(wrapper.text()).toContain('admin.riskControl.records')
    expect(wrapper.text()).toContain('128')
    expect(wrapper.text()).toContain('120')
    expect(wrapper.text()).toContain('8')
    expect(wrapper.text()).toContain('86 ms')
    expect(wrapper.text()).toContain('admin.riskControl.preBlockAPIKeyLoad')
    expect(wrapper.text()).toContain('sk-...one')
    expect(wrapper.text()).toContain('sk-...two')
    expect(wrapper.text()).toContain('72')
    expect(wrapper.text()).toContain('56')
    expect(wrapper.text()).toContain('同步并发 2 / 可用 Key 2，累计 128 次，worker：3 / 7')

    const runtimeCards = wrapper.get('[data-test="pre-block-runtime-cards"]')
    const syncCard = wrapper.get('[data-test="pre-block-sync-card"]')
    const apiKeyLoadCard = wrapper.get('[data-test="pre-block-api-key-load-card"]')

    expect(runtimeCards.classes()).toEqual(expect.arrayContaining([
      'grid',
      'grid-cols-1',
      'xl:grid-cols-[minmax(0,520px)_minmax(0,1fr)]',
    ]))
    expect(syncCard.element.parentElement).toBe(runtimeCards.element)
    expect(apiKeyLoadCard.element.parentElement).toBe(runtimeCards.element)
    expect(syncCard.classes()).toContain('card')
    expect(apiKeyLoadCard.classes()).toContain('card')
    expect(syncCard.get('h2').text()).toBe('admin.riskControl.preBlockSyncStatus')
    expect(syncCard.text()).toContain('admin.riskControl.preBlockSyncHint')
    expect(apiKeyLoadCard.get('h2').text()).toBe('admin.riskControl.preBlockAPIKeyLoad')
    expect(apiKeyLoadCard.text()).toContain('admin.riskControl.preBlockAPIKeyLoadHint')
    expect(wrapper.get('[data-test="pre-block-api-key-load-list"]').classes()).toEqual(expect.arrayContaining([
      'max-h-[280px]',
      'overflow-y-auto',
    ]))
  })

  it('labels combined keyword-block input detail as matched context', async () => {
    const excerpt = '输入开头\n…已省略中间内容…\n这里包含风控绕过关键词'
    listLogs.mockResolvedValue({
      items: [contentModerationLog({
        action: 'keyword_block',
        flagged: true,
        highest_category: 'keyword',
        highest_score: 1,
        matched_keyword: '风控绕过',
        input_excerpt: excerpt,
      })],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()
    const summaryButton = wrapper.findAll('button').find((button) => button.attributes('title') === excerpt)
    expect(summaryButton).toBeDefined()
    await summaryButton!.trigger('click')

    expect(wrapper.text()).toContain('输入开头与命中上下文')
    expect(wrapper.text()).toContain('风控绕过')
    expect(wrapper.text()).toContain('…已省略中间内容…')
  })

  it('labels legacy keyword-block input detail without the combined marker as input excerpt', async () => {
    const excerpt = '旧版关键词日志只保存输入开头'
    listLogs.mockResolvedValue({
      items: [contentModerationLog({
        action: 'keyword_block',
        flagged: true,
        highest_category: 'keyword',
        highest_score: 1,
        matched_keyword: '风控绕过',
        input_excerpt: excerpt,
      })],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()
    const summaryButton = wrapper.findAll('button').find((button) => button.attributes('title') === excerpt)
    expect(summaryButton).toBeDefined()
    await summaryButton!.trigger('click')

    expect(wrapper.text()).toContain('输入摘要')
    expect(wrapper.text()).not.toContain('输入开头与命中上下文')
  })

  it('labels non-keyword input detail as input excerpt', async () => {
    const excerpt = '普通输入摘要'
    listLogs.mockResolvedValue({
      items: [contentModerationLog({ input_excerpt: excerpt })],
      total: 1,
      page: 1,
      page_size: 20,
      pages: 1,
    })
    const wrapper = mount(RiskControlView, {
      global: {
        stubs: {
          AppLayout: AppLayoutStub,
          BaseDialog: BaseDialogStub,
          Icon: true,
          Select: true,
          Toggle: true,
          Pagination: true,
          ModelWhitelistSelector: ModelWhitelistSelectorStub,
        },
      },
    })

    await flushPromises()
    const summaryButton = wrapper.findAll('button').find((button) => button.attributes('title') === excerpt)
    expect(summaryButton).toBeDefined()
    await summaryButton!.trigger('click')

    expect(wrapper.text()).toContain('输入摘要')
    expect(wrapper.text()).not.toContain('输入开头与命中上下文')
  })
})
