import { describe, expect, it, vi } from 'vitest'
import { runApiKeyBulkAction } from '../bulkActions'

describe('runApiKeyBulkAction', () => {
  it('limits concurrency and preserves input order in the result', async () => {
    let active = 0
    let maxActive = 0

    const result = await runApiKeyBulkAction(
      [1, 2, 3, 4, 5, 6],
      async (id) => {
        active += 1
        maxActive = Math.max(maxActive, active)
        await Promise.resolve()
        active -= 1
        if (id === 2 || id === 5) {
          throw new Error('expected failure')
        }
      },
      2
    )

    expect(maxActive).toBeLessThanOrEqual(2)
    expect(result).toEqual({
      succeededIds: [1, 3, 4, 6],
      failedIds: [2, 5]
    })
  })

  it('deduplicates ids and uses the default five workers', async () => {
    let active = 0
    let maxActive = 0
    const operation = vi.fn(async () => {
      active += 1
      maxActive = Math.max(maxActive, active)
      await new Promise<void>((resolve) => setTimeout(resolve, 0))
      active -= 1
    })

    const result = await runApiKeyBulkAction([1, 1, 2, 3, 4, 5, 6], operation)

    expect(operation).toHaveBeenCalledTimes(6)
    expect(maxActive).toBeLessThanOrEqual(5)
    expect(result.failedIds).toEqual([])
    expect(result.succeededIds).toEqual([1, 2, 3, 4, 5, 6])
  })

  it('enforces five as the hard concurrency limit', async () => {
    let active = 0
    let maxActive = 0
    const operation = vi.fn(async () => {
      active += 1
      maxActive = Math.max(maxActive, active)
      await new Promise<void>((resolve) => setTimeout(resolve, 0))
      active -= 1
    })

    await runApiKeyBulkAction([1, 2, 3, 4, 5, 6, 7, 8], operation, 10)

    expect(maxActive).toBeLessThanOrEqual(5)
  })

  it('retains every id when all operations fail', async () => {
    const result = await runApiKeyBulkAction(
      [1, 2, 3],
      async () => Promise.reject(new Error('expected failure'))
    )

    expect(result).toEqual({
      succeededIds: [],
      failedIds: [1, 2, 3]
    })
  })

  it('returns immediately for an empty selection', async () => {
    const operation = vi.fn()

    await expect(runApiKeyBulkAction([], operation)).resolves.toEqual({
      succeededIds: [],
      failedIds: []
    })
    expect(operation).not.toHaveBeenCalled()
  })
})
