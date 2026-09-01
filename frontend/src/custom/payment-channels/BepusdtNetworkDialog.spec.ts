import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import BepusdtNetworkDialog from './BepusdtNetworkDialog.vue'

vi.mock('vue-i18n', () => ({
  useI18n: () => ({ t: (key: string) => key }),
}))

const networks = [
  { code: 'trc20', display_name: 'TRC20' },
  { code: 'bep20', display_name: 'BEP20' },
  { code: 'polygon', display_name: 'Polygon' },
  { code: 'plasma', display_name: 'Plasma' },
]

describe('BepusdtNetworkDialog', () => {
  it('defaults to BEP20 and emits only after confirmation', async () => {
    const wrapper = mount(BepusdtNetworkDialog, {
      props: { show: true, networks },
      global: { stubs: { Teleport: true, Transition: true } },
    })
    const select = wrapper.get('select')
    expect((select.element as HTMLSelectElement).value).toBe('bep20')
    expect(wrapper.emitted('confirm')).toBeUndefined()
    await wrapper.get('button.btn-primary').trigger('click')
    expect(wrapper.emitted('confirm')?.[0]).toEqual(['bep20'])
  })

  it('falls back to the first configured network when BEP20 is absent', () => {
    const wrapper = mount(BepusdtNetworkDialog, {
      props: { show: true, networks: [networks[0], networks[2]] },
      global: { stubs: { Teleport: true, Transition: true } },
    })
    expect((wrapper.get('select').element as HTMLSelectElement).value).toBe('trc20')
  })
})
