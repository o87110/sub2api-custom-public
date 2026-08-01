import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import type { Group } from '@/types'
import ApiKeyGroupPriorityEditor from '../ApiKeyGroupPriorityEditor.vue'

vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  return {
    ...actual,
    useI18n: () => ({
      locale: ref('zh-CN')
    })
  }
})

const createGroup = (
  id: number,
  overrides: Partial<Group> = {}
): Group => ({
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
  updated_at: '2026-07-25T00:00:00Z',
  ...overrides
})

const mountEditor = (
  props: Partial<InstanceType<typeof ApiKeyGroupPriorityEditor>['$props']> = {}
) => mount(ApiKeyGroupPriorityEditor, {
  props: {
    modelValue: [],
    groups: [createGroup(1), createGroup(2), createGroup(3)],
    ...props
  },
  global: {
    stubs: {
      GroupBadge: {
        name: 'GroupBadge',
        props: ['name'],
        template: '<span data-test="group-badge">{{ name }}</span>'
      },
      Icon: true
    }
  }
})

describe('ApiKeyGroupPriorityEditor', () => {
  it('adds and removes groups while emitting the complete ordered list', async () => {
    const wrapper = mountEditor()

    await wrapper.get('[data-test="group-priority-select"]').setValue('1')
    await wrapper.get('[data-test="group-priority-add"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([1])
    expect(wrapper.get('[data-test="group-priority-item-1"]').exists()).toBe(true)

    await wrapper.get('[data-test="group-priority-remove-1"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([])
    expect(wrapper.get('[data-test="group-priority-empty"]').exists()).toBe(true)
  })

  it('enforces the maximum of ten groups', () => {
    const groups = Array.from({ length: 11 }, (_, index) => createGroup(index + 1))
    const wrapper = mountEditor({
      groups,
      modelValue: groups.slice(0, 10).map((group) => group.id)
    })

    expect(wrapper.get('[data-test="group-priority-select"]').attributes('disabled'))
      .toBeDefined()
    expect(wrapper.get('[data-test="group-priority-add"]').attributes('disabled'))
      .toBeDefined()
    expect(wrapper.text()).toContain('10/10')
  })

  it('only offers eligible groups from the platform selected by the first item', () => {
    const blocked = createGroup(4, {
      minimum_balance: 100,
      current_balance: 80,
      usable_balance: 0,
      balance_gap: 20,
      balance_eligible: false
    })
    const wrapper = mountEditor({
      modelValue: [1],
      groups: [
        createGroup(1),
        createGroup(2, { platform: 'anthropic' }),
        createGroup(3),
        blocked
      ]
    })
    const optionValues = wrapper.findAll('option').map((option) => option.attributes('value'))

    expect(optionValues).toEqual(['', '3'])
  })

  it('allows a previously assigned group to be removed and re-added after its balance gate changes', async () => {
    const previouslyAssigned = createGroup(2, {
      minimum_balance: 100,
      current_balance: 80,
      usable_balance: 0,
      balance_gap: 20,
      balance_eligible: false
    })
    const wrapper = mountEditor({
      modelValue: [1, 2],
      selectedGroups: [createGroup(1), previouslyAssigned],
      groups: [createGroup(1), previouslyAssigned, createGroup(3)]
    })

    await wrapper.get('[data-test="group-priority-remove-2"]').trigger('click')

    expect(wrapper.findAll('option').map((option) => option.attributes('value')))
      .toContain('2')
  })

  it('restores focus to an adjacent row, then the selector when the list becomes empty', async () => {
    const wrapper = mountEditor({ modelValue: [1, 2] })
    document.body.appendChild(wrapper.element)

    await wrapper.get('[data-test="group-priority-remove-1"]').trigger('click')
    expect(document.activeElement).toBe(
      wrapper.get('[data-test="group-priority-item-2"]').element
    )

    await wrapper.get('[data-test="group-priority-remove-2"]').trigger('click')
    expect(document.activeElement).toBe(
      wrapper.get('[data-test="group-priority-select"]').element
    )
    wrapper.unmount()
  })

  it('supports button, keyboard and drag ordering', async () => {
    const buttonWrapper = mountEditor({ modelValue: [1, 2] })
    await buttonWrapper.get('[data-test="group-priority-move-down-1"]').trigger('click')
    expect(buttonWrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([2, 1])

    const keyboardWrapper = mountEditor({ modelValue: [1, 2] })
    await keyboardWrapper.get('[data-test="group-priority-item-1"]').trigger('keydown', {
      altKey: true,
      key: 'ArrowDown'
    })
    expect(keyboardWrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([2, 1])

    const dragWrapper = mountEditor({ modelValue: [1, 2] })
    await dragWrapper.get('[data-test="group-priority-drag-1"]').trigger('dragstart')
    await dragWrapper.get('[data-test="group-priority-item-2"]').trigger('drop')
    expect(dragWrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([2, 1])
  })

  it('does not reorder from keyboard or a stale drag while disabled or busy', async () => {
    const disabledWrapper = mountEditor({ modelValue: [1, 2], disabled: true })
    const disabledItem = disabledWrapper.get('[data-test="group-priority-item-1"]')
    expect(disabledItem.attributes('tabindex')).toBe('-1')
    expect(disabledItem.attributes('aria-disabled')).toBe('true')
    await disabledItem.trigger('keydown', {
      altKey: true,
      key: 'ArrowDown'
    })
    expect(disabledWrapper.emitted('update:modelValue')).toBeUndefined()

    const busyWrapper = mountEditor({ modelValue: [1, 2] })
    await busyWrapper.get('[data-test="group-priority-drag-1"]').trigger('dragstart')
    await busyWrapper.setProps({ busy: true })
    await busyWrapper.get('[data-test="group-priority-item-2"]').trigger('drop')
    expect(busyWrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('keeps edits local until save and restores the input value on cancel', async () => {
    const wrapper = mountEditor({
      modelValue: [1],
      showActions: true
    })

    await wrapper.get('[data-test="group-priority-select"]').setValue('2')
    await wrapper.get('[data-test="group-priority-add"]').trigger('click')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()

    await wrapper.get('[data-test="group-priority-save"]').trigger('click')
    expect(wrapper.emitted('save')?.at(-1)?.[0]).toEqual([1, 2])

    await wrapper.get('[data-test="group-priority-cancel"]').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(1)
    expect(wrapper.find('[data-test="group-priority-item-2"]').exists()).toBe(false)
  })

  it('provides accessible 44px ordering controls and field-level errors', () => {
    const wrapper = mountEditor({
      modelValue: [1, 2],
      error: '分组列表无效'
    })
    const controls = [
      wrapper.get('[data-test="group-priority-move-up-1"]'),
      wrapper.get('[data-test="group-priority-move-down-1"]'),
      wrapper.get('[data-test="group-priority-remove-1"]')
    ]

    for (const control of controls) {
      expect(control.classes()).toEqual(expect.arrayContaining(['h-11', 'w-11']))
      expect(control.attributes('aria-label')).toBeTruthy()
    }
    expect(wrapper.get('[role="alert"]').text()).toBe('分组列表无效')
    expect(wrapper.get('[data-test="group-priority-select"]').attributes('aria-invalid')).toBe('true')
    expect(wrapper.get('[data-test="group-priority-select"]').attributes('aria-describedby'))
      .toBe(wrapper.get('[role="alert"]').attributes('id'))
    expect(wrapper.get('[data-test="group-priority-item-1"]').attributes('tabindex')).toBe('0')
  })

  it('revalidates a pending option before adding it', async () => {
    const wrapper = mountEditor()
    await wrapper.get('[data-test="group-priority-select"]').setValue('2')
    await wrapper.setProps({
      groups: [
        createGroup(1),
        createGroup(2, {
          minimum_balance: 100,
          current_balance: 80,
          usable_balance: 0,
          balance_gap: 20,
          balance_eligible: false
        })
      ]
    })

    await wrapper.get('[data-test="group-priority-add"]').trigger('click')

    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(wrapper.find('[data-test="group-priority-item-2"]').exists()).toBe(false)
  })
})
