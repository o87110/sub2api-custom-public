import { describe, expect, it } from 'vitest'
import { apiKeyGroupFieldError } from '../fieldError'

describe('apiKeyGroupFieldError', () => {
  it('reads legacy nested field errors', () => {
    expect(apiKeyGroupFieldError({
      response: {
        data: {
          fields: {
            group_ids: ['分组列表无效']
          }
        }
      }
    })).toBe('分组列表无效')
  })

  it('reads flattened API client errors with field metadata', () => {
    expect(apiKeyGroupFieldError({
      message: 'group_ids must use one platform',
      metadata: {
        field: 'group_ids'
      }
    })).toBe('group_ids must use one platform')
  })

  it('supports the legacy group_id field', () => {
    expect(apiKeyGroupFieldError({
      message: 'group is unavailable',
      metadata: {
        field: 'group_id'
      }
    }, ['group_id'])).toBe('group is unavailable')
  })
})
