const CUSTOM_RELEASE_METADATA_PATTERNS = [
  /^>?\s*AI API Gateway Platform\b/i,
  /^Based on official\b/i,
  /^Source commit:\s*/i
]

const RELEASE_DETAILS_SECTION_PATTERN =
  /^#{1,6}\s*(?:📥|📚)?\s*(?:installation|documentation)\b/i

function stripInlineMarkdown(value: string): string {
  return value
    .replace(/^#{1,6}\s*/, '')
    .replace(/^[-*+>]\s*/, '')
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/__([^_]+)__/g, '$1')
    .trim()
}

/**
 * Build a compact, plain-text rollback summary from a GitHub Release body.
 *
 * Custom release metadata and installation/documentation boilerplate are
 * intentionally omitted because the rollback card already shows the version
 * and links to the complete release page.
 */
export function summarizeReleaseNotes(body: string, maxLength = 180): string {
  if (!body?.trim()) return ''

  const summaryLines: string[] = []
  let insideCodeFence = false

  for (const rawLine of body.split(/\r?\n/)) {
    const line = rawLine.trim()

    if (line.startsWith('```')) {
      insideCodeFence = !insideCodeFence
      continue
    }
    if (insideCodeFence || !line || /^-{3,}$/.test(line)) continue
    if (RELEASE_DETAILS_SECTION_PATTERN.test(line)) break
    if (CUSTOM_RELEASE_METADATA_PATTERNS.some((pattern) => pattern.test(line))) continue

    const plainText = stripInlineMarkdown(line)
    if (plainText) summaryLines.push(plainText)
  }

  const summary = summaryLines.join(' ').replace(/\s+/g, ' ').trim()
  if (!summary) return ''

  const limit = Math.max(2, maxLength)
  if (summary.length <= limit) return summary
  return `${summary.slice(0, limit - 1).trimEnd()}…`
}
