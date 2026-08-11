<template>
  <div class="border-t pt-4">
    <label class="text-sm font-medium text-gray-700 dark:text-gray-300">
      {{ t("admin.groups.modelsList.blocklistTitle") }}
    </label>
    <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
      {{ t("admin.groups.modelsList.blocklistHint") }}
    </p>

    <div class="mt-3 overflow-hidden rounded-lg border border-gray-200 bg-gray-50/50 dark:border-dark-600 dark:bg-dark-800/40">
      <div
        v-if="!loading && items.length > 0"
        class="flex items-center justify-between gap-2 border-b border-gray-200 bg-gray-50 px-3 py-2 text-xs dark:border-dark-600 dark:bg-dark-800"
      >
        <span class="text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.modelsList.blockedSummary", { blocked: blockedCount, total: items.length }) }}
        </span>
        <div class="flex items-center gap-1.5">
          <button
            type="button"
            class="rounded px-2 py-1 font-medium text-red-600 transition-colors hover:bg-red-50 dark:text-red-400 dark:hover:bg-red-900/20"
            @click="$emit('blockAll')"
          >
            {{ t("admin.groups.modelsList.blockAll") }}
          </button>
          <button
            type="button"
            class="rounded px-2 py-1 font-medium text-gray-600 transition-colors hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-dark-700"
            @click="$emit('invert')"
          >
            {{ t("admin.groups.modelsList.invertSelection") }}
          </button>
        </div>
      </div>

      <div class="max-h-64 space-y-2 overflow-y-auto p-2">
        <p v-if="loading" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.modelsList.loading") }}
        </p>
        <p v-else-if="items.length === 0" class="text-xs text-gray-500 dark:text-gray-400">
          {{ t("admin.groups.modelsList.empty") }}
        </p>
        <label
          v-for="item in items"
          :key="item.id"
          class="flex cursor-pointer items-center gap-2 rounded border border-gray-200 bg-white px-3 py-2 dark:border-dark-600 dark:bg-dark-800"
        >
          <input
            :checked="item.blocked"
            type="checkbox"
            class="h-4 w-4 rounded border-gray-300 text-red-600 focus:ring-red-500"
            @change="$emit('toggle', item.id)"
          />
          <span class="min-w-0 flex-1 break-all text-sm text-gray-700 dark:text-gray-300">
            {{ item.id }}
          </span>
        </label>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useI18n } from "vue-i18n";

interface BlocklistItem {
  id: string;
  blocked: boolean;
}

const props = defineProps<{
  items: BlocklistItem[];
  loading: boolean;
}>();

defineEmits<{
  toggle: [modelID: string];
  blockAll: [];
  invert: [];
}>();

const { t } = useI18n();
const blockedCount = computed(() => props.items.filter((item) => item.blocked).length);
</script>
