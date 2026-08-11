<template>
  <section class="binding-configuration">
    <header class="binding-header">
      <div>
        <h2>{{ t(`console.configuration.ai.${owner}.title`) }}</h2>
        <p>{{ contextLabel }}</p>
      </div>
      <el-button :icon="Refresh" circle :loading="loading" :title="t('console.configuration.reload')" @click="load" />
    </header>

    <el-table v-loading="loading" :data="rows" row-key="scenarioCode" class="binding-table">
      <el-table-column :label="t('console.configuration.ai.scenario')" min-width="220">
        <template #default="{ row }">
          <div class="scenario-name">{{ scenarioLabel(row.scenarioCode) }}</div>
          <code>{{ row.scenarioCode }}</code>
        </template>
      </el-table-column>
      <el-table-column :label="t('console.configuration.ai.modelProfile')" min-width="300">
        <template #default="{ row }">
          <el-select v-model="row.modelProfileId" filterable :disabled="!canUpdate" class="profile-select">
            <el-option
              v-for="profile in profiles"
              :key="profile.id"
              :label="profile.name"
              :value="profile.id"
            >
              <span>{{ profile.name }}</span>
              <span class="profile-code">{{ profile.code }}</span>
            </el-option>
          </el-select>
        </template>
      </el-table-column>
      <el-table-column :label="t('console.configuration.ai.source')" width="150">
        <template #default="{ row }">
          <el-tag v-if="row.configured" :type="row.inherited ? 'info' : 'success'" effect="plain">
            {{ row.inherited ? t('console.configuration.ai.inherited') : t('console.configuration.ai.currentScope') }}
          </el-tag>
          <el-tag v-else type="warning" effect="plain">{{ t('console.configuration.ai.notConfigured') }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column :label="t('console.configuration.ai.version')" width="100" align="center">
        <template #default="{ row }">{{ row.version || '-' }}</template>
      </el-table-column>
      <el-table-column width="110" align="right">
        <template #default="{ row }">
          <el-button
            type="primary"
            :icon="Check"
            :loading="row.saving"
            :disabled="!canUpdate || !row.modelProfileId"
            @click="save(row)"
          >
            {{ t('console.configuration.save') }}
          </el-button>
        </template>
      </el-table-column>
    </el-table>
  </section>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../../store/auth'
import { getInferenceBinding, listInferenceProfiles, updateInferenceBinding } from '../../api/inferenceConfiguration'
import { translateDynamicKey } from '../../utils/configurationI18n'

const props = defineProps({
  owner: { type: String, required: true, validator: (value) => ['agent', 'copilot'].includes(value) }
})

const SCENARIOS = {
  agent: ['reasoning', 'general-chat'],
  copilot: [
    'resource_resolution',
    'query_generation',
    'workflow_generation',
    'notebook_generation',
    'transfer_generation',
    'navigation_guide',
    'knowledge_graph_extraction'
  ]
}

const { t } = useI18n()
const authStore = useAuthStore()
const loading = ref(false)
const profiles = ref([])
const rows = ref([])
const contextType = computed(() => authStore.contextType || 'tenant')
const contextLabel = computed(() => contextType.value === 'tenant'
  ? t('console.configuration.tenantContext')
  : t('console.configuration.platformContext'))
const canUpdate = computed(() => authStore.hasPermission(`${props.owner}.configuration.update`))
const scenarioLabel = scenarioCode => translateDynamicKey(
  t,
  'console.configuration.ai.scenarios',
  scenarioCode
)

async function load() {
  loading.value = true
  try {
    const [profilePage, ...bindings] = await Promise.all([
      listInferenceProfiles(),
      ...SCENARIOS[props.owner].map((scenario) => getInferenceBinding(props.owner, scenario))
    ])
    profiles.value = profilePage.data || []
    rows.value = SCENARIOS[props.owner].map((scenarioCode, index) => {
      const binding = bindings[index]
      const configured = Boolean(binding.model_profile_id)
      return {
        scenarioCode,
        modelProfileId: binding.model_profile_id || '',
        version: binding.version || 0,
        expectedVersion: binding.scope_type === contextType.value ? binding.version : 0,
        inherited: configured && binding.scope_type !== contextType.value,
        configured,
        saving: false
      }
    })
  } catch (error) {
    rows.value = []
    ElMessage.error(error?.response?.data?.error || t('console.configuration.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save(row) {
  row.saving = true
  try {
    await updateInferenceBinding(props.owner, row.scenarioCode, {
      version: row.expectedVersion,
      model_profile_id: row.modelProfileId
    })
    ElMessage.success(t('console.configuration.saveSuccess'))
    await load()
  } catch (error) {
    if (error?.response?.status === 409) {
      ElMessage.warning(t('console.configuration.versionConflict'))
      await load()
    } else {
      ElMessage.error(error?.response?.data?.error || t('console.configuration.saveFailed'))
    }
  } finally {
    row.saving = false
  }
}

onMounted(load)
</script>

<style scoped>
.binding-configuration { width: 100%; }
.binding-header { display: flex; align-items: center; justify-content: space-between; gap: 20px; margin-bottom: 20px; }
.binding-header h2 { margin: 0 0 6px; color: var(--addp-text-primary); font-size: 20px; font-weight: 600; letter-spacing: 0; }
.binding-header p { margin: 0; color: var(--addp-text-secondary); font-size: 14px; }
.binding-table { width: 100%; }
.scenario-name { color: var(--addp-text-primary); font-weight: 500; }
code { color: var(--addp-text-tertiary); font-size: 12px; }
.profile-select { width: 100%; }
.profile-code { float: right; margin-left: 20px; color: var(--addp-text-tertiary); }
</style>
