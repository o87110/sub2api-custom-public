export type ApiKeyBulkAction = 'group' | 'disable' | 'delete'

export interface ApiKeyBulkExecutionResult {
  succeededIds: number[]
  failedIds: number[]
}

export interface ApiKeyBulkCompletedResult extends ApiKeyBulkExecutionResult {
  action: ApiKeyBulkAction
  skippedIds: number[]
}

const MAX_CONCURRENCY = 5

export async function runApiKeyBulkAction(
  ids: number[],
  operation: (id: number) => Promise<unknown>,
  concurrency = MAX_CONCURRENCY
): Promise<ApiKeyBulkExecutionResult> {
  const uniqueIds = [...new Set(ids)]
  if (uniqueIds.length === 0) {
    return { succeededIds: [], failedIds: [] }
  }

  const workerCount = Math.min(
    uniqueIds.length,
    MAX_CONCURRENCY,
    Math.max(1, Math.floor(Number.isFinite(concurrency) ? concurrency : 1))
  )
  const succeeded = new Array<boolean>(uniqueIds.length).fill(false)
  let nextIndex = 0

  const worker = async () => {
    while (nextIndex < uniqueIds.length) {
      const index = nextIndex
      nextIndex += 1
      try {
        await operation(uniqueIds[index])
        succeeded[index] = true
      } catch {
        succeeded[index] = false
      }
    }
  }

  await Promise.all(Array.from({ length: workerCount }, () => worker()))

  return uniqueIds.reduce<ApiKeyBulkExecutionResult>(
    (result, id, index) => {
      if (succeeded[index]) {
        result.succeededIds.push(id)
      } else {
        result.failedIds.push(id)
      }
      return result
    },
    { succeededIds: [], failedIds: [] }
  )
}
