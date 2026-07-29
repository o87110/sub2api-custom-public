<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatGroupBalance, type GroupBalanceRequirement } from './minimumBalance'

const props = defineProps<{
  requirement: GroupBalanceRequirement
}>()

const { locale } = useI18n()
const open = ref(false)
const triggerRef = ref<HTMLButtonElement | null>(null)
const panelRef = ref<HTMLElement | null>(null)
const panelStyle = ref({ top: '0px', left: '8px' })

const zh = computed(() => locale.value.toLowerCase().startsWith('zh'))
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
  const rect = trigger.getBoundingClientRect()
  const panelWidth = Math.min(320, window.innerWidth - 16)
  const left = Math.min(
    Math.max(8, rect.right - panelWidth),
    Math.max(8, window.innerWidth - panelWidth - 8)
  )
  const estimatedHeight = props.requirement.balanceGap > 0 ? 220 : 230
  const showAbove = rect.bottom + estimatedHeight > window.innerHeight && rect.top > estimatedHeight
  panelStyle.value = {
    top: `${showAbove ? Math.max(8, rect.top - estimatedHeight - 8) : rect.bottom + 8}px`,
    left: `${left}px`
  }
}

function toggle(event: Event) {
  event.stopPropagation()
  open.value = !open.value
  if (open.value) nextTick(updatePosition)
}

function close() {
  open.value = false
}

function onDocumentPointer(event: Event) {
  if (!open.value) return
  const target = event.target as Node | null
  if (target && (triggerRef.value?.contains(target) || panelRef.value?.contains(target))) return
  close()
}

function onDocumentKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') {
    close()
    triggerRef.value?.focus()
  }
}

function onViewportChange() {
  if (open.value) updatePosition()
}

onMounted(() => {
  document.addEventListener('pointerdown', onDocumentPointer, true)
  document.addEventListener('keydown', onDocumentKeydown)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('scroll', onViewportChange, true)
})

onBeforeUnmount(() => {
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
      class="inline-flex h-5 w-5 items-center justify-center rounded-full border border-amber-300 text-xs font-bold text-amber-700 transition-colors hover:bg-amber-100 focus:outline-none focus-visible:ring-2 focus-visible:ring-amber-500/50 dark:border-amber-700 dark:text-amber-300 dark:hover:bg-amber-900/40"
      :aria-label="text.aria"
      :aria-expanded="open"
      aria-haspopup="dialog"
      data-test="group-balance-warning-help"
      @mousedown.stop
      @click="toggle"
    >
      ?
    </button>
    <Teleport to="body">
      <section
        v-if="open"
        ref="panelRef"
        role="dialog"
        :aria-label="text.title"
        class="fixed z-[100000030] w-[min(20rem,calc(100vw-1rem))] rounded-xl border border-amber-200 bg-white p-4 text-left shadow-xl dark:border-amber-800 dark:bg-dark-800"
        :style="panelStyle"
        data-test="group-balance-warning-panel"
        @click.stop
        @mousedown.stop
      >
        <div class="mb-3 flex items-start justify-between gap-3">
          <h3 class="text-sm font-semibold text-gray-900 dark:text-white">{{ text.title }}</h3>
          <button
            type="button"
            class="rounded p-0.5 text-gray-400 hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-gray-200"
            :aria-label="zh ? '关闭' : 'Close'"
            @click="close"
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
