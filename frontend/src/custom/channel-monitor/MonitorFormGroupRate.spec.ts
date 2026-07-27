import { defineComponent } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { ChannelMonitor } from '@/api/admin/channelMonitor'
import MonitorFormDialog from '@/components/admin/monitor/MonitorFormDialog.vue'

const {
  createMonitor,
  updateMonitor,
  listTemplates,
  showError,
} = vi.hoisted(() => ({
  createMonitor: vi.fn(),
  updateMonitor: vi.fn(),
  listTemplates: vi.fn(),
  showError: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    channelMonitor: {
      create: createMonitor,
      update: updateMonitor,
    },
    channelMonitorTemplate: {
      list: listTemplates,
    },
  },
}))

vi.mock('@/api/keys', () => ({
  keysAPI: { list: vi.fn() },
}))

vi.mock('@/api/groups', () => ({
  userGroupsAPI: { getUserGroupRates: vi.fn() },
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    cachedPublicSettings: null,
    showError,
    showSuccess: vi.fn(),
  }),
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const BaseDialogStub = defineComponent({
  props: { show: { type: Boolean, default: false } },
  template: '<div v-if="show"><slot /><slot name="footer" /></div>',
})

function makeMonitor(overrides: Partial<ChannelMonitor> = {}): ChannelMonitor {
  return {
    id: 7,
    name: 'monitor',
    provider: 'openai',
    api_mode: 'chat_completions',
    endpoint: 'https://api.example.com',
    api_key_masked: 'sk-***',
    primary_model: 'gpt-test',
    extra_models: [],
    group_name: '特价分组',
    group_rate_override: 0.9,
    group_rate_display_template: '{rate}优先用',
    enabled: true,
    interval_seconds: 60,
    jitter_seconds: 0,
    last_checked_at: null,
    created_by: 1,
    created_at: '2026-07-28T00:00:00Z',
    updated_at: '2026-07-28T00:00:00Z',
    primary_status: '',
    primary_latency_ms: null,
    availability_7d: 0,
    extra_models_status: [],
    template_id: null,
    extra_headers: {},
    body_override_mode: 'off',
    body_override: null,
    ...overrides,
  }
}

function mountDialog(monitor: ChannelMonitor | null) {
  return mount(MonitorFormDialog, {
    props: { show: true, monitor },
    global: {
      stubs: {
        BaseDialog: BaseDialogStub,
        Toggle: true,
        Select: true,
        ModelTagInput: true,
        MonitorKeyPickerDialog: true,
        MonitorAdvancedRequestConfig: true,
      },
    },
  })
}

async function fillRequiredCreateFields(wrapper: ReturnType<typeof mountDialog>) {
  await wrapper.get('[data-testid="monitor-name"]').setValue('new monitor')
  await wrapper.get('[data-testid="monitor-endpoint"]').setValue('https://api.example.com')
  await wrapper.get('[data-testid="monitor-api-key"]').setValue('sk-test')
  await wrapper.get('[data-testid="monitor-primary-model"]').setValue('claude-test')
}

describe('MonitorFormDialog group rate display config', () => {
  beforeEach(() => {
    createMonitor.mockReset().mockResolvedValue(makeMonitor())
    updateMonitor.mockReset().mockResolvedValue(makeMonitor())
    listTemplates.mockReset().mockResolvedValue({ items: [] })
    showError.mockReset()
  })

  it('creates a monitor with an independent group name, override, and template', async () => {
    const wrapper = mountDialog(null)
    await flushPromises()
    await fillRequiredCreateFields(wrapper)
    await wrapper.get('[data-testid="monitor-group-name"]').setValue('内部标签')
    await wrapper.get('[data-testid="monitor-group-rate-override"]').setValue('0.9')
    await wrapper.get('[data-testid="monitor-group-rate-template"]').setValue('约{rate}x')

    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(createMonitor).toHaveBeenCalledWith(expect.objectContaining({
      group_name: '内部标签',
      group_rate_override: 0.9,
      group_rate_display_template: '约{rate}x',
    }))
  })

  it('loads existing values and explicitly clears the override back to automatic resolution', async () => {
    const wrapper = mountDialog(makeMonitor())
    await flushPromises()

    expect((wrapper.get('[data-testid="monitor-group-name"]').element as HTMLInputElement).value).toBe('特价分组')
    expect((wrapper.get('[data-testid="monitor-group-rate-override"]').element as HTMLInputElement).value).toBe('0.9')
    expect((wrapper.get('[data-testid="monitor-group-rate-template"]').element as HTMLInputElement).value).toBe('{rate}优先用')

    await wrapper.get('[data-testid="monitor-group-rate-override"]').setValue('')
    await wrapper.get('[data-testid="monitor-group-rate-template"]').setValue('{rate}')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(updateMonitor).toHaveBeenCalledWith(7, expect.objectContaining({
      group_name: '特价分组',
      clear_group_rate_override: true,
      group_rate_display_template: '{rate}',
    }))
    expect(updateMonitor.mock.calls[0][1]).not.toHaveProperty('group_rate_override')
  })

  it('rejects non-positive overrides and invalid templates before submitting', async () => {
    const wrapper = mountDialog(null)
    await flushPromises()
    await fillRequiredCreateFields(wrapper)

    await wrapper.get('[data-testid="monitor-group-rate-override"]').setValue('-1')
    await wrapper.get('form').trigger('submit')
    expect(showError).toHaveBeenLastCalledWith('admin.channelMonitor.groupRateOverrideInvalid')
    expect(createMonitor).not.toHaveBeenCalled()

    await wrapper.get('[data-testid="monitor-group-rate-override"]').setValue('0.9')
    await wrapper.get('[data-testid="monitor-group-rate-template"]').setValue('missing placeholder')
    await wrapper.get('form').trigger('submit')
    expect(showError).toHaveBeenLastCalledWith('admin.channelMonitor.groupRateDisplayTemplateInvalid')
    expect(createMonitor).not.toHaveBeenCalled()
  })
})
