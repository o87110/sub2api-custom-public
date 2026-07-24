import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { describe, expect, it } from 'vitest'

const componentPath = resolve(dirname(fileURLToPath(import.meta.url)), '../VersionBadge.vue')
const componentSource = readFileSync(componentPath, 'utf8')

describe('VersionBadge rollback release notes', () => {
  it('does not present a failed version check as an up-to-date result', () => {
    expect(componentSource).toContain('v-if="!hasUpdate && !versionCheckError"')
    expect(componentSource).toContain('<div v-if="displayError"')
    expect(componentSource).toContain("const versionCheckError = computed(() => updaterStore.versionError)")
  })

  it('renders a compact summary instead of the raw release body', () => {
    expect(componentSource).toContain('{{ summarizeReleaseNotes(item.body) }}')
    expect(componentSource).not.toContain('{{ item.body }}')
  })

  it('uses a dedicated two-line clamp that cannot be overridden by a block utility', () => {
    const styleMatch = componentSource.match(
      /\.rollback-release-notes\s*\{[\s\S]*?\n\}/
    )

    expect(styleMatch).not.toBeNull()
    expect(styleMatch?.[0]).toContain('display: -webkit-box;')
    expect(styleMatch?.[0]).toContain('-webkit-line-clamp: 2;')
    expect(componentSource).not.toContain('class="line-clamp-2 block')
  })

  it('links each rollback entry to its complete GitHub Release', () => {
    expect(componentSource).toContain(':href="item.html_url"')
    expect(componentSource).toContain('rel="noopener noreferrer"')
  })
})
