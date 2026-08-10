export type MatrixWheelZoomEvent = Pick<WheelEvent, 'altKey' | 'target'>

/**
 * Resolve the pulse track for an intentional matrix zoom gesture.
 * Plain wheel events must remain native so the matrix or page can scroll.
 */
export function resolveMatrixWheelZoomTrack(
  event: MatrixWheelZoomEvent,
): HTMLElement | null {
  if (!event.altKey || typeof Element === 'undefined' || !(event.target instanceof Element)) {
    return null
  }
  return event.target.closest<HTMLElement>('.pulse-track')
}

export function matrixWheelZoomHint(locale?: string): string {
  if (locale?.trim().toLowerCase().startsWith('zh')) {
    return '按住 Alt/Option，在色块上滚轮缩放（区间变窄、色块变宽）'
  }
  return 'Hold Alt/Option and scroll over a pulse block to zoom (narrower range, wider blocks)'
}
