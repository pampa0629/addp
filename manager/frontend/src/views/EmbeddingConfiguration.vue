<template>
  <main class="embedding-configuration">
    <header class="page-header">
      <div>
        <h1>{{ t('manager.embeddingConfiguration.title') }}</h1>
        <div class="status-line">
          <el-tag effect="plain">{{ scopeLabel }}</el-tag>
          <span>{{ t('manager.embeddingConfiguration.bindingVersion', { version: binding.version }) }}</span>
        </div>
      </div>
      <el-button :icon="Refresh" circle :loading="loading" :title="t('manager.embeddingConfiguration.reload')" @click="load" />
    </header>

    <el-tabs v-model="activeTab" class="configuration-tabs">
      <el-tab-pane :label="t('manager.embeddingConfiguration.tabs.embedding')" name="embedding">
    <el-form ref="formRef" v-loading="loading" :model="form" :rules="rules" label-position="top">
      <section class="form-section">
        <h2>{{ t('manager.embeddingConfiguration.profileSection') }}</h2>
        <div class="form-grid">
          <el-form-item :label="t('manager.embeddingConfiguration.modelProfile')" prop="modelProfileID">
            <el-select v-model="form.modelProfileID" filterable class="profile-select">
              <el-option
                v-for="profile in compatibleProfiles"
                :key="profile.id"
                :label="`${profile.name} · ${profile.code} · v${profile.version}`"
                :value="profile.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item :label="t('manager.embeddingConfiguration.dimension')">
            <el-input-number :model-value="2560" disabled controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <section v-if="isPlatform" class="form-section">
        <h2>{{ t('manager.embeddingConfiguration.policySection') }}</h2>
        <div class="form-grid form-grid--three">
          <el-form-item :label="t('manager.embeddingConfiguration.maxDistance')" prop="maxDistance">
            <el-input-number v-model="form.maxDistance" :min="0.01" :max="2" :step="0.01" :precision="2" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('manager.embeddingConfiguration.maxFileSize')" prop="maxFileSizeMB">
            <el-input-number v-model="form.maxFileSizeMB" :min="1" :max="1024" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('manager.embeddingConfiguration.batchConcurrency')" prop="batchConcurrency">
            <el-input-number v-model="form.batchConcurrency" :min="1" :max="64" controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <footer class="form-actions">
        <el-button type="primary" :icon="Check" :loading="saving" @click="save">
          {{ t('manager.embeddingConfiguration.save') }}
        </el-button>
      </footer>
    </el-form>
      </el-tab-pane>
      <el-tab-pane v-if="isPlatform" :label="t('manager.embeddingConfiguration.tabs.quickViewPolicy')" name="quick-view-policy">
        <QuickViewPolicyConfiguration />
      </el-tab-pane>
    </el-tabs>
  </main>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useAuthStore } from '../store/auth'
import {
  getEmbeddingConfiguration,
  getInferenceBinding,
  listInferenceDeployments,
  listInferenceProfiles,
  updateEmbeddingConfiguration,
  updateInferenceBinding
} from '../api/embeddingConfiguration'
import QuickViewPolicyConfiguration from '../components/QuickViewPolicyConfiguration.vue'

const { t } = useI18n()
const authStore = useAuthStore()
const formRef = ref(null)
const loading = ref(false)
const saving = ref(false)
const activeTab = ref('embedding')
const profiles = ref([])
const deployments = ref([])
const binding = reactive({ version: 0, scopeType: '', effective: false })
const form = reactive({
  configurationVersion: 0,
  modelProfileID: '',
  maxDistance: 0.78,
  maxFileSizeMB: 10,
  batchConcurrency: 5
})

const isPlatform = computed(() => authStore.contextType === 'platform')
const scopeLabel = computed(() => t(`manager.embeddingConfiguration.scope.${isPlatform.value ? 'platform' : 'tenant'}`))
const deploymentByID = computed(() => new Map(deployments.value.map(item => [item.id, item])))
const compatibleProfiles = computed(() => profiles.value.filter(profile => {
  if (profile.status !== 'active') return false
  const deployment = deploymentByID.value.get(profile.model_deployment_id)
  return deployment?.status === 'active' && deployment.dimension === 2560 &&
    deployment.operations?.includes('embedding') &&
    deployment.modalities?.includes('text') && deployment.modalities?.includes('image')
}))
const rules = {
  modelProfileID: [{ required: true, message: () => t('manager.embeddingConfiguration.modelProfileRequired'), trigger: 'change' }]
}

async function load() {
  loading.value = true
  try {
    const requests = [getInferenceBinding(), listInferenceProfiles(), listInferenceDeployments()]
    if (isPlatform.value) requests.push(getEmbeddingConfiguration())
    const [bindingValue, profilePage, deploymentPage, policy] = await Promise.all(requests)
    Object.assign(binding, { version: bindingValue.version || 0, scopeType: bindingValue.scope_type, effective: bindingValue.effective })
    form.modelProfileID = bindingValue.model_profile_id || ''
    profiles.value = profilePage.data || []
    deployments.value = deploymentPage.data || []
    if (policy) {
      form.configurationVersion = policy.version
      form.maxDistance = policy.max_distance
      form.maxFileSizeMB = policy.max_file_size_mb
      form.batchConcurrency = policy.batch_concurrency
    }
  } catch (_error) {
    ElMessage.error(t('manager.embeddingConfiguration.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return
  saving.value = true
  try {
    const requests = [updateInferenceBinding({ version: binding.version, model_profile_id: form.modelProfileID })]
    if (isPlatform.value) {
      requests.push(updateEmbeddingConfiguration({
        version: form.configurationVersion,
        max_distance: form.maxDistance,
        max_file_size_mb: form.maxFileSizeMB,
        batch_concurrency: form.batchConcurrency
      }))
    }
    await Promise.all(requests)
    ElMessage.success(t('manager.embeddingConfiguration.saveSuccess'))
    await load()
  } catch (error) {
    if (error?.response?.status === 409) {
      ElMessage.warning(t('manager.embeddingConfiguration.versionConflict'))
      await load()
    } else {
      ElMessage.error(t('manager.embeddingConfiguration.saveFailed'))
    }
  } finally {
    saving.value = false
  }
}

load()
</script>

<style scoped>
.embedding-configuration { width: min(960px, 100%); margin: 0 auto; padding: 28px 32px 40px; box-sizing: border-box; }
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 28px; }
.page-header h1 { margin: 0 0 10px; color: var(--addp-text-primary); font-size: 24px; font-weight: 600; letter-spacing: 0; }
.status-line { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; color: var(--addp-text-secondary); font-size: 13px; }
.form-section { padding: 0 0 24px; margin-bottom: 26px; border-bottom: 1px solid var(--addp-border-color); }
.form-section h2 { margin: 0 0 18px; color: var(--addp-text-primary); font-size: 16px; font-weight: 600; letter-spacing: 0; }
.form-grid { display: grid; grid-template-columns: minmax(0, 2fr) minmax(180px, 1fr); gap: 0 20px; }
.form-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.profile-select { width: 100%; }
.form-actions { display: flex; justify-content: flex-end; }
@media (max-width: 720px) {
  .embedding-configuration { padding: 20px 16px 32px; }
  .form-grid, .form-grid--three { grid-template-columns: minmax(0, 1fr); }
}
</style>
