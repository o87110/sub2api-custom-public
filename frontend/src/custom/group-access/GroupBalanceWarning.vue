<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatGroupBalance, type GroupBalanceRequirement } from './minimumBalance'

type OpenMode = 'closed' | 'preview' | 'pinned'

const HOVER_CLOSE_DELAY = 120
const VIEWPORT_MARGIN = 8
const PANEL_GAP = 8

const props = defineProps<{
  requirement: GroupBalanceRequirement
}>()

const { locale } = useI18n()
const openMode = ref<OpenMode>('closed')
const triggerRef = ref<HTMLButtonElement | null>(null)
const panelRef = ref<HTMLElement | null>(null)
const panelStyle = ref({ top: '0px', left: '8px' })
const hoverCapable = ref(false)
const triggerHovered = ref(false)
const panelHovered = ref(false)
const hoverSuppressed = ref(false)
let hoverMediaQuery: MediaQueryList | null = null
let hoverCloseTimer: ReturnType<typeof setTimeout> | null = null

const zh = computed(() => locale.value.toLowerCase().startsWith('zh'))
const open = computed(() => openMode.value !== 'closed')
const text = computed(() => ({
  badge: zh.value ? '余额不足' : 'Insufficient balance',
  aria: zh.value ? '查看余额不足原因' : 'View insufficient balance details',
  title: zh.value ? '为什么该分组不可用？' : 'Why is this group unavailable?',
  current: zh.value ? '当前余额' : 'Current balance',
  minimum: zh.value ? '最低要求' : 'Minimum required',
  greaterThan: zh.value ? '高于' : 'Greater than',
  add: zh.value ? '还需增加' : 'Still needed',
  moreThan: zh.value ? '超过' : 'More than',
  equal:
    zh.value
      ? `当前余额刚好等于门槛，仍不可使用。余额增加至高于 ${formatGroupBalance(props.requirement.minimumBalance)} 后自动恢复。`
      : `The current balance is exactly the threshold, so the group remains unavailable. Access resumes automatically after the balance rises above ${formatGroupBalance(props.requirement.minimumBalance)}.`,
  recovery:
    zh.value
      ? '余额超过门槛后自动恢复，无需修改 API 密钥。'
      : 'Access resumes automatically after the balance exceeds the threshold; no API key change is required.'
}))

function updatePosition() {
  const trigger = triggerRef.value
  if (!trigger) return
  const triggerRect = trigger.getBoundingClientRect()
  const panelRect = panelRef.value?.getBoundingClientRect()
  const availableWidth = Math.max(0, window.innerWidth - VIEWPORT_MARGIN * 2)
  const panelWidth = panelRect?.width || Math.min(320, availableWidth)
  const panelHeight = panelRect?.height || (props.requirement.balanceGap > 0 ? 220 : 230)
  const maxLeft = Math.max(VIEWPORT_MARGIN, window.innerWidth - panelWidth - VIEWPORT_MARGIN)
  const left = Math.min(Math.max(VIEWPORT_MARGIN, triggerRect.right - panelWidth), maxLeft)
  const belowTop = triggerRect.bottom + PANEL_GAP
  const aboveTop = triggerRect.top - panelHeight - PANEL_GAP
  const showAbove = belowTop + panelHeight > window.innerHeight - VIEWPORT_MARGIN && aboveTop >= VIEWPORT_MARGIN
  const maxTop = Math.max(VIEWPORT_MARGIN, window.innerHeight - panelHeight - VIEWPORT_MARGIN)
  const top = Math.min(Math.max(VIEWPORT_MARGIN, showAbove ? aboveTop : belowTop), maxTop)
  panelStyle.value = {
    top: `${top}px`,
    left: `${left}px`
  }
}

function cancelHoverClose() {
  if (hoverCloseTimer === null) return
  clearTimeout(hoverCloseTimer)
  hoverCloseTimer = null
}

function openPreview() {
  if (!hoverCapable.value || hoverSuppressed.value || openMode.value === 'pinned') return
  cancelHoverClose()
  openMode.value = 'preview'
  nextTick(updatePosition)
}

function schedulePreviewClose() {
  if (openMode.value !== 'preview') return
  cancelHoverClose()
  hoverCloseTimer = setTimeout(() => {
    hoverCloseTimer = null
    if (!triggerHovered.value && !panelHovered.value && openMode.value === 'preview') {
      openMode.value = 'closed'
    }
  }, HOVER_CLOSE_DELAY)
}

function resetHoverSuppressionIfOutside() {
  if (!triggerHovered.value && !panelHovered.value) hoverSuppressed.value = false
}

function onTriggerEnter() {
  triggerHovered.value = true
  cancelHoverClose()
  openPreview()
}

function onTriggerLeave() {
  triggerHovered.value = false
  resetHoverSuppressionIfOutside()
  schedulePreviewClose()
}

function onPanelEnter() {
  panelHovered.value = true
  cancelHoverClose()
}

function onPanelLeave() {
  panelHovered.value = false
  resetHoverSuppressionIfOutside()
  schedulePreviewClose()
}

function close(options: { suppressHover?: boolean; restoreFocus?: boolean } = {}) {
  if (!open.value) return
  cancelHoverClose()
  const suppressUntilTriggerLeave = Boolean(
    options.suppressHover && hoverCapable.value && triggerHovered.value
  )
  openMode.value = 'closed'
  panelHovered.value = false
  hoverSuppressed.value = suppressUntilTriggerLeave
  if (options.restoreFocus) nextTick(() => triggerRef.value?.focus())
}

function togglePinned(event: Event) {
  event.stopPropagation()
  cancelHoverClose()
  if (openMode.value === 'pinned') {
    close({ suppressHover: true })
    return
  }
  hoverSuppressed.value = false
  openMode.value = 'pinned'
  nextTick(updatePosition)
}

function onDocumentPointer(event: Event) {
  if (!open.value) return
  const target = event.target as Node | null
  if (target && (triggerRef.value?.contains(target) || panelRef.value?.contains(target))) return
  close({ suppressHover: true })
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape' && open.value) close({ suppressHover: true, restoreFocus: true })
}

function onViewportChange() {
  if (open.value) updatePosition()
}

function onHoverCapabilityChange(event: MediaQueryListEvent) {
  hoverCapable.value = event.matches
  if (!event.matches && openMode.value === 'preview') close()
}

onMounted(() => {
  if (typeof window.matchMedia === 'function') {
    hoverMediaQuery = window.matchMedia('(hover: hover) and (pointer: fine)')
    hoverCapable.value = hoverMediaQuery.matches
    hoverMediaQuery.addEventListener('change', onHoverCapabilityChange)
  }
  document.addEventListener('pointerdown', onDocumentPointer, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
  cancelHoverClose()
  hoverMediaQuery?.removeEventListener('change', onHoverCapabilityChange)
  document.removeEventListener('pointerdown', onDocumentPointer, true)
  document.removeEventListener('keydown', onDocumentKeydown)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('scroll', onViewportChange, true)
})
</script>

<template>
  <span class="inline-flex flex-shrink-0 items-center gap-1" data-test="group-balance-warning">
    <span class="rounded-full bg-amber-100 px-1.5 py-0.5 text-[11px] font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300">
      {{ text.badge }}
    </span>
    <button
      ref="triggerRef"
      type="button"
      class="group/help inline-flex h-11 w-11 cursor-pointer items-center justify-center rounded-full focus:outline-none md:h-5 md:w-5"
      :aria-label="text.aria"
      :aria-expanded="open"
      aria-haspopup="dialog"
      data-test="group-balance-warning-help"
      @mousedown.stop
      @click="togglePinned"
      @pointerenter="onTriggerEnter"
      @pointerleave="onTriggerLeave"
    >
      <span class="inline-flex h-5 w-5 items-center justify-center rounded-full border border-amber-300 text-xs font-bold text-amber-700 transition-colors group-hover/help:bg-amber-100 group-focus-visible/help:ring-2 group-focus-visible/help:ring-amber-500/50 dark:border-amber-700 dark:text-amber-300 dark:group-hover/help:bg-amber-900/40">
        ?
      </span>
    </button>
    <Teleport to="body">
      <section
        v-if="open"
        ref="panelRef"
        role="dialog"
        :aria-label="text.title"
        class="fixed z-[100000030] max-h-[calc(100vh-1rem)] w-[min(20rem,calc(100vw-1rem))] overflow-y-auto rounded-xl border border-amber-200 bg-white p-4 text-left shadow-xl dark:border-amber-800 dark:bg-dark-800"
        :style="panelStyle"
        data-test="group-balance-warning-panel"
        @click.stop
        @mousedown.stop
        @pointerenter="onPanelEnter"
        @pointerleave="onPanelLeave"
      >
        <div class="mb-3 flex items-start justify-between gap-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ text.title }}</h3>
          <button
            type="button"
            class="-mr-2 -mt-2 inline-flex h-11 w-11 flex-shrink-0 cursor-pointer items-center justify-center rounded text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-700 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-500/50 dark:hover:bg-dark-700 dark:hover:text-gray-200"
            :aria-label="zh ? '关闭' : 'Close'"
            @click="close({ suppressHover: true })"
          >
            ×
          </button>
        </div>
        <dl class="space-y-2 text-sm">
          <div class="flex items-center justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ text.current }}</dt>
            <dd class="font-medium text-gray-900 dark:text-white">
              {{ formatGroupBalance(requirement.currentBalance) }}
            </dd>
          </div>
          <div class="flex items-center justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ text.minimum }}</dt>
            <dd class="font-medium text-gray-900 dark:text-white">
              {{ text.greaterThan }} {{ formatGroupBalance(requirement.minimumBalance) }}
            </dd>
          </div>
          <div v-if="requirement.balanceGap > 0" class="flex items-center justify-between gap-3">
            <dt class="text-gray-500 dark:text-gray-400">{{ text.add }}</dt>
            <dd class="font-semibold text-amber-700 dark:text-amber-300">
              {{ text.moreThan }} {{ formatGroupBalance(requirement.balanceGap) }}
            </dd>
          </div>
        </dl>
        <p class="mt-3 border-t border-gray-100 pt-3 text-xs leading-relaxed text-gray-600 dark:border-dark-700 dark:text-gray-300">
          {{ requirement.balanceGap > 0 ? text.recovery : text.equal }}
        </p>
      </section>
    </Teleport>
  </span>
</template>
