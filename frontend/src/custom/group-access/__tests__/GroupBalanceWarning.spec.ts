import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount, type VueWrapper } from '@vue/test-utils'
import { nextTick } from 'vue'
import GroupBalanceWarning from '../GroupBalanceWarning.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ locale: { value: 'zh-CN' } })
}))

let wrapper: VueWrapper | null = null
const originalMatchMedia = window.matchMedia

const requirement = {
  groupName: '分组 A',
  minimumBalance: 100,
  currentBalance: 80,
  usableBalance: 0,
  balanceGap: 20,
  eligible: false
}

function mockHoverCapability(matches: boolean) {
  const listeners = new Set<(event: MediaQueryListEvent) => void>()
  const mediaQuery = {
    matches,
    media: '(hover: hover) and (pointer: fine)',
    onchange: null,
    addEventListener: vi.fn((type: string, listener: (event: MediaQueryListEvent) => void) => {
      if (type === 'change') listeners.add(listener)
    }),
    removeEventListener: vi.fn((type: string, listener: (event: MediaQueryListEvent) => void) => {
      if (type === 'change') listeners.delete(listener)
    }),
    addListener: vi.fn(),
    removeListener: vi.fn(),
    dispatchEvent: vi.fn()
  } as unknown as MediaQueryList

  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: vi.fn(() => mediaQuery)
  })
}

function mountWarning(overrides: Partial<typeof requirement> = {}) {
  wrapper = mount(GroupBalanceWarning, {
    props: {
      requirement: { ...requirement, ...overrides }
    },
    attachTo: document.body
  })
  return wrapper
}

function warningPanel() {
  return document.body.querySelector<HTMLElement>('[data-test="group-balance-warning-panel"]')
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
  vi.useRealTimers()
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: originalMatchMedia
  })
})

describe('GroupBalanceWarning', () => {
  it('opens from the question button and closes with Escape', async () => {
    mockHoverCapability(false)
    const currentWrapper = mountWarning()

    expect(currentWrapper.get('[data-test="group-balance-warning"]').text()).toContain('余额不足')
    expect(warningPanel()).toBeNull()

    const trigger = currentWrapper.get('[data-test="group-balance-warning-help"]')
    await trigger.trigger('click')
    await nextTick()

    const panel = warningPanel()
    expect(panel?.textContent).toContain('当前余额')
    expect(panel?.textContent).toContain('$80.00')
    expect(panel?.textContent).toContain('超过 $20.00')
    expect(trigger.attributes('aria-expanded')).toBe('true')

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await nextTick()
    expect(warningPanel()).toBeNull()
    expect(document.activeElement).toBe(trigger.element)
  })

  it('previews on fine-pointer hover and allows moving into the panel before closing', async () => {
    vi.useFakeTimers()
    mockHoverCapability(true)
    const currentWrapper = mountWarning()
    const trigger = currentWrapper.get('[data-test="group-balance-warning-help"]')

    await trigger.trigger('pointerenter')
    await nextTick()
    expect(warningPanel()).not.toBeNull()

    await trigger.trigger('pointerleave')
    warningPanel()?.dispatchEvent(new Event('pointerenter'))
    vi.advanceTimersByTime(120)
    await nextTick()
    expect(warningPanel()).not.toBeNull()

    warningPanel()?.dispatchEvent(new Event('pointerleave'))
    vi.advanceTimersByTime(119)
    await nextTick()
    expect(warningPanel()).not.toBeNull()

    vi.advanceTimersByTime(1)
    await nextTick()
    expect(warningPanel()).toBeNull()
  })

  it('pins a hover preview on click and closes it on the next click', async () => {
    vi.useFakeTimers()
    mockHoverCapability(true)
    const currentWrapper = mountWarning()
    const trigger = currentWrapper.get('[data-test="group-balance-warning-help"]')

    await trigger.trigger('pointerenter')
    await trigger.trigger('click')
    await trigger.trigger('pointerleave')
    vi.advanceTimersByTime(500)
    await nextTick()
    expect(warningPanel()).not.toBeNull()

    await trigger.trigger('pointerenter')
    await trigger.trigger('click')
    await nextTick()
    expect(warningPanel()).toBeNull()

    await trigger.trigger('pointerenter')
    await nextTick()
    expect(warningPanel()).toBeNull()

    await trigger.trigger('pointerleave')
    await trigger.trigger('pointerenter')
    await nextTick()
    expect(warningPanel()).not.toBeNull()
  })

  it('does not open from hover on touch-style pointers but still opens from click', async () => {
    mockHoverCapability(false)
    const currentWrapper = mountWarning()
    const trigger = currentWrapper.get('[data-test="group-balance-warning-help"]')

    await trigger.trigger('pointerenter')
    await nextTick()
    expect(warningPanel()).toBeNull()

    await trigger.trigger('click')
    await nextTick()
    expect(warningPanel()).not.toBeNull()
    expect(trigger.classes()).toEqual(expect.arrayContaining(['h-11', 'w-11', 'md:h-5', 'md:w-5']))
  })

  it('closes a pinned panel from its close button or an outside pointer event', async () => {
    mockHoverCapability(false)
    const currentWrapper = mountWarning()
    const trigger = currentWrapper.get('[data-test="group-balance-warning-help"]')

    await trigger.trigger('click')
    await nextTick()
    warningPanel()?.querySelector<HTMLButtonElement>('button[aria-label="关闭"]')?.click()
    await nextTick()
    expect(warningPanel()).toBeNull()

    await trigger.trigger('click')
    await nextTick()
    document.body.dispatchEvent(new Event('pointerdown', { bubbles: true }))
    await nextTick()
    expect(warningPanel()).toBeNull()
  })

  it('can preview again after explicitly closing while the pointer is inside the panel', async () => {
    vi.useFakeTimers()
    mockHoverCapability(true)
    const currentWrapper = mountWarning()
    const trigger = currentWrapper.get('[data-test="group-balance-warning-help"]')

    await trigger.trigger('pointerenter')
    await trigger.trigger('pointerleave')
    warningPanel()?.dispatchEvent(new Event('pointerenter'))
    warningPanel()?.querySelector<HTMLButtonElement>('button[aria-label="关闭"]')?.click()
    await nextTick()
    expect(warningPanel()).toBeNull()

    await trigger.trigger('pointerenter')
    await nextTick()
    expect(warningPanel()).not.toBeNull()
  })

  it('does not render a misleading zero gap for an equal balance', async () => {
    mockHoverCapability(false)
    const currentWrapper = mountWarning({ currentBalance: 100, balanceGap: 0 })

    await currentWrapper.get('[data-test="group-balance-warning-help"]').trigger('click')
    await nextTick()

    const text = warningPanel()?.textContent
    expect(text).toContain('当前余额刚好等于门槛')
    expect(text).not.toContain('还需增加')
  })
})
