<template>
  <section class="oauth-context">
    <div class="context-label">{{ t('console.oauth.context.label') }}</div>
    <div class="context-control">
      <el-select
        v-model="selectedKey"
        :loading="loading"
        :placeholder="t('console.oauth.context.placeholder')"
        class="context-select"
      >
        <el-option
          v-for="option in options"
          :key="contextOptionKey(option)"
          :label="optionLabel(option)"
          :value="contextOptionKey(option)"
          :disabled="option.requires_step_up"
        />
      </el-select>
      <el-button
        :icon="Switch"
        :loading="switching"
        :disabled="!canSwitch"
        @click="switchSelectedContext"
      >
        {{ t('console.oauth.context.switch') }}
      </el-button>
    </div>
    <p class="context-note">{{ t('console.oauth.context.binding') }}</p>
    <el-alert v-if="error" :title="error" type="error" :closable="false" />
  </section>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { Switch } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '../store/auth'
import { contextChoice, contextOptionKey, currentContextOption } from '../oauth/context'

const { t } = useI18n()
const authStore = useAuthStore()
const loading = ref(true)
const switching = ref(false)
const error = ref('')
const options = ref([])
const selectedKey = ref('')

const selectedOption = computed(() => options.value.find(
  (option) => contextOptionKey(option) === selectedKey.value
) || null)
const canSwitch = computed(() => Boolean(
  selectedOption.value && !selectedOption.value.current && !selectedOption.value.requires_step_up
))

function optionLabel(option) {
  const current = option.current ? t('console.oauth.context.currentSuffix') : ''
  const stepUp = option.requires_step_up ? t('console.oauth.context.stepUpSuffix') : ''
  if (option.type === 'platform') {
    return `${t('console.oauth.context.platform')}${current}${stepUp}`
  }
  return t('console.oauth.context.tenant', {
    name: option.tenant_name || option.tenant_code || option.tenant_id,
    code: option.tenant_code || option.tenant_id,
    current,
    stepUp
  })
}

async function loadOptions() {
  loading.value = true
  error.value = ''
  try {
    options.value = await authStore.fetchContextOptions()
    const current = currentContextOption(options.value)
    selectedKey.value = current ? contextOptionKey(current) : ''
  } catch {
    error.value = t('console.oauth.context.loadFailed')
  } finally {
    loading.value = false
  }
}

async function switchSelectedContext() {
  if (!canSwitch.value) return
  switching.value = true
  error.value = ''
  try {
    await authStore.switchContext(contextChoice(selectedOption.value))
    window.location.reload()
  } catch (requestError) {
    error.value = requestError.response?.data?.error || t('console.oauth.context.switchFailed')
    switching.value = false
  }
}

onMounted(loadOptions)
</script>

<style scoped>
.oauth-context { margin: 0 0 20px; }
.context-label { margin-bottom: 8px; color: var(--addp-text-secondary); font-size: 13px; }
.context-control { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 10px; align-items: center; }
.context-select { width: 100%; }
.context-note { margin: 8px 0 0; color: var(--addp-text-tertiary); font-size: 12px; line-height: 1.5; }
.el-alert { margin-top: 12px; }

@media (max-width: 520px) {
  .context-control { grid-template-columns: minmax(0, 1fr); }
  .context-control :deep(.el-button) { width: 100%; margin: 0; }
}
</style>
