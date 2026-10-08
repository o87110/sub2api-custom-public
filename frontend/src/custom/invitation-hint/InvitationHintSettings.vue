<template>
  <div class="space-y-4 border-t border-gray-100 pt-4 dark:border-dark-700" data-testid="invitation-hint-settings">
    <div>
      <label for="invitation_code_hint_text" class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('admin.settings.registration.invitationCodeHintText') }}
      </label>
      <input
        id="invitation_code_hint_text"
        type="text"
        class="input"
        :value="text"
        :placeholder="t('admin.settings.registration.invitationCodeHintTextPlaceholder')"
        @input="emit('update:text', ($event.target as HTMLInputElement).value)"
      />
      <p class="input-hint">{{ t('admin.settings.registration.invitationCodeHintTextHint') }}</p>
    </div>
    <div>
      <label for="invitation_code_hint_url" class="mb-2 block text-sm font-medium text-gray-700 dark:text-gray-300">
        {{ t('admin.settings.registration.invitationCodeHintURL') }}
      </label>
      <input
        id="invitation_code_hint_url"
        type="url"
        class="input"
        :value="url"
        :placeholder="t('admin.settings.registration.invitationCodeHintURLPlaceholder')"
        @input="emit('update:url', ($event.target as HTMLInputElement).value)"
      />
      <p class="input-hint">{{ t('admin.settings.registration.invitationCodeHintURLHint') }}</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'

defineProps<{ text: string; url: string }>()
const emit = defineEmits<{
  'update:text': [value: string]
  'update:url': [value: string]
}>()
const { t } = useI18n()
</script>
