import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, ref } from 'vue'
import UserBanThresholdOverrides from '@/custom/moderation/UserBanThresholdOverrides.vue'

const { searchUsers, getUserByID } = vi.hoisted(() => ({
  searchUsers: vi.fn(),
  getUserByID: vi.fn(),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { searchUsers },
    users: { getById: getUserByID },
  },
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      locale: { value: 'zh-CN' },
      t: (key: string) => key,
    }),
  }
})

const normalUser = {
  id: 11,
  email: 'user@example.com',
  role: 'user',
  status: 'active',
  deleted_at: null,
}

function mountComponent(modelValue = [{ user_id: 11, ban_threshold: 20 }]) {
  return mount(UserBanThresholdOverrides, {
    props: { modelValue, defaultThreshold: 10 },
    global: { stubs: { Icon: true } },
  })
}

function mountControlledComponent(modelValue: Array<{ user_id: number; ban_threshold: number }> = []) {
  const Harness = defineComponent({
    components: { UserBanThresholdOverrides },
    setup() {
      const overrides = ref(modelValue)
      return { overrides }
    },
    template: '<UserBanThresholdOverrides v-model="overrides" :default-threshold="10" />',
  })
  return mount(Harness, {
    attachTo: document.body,
    global: { stubs: { Icon: true } },
  })
}

describe('UserBanThresholdOverrides', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    searchUsers.mockReset()
    getUserByID.mockReset()
    getUserByID.mockResolvedValue(normalUser)
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('hydrates an existing user and emits edited thresholds with inline validation', async () => {
    const wrapper = mountComponent()
    await flushPromises()

    expect(wrapper.text()).toContain('user@example.com')
    const threshold = wrapper.get<HTMLInputElement>('input[type="number"]')
    await threshold.setValue('0')

    const emitted = wrapper.emitted('update:modelValue')?.at(-1)?.[0]
    expect(emitted).toEqual([
      { user_id: 11, ban_threshold: 0 },
    ])
    await wrapper.setProps({ modelValue: emitted })
    expect(wrapper.text()).toContain('请输入 1–1000 的整数。')
  })

  it('searches and adds a normal user with the current default threshold', async () => {
    const addedUser = { ...normalUser, id: 12, email: 'added@example.com' }
    searchUsers.mockResolvedValue([{ id: 12, email: addedUser.email, deleted: false }])
    getUserByID.mockResolvedValue(addedUser)
    const wrapper = mountControlledComponent()
    const component = wrapper.getComponent(UserBanThresholdOverrides)

    await component.get('input[type="search"]').setValue('added')
    vi.advanceTimersByTime(300)
    await flushPromises()
    await component.get('button').trigger('click')
    await flushPromises()

    expect(component.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([
      { user_id: 12, ban_threshold: 10 },
    ])
    expect(document.activeElement).toBe(component.get('input[type="number"]').element)
    wrapper.unmount()
  })

  it('prevents overlapping user selections from losing an override', async () => {
    searchUsers.mockResolvedValue([
      { id: 12, email: 'first@example.com', deleted: false },
      { id: 13, email: 'second@example.com', deleted: false },
    ])
    let resolveUser!: (value: typeof normalUser) => void
    getUserByID.mockImplementation(() => new Promise((resolve) => { resolveUser = resolve }))
    const wrapper = mountComponent([])

    await wrapper.get('input[type="search"]').setValue('example')
    vi.advanceTimersByTime(300)
    await flushPromises()
    const options = wrapper.findAll('button')
    await options[0].trigger('click')
    expect(options[0].attributes('disabled')).toBeDefined()
    expect(options[1].attributes('disabled')).toBeDefined()
    await options[1].trigger('click')
    expect(getUserByID).toHaveBeenCalledTimes(1)

    resolveUser({ ...normalUser, id: 12, email: 'first@example.com' })
    await flushPromises()
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([
      { user_id: 12, ban_threshold: 10 },
    ])
  })

  it('filters duplicate search results and closes the result list with Escape', async () => {
    searchUsers.mockResolvedValue([
      { id: 11, email: 'user@example.com', deleted: false },
      { id: 12, email: 'added@example.com', deleted: false },
    ])
    const wrapper = mountComponent()
    await flushPromises()

    const search = wrapper.get('input[type="search"]')
    await search.setValue('example')
    vi.advanceTimersByTime(300)
    await flushPromises()
    const options = wrapper.findAll('button').filter((button) => !button.attributes('title'))
    expect(options).toHaveLength(1)
    expect(options[0].text()).toContain('#12')

    await search.trigger('keydown', { key: 'Escape' })
    expect(wrapper.findAll('button').filter((button) => !button.attributes('title'))).toHaveLength(0)
  })

  it('rejects a user deleted after search and keeps responsive row layout', async () => {
    searchUsers.mockResolvedValue([{ id: 12, email: 'deleted@example.com', deleted: false }])
    getUserByID.mockResolvedValue({
      ...normalUser,
      id: 12,
      email: 'deleted@example.com',
      deleted_at: '2026-08-01T00:00:00Z',
    })
    const wrapper = mountComponent()
    await flushPromises()
    expect(wrapper.get('div.grid').classes()).toContain('sm:grid-cols-[minmax(0,1fr)_11rem_2.75rem]')

    await wrapper.get('input[type="search"]').setValue('deleted')
    vi.advanceTimersByTime(300)
    await flushPromises()
    const option = wrapper.findAll('button').find((button) => button.text().includes('#12'))
    await option!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('不能为已删除用户新增专属阈值。')
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
  })

  it('rejects administrators and keeps missing configured users removable', async () => {
    getUserByID.mockRejectedValueOnce(new Error('missing'))
    const wrapper = mountComponent([{ user_id: 99, ban_threshold: 30 }])
    await flushPromises()
    expect(wrapper.text()).toContain('用户 #99')
    expect(wrapper.text()).toContain('用户不可用')

    const removeButton = wrapper.findAll('button').find((button) => button.attributes('title') === '移除用户专属阈值')
    expect(removeButton).toBeDefined()
    await removeButton!.trigger('click')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([])

    searchUsers.mockResolvedValue([{ id: 1, email: 'admin@example.com', deleted: false }])
    getUserByID.mockResolvedValue({ ...normalUser, id: 1, email: 'admin@example.com', role: 'admin' })
    await wrapper.setProps({ modelValue: [] })
    await wrapper.get('input[type="search"]').setValue('admin')
    vi.advanceTimersByTime(300)
    await flushPromises()
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('管理员账户不会被自动封禁，不能添加专属阈值。')
    expect(wrapper.emitted('update:modelValue')?.at(-1)?.[0]).toEqual([])
  })
})
