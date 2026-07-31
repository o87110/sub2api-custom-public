import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'

import GroupMinimumBalanceField from '../GroupMinimumBalanceField.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

describe('GroupMinimumBalanceField', () => {
  it('supports create, edit, clear, and reset values through v-model', async () => {
    const wrapper = mount(GroupMinimumBalanceField, { props: { modelValue: 0 } })
    const input = wrapper.get('[data-testid="group-minimum-balance"]')

    await input.setValue('12.5')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([12.5])

    await wrapper.setProps({ modelValue: 20 })
    expect((input.element as HTMLInputElement).value).toBe('20')

    await input.setValue('')
    expect(wrapper.emitted('update:modelValue')?.at(-1)).toEqual([''])

    await wrapper.setProps({ modelValue: 0 })
    expect((input.element as HTMLInputElement).value).toBe('0')
  })
})
