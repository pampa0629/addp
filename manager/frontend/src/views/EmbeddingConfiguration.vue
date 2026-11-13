<template>
  <main v-loading="loading" class="embedding-configuration" :class="{ 'embedding-configuration--embedded': isEmbedded }">
    <header class="page-header">
      <div>
        <h1 v-if="!isEmbedded">{{ t(isSegmentation ? 'manager.segment.title' : 'manager.embeddingConfiguration.title') }}</h1>
        <div class="status-line">
          <el-tag effect="plain">{{ scopeLabel }}</el-tag>
          <span v-if="loaded">{{ t('manager.embeddingConfiguration.bindingVersion', { version: bindingVersion }) }}</span>
        </div>
      </div>
      <el-button :icon="Refresh" circle :loading="loading" :disabled="saving" :title="t('manager.embeddingConfiguration.reload')" @click="load" />
    </header>

    <el-form v-if="loaded" ref="formRef" :model="form" :rules="rules" :disabled="!canSave" label-position="top" @submit.prevent>
      <section class="form-section">
        <h2>{{ t('manager.embeddingConfiguration.profileSection') }}</h2>
        <div class="form-grid" :class="{ 'form-grid--single': isSegmentation }">
          <el-form-item :label="t('manager.embeddingConfiguration.modelProfile')" prop="modelProfileID">
            <el-select v-model="form.modelProfileID" filterable class="profile-select">
              <el-option
                v-for="profile in compatibleProfiles"
                :key="profile.id"
                :label="isSegmentation ? profile.name : `${profile.name} · ${profile.code} · v${profile.version}`"
                :value="profile.id"
              />
            </el-select>
          </el-form-item>
          <el-form-item v-if="!isSegmentation" :label="t('manager.embeddingConfiguration.dimension')">
            <el-input-number :model-value="2560" disabled controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <section v-if="!isSegmentation && isPlatform" class="form-section">
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
        <el-button type="primary" :icon="Check" :loading="saving" :disabled="!canSave" @click="save">
          {{ t('manager.embeddingConfiguration.save') }}
        </el-button>
      </footer>
    </el-form>
  </main>
</template>

<script setup>
import { computed, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useRoute, useRouter } from 'vue-router'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useUnsavedChangesGuard } from '@common-ui'
import { useAuthStore } from '../store/auth'
import {
  getEmbeddingConfiguration,
  getInferenceBinding,
  listInferenceDeployments,
  listInferenceProfiles,
  updateEmbeddingConfiguration,
  updateInferenceBinding
} from '../api/embeddingConfiguration'

const { t } = useI18n()
const authStore = useAuthStore()
const formRef = ref(null)
const loading = ref(false)
const loaded = ref(false)
const saving = ref(false)
const canSave = computed(() => loaded.value && !saving.value && authStore.hasPermission('manager.configuration.update'))
const isSegmentation = useRoute().query.tab === 'segmentation'
const isEmbedded = window.self !== window.top
const scenario = isSegmentation ? 'image_segmentation' : 'semantic_search_embedding'
const profiles = ref([])
const deployments = ref([])
const bindingVersion = ref(0)
const form = reactive({
  configurationVersion: 0,
  modelProfileID: '',
  maxDistance: 0.78,
  maxFileSizeMB: 10,
  batchConcurrency: 5
})
const savedForm = ref('')
const captureForm = () => JSON.stringify(form)
useUnsavedChangesGuard({
  router: useRouter(),
  isDirty: () => loaded.value && captureForm() !== savedForm.value
})

const isPlatform = computed(() => authStore.contextType === 'platform')
const scopeLabel = computed(() => t(`manager.embeddingConfiguration.scope.${isPlatform.value ? 'platform' : 'tenant'}`))
const deploymentByID = computed(() => new Map(deployments.value.map(item => [item.id, item])))
const compatibleProfiles = computed(() => profiles.value.filter(profile => {
  if (profile.status !== 'active') return false
  const deployment = deploymentByID.value.get(profile.model_deployment_id)
  if (deployment?.status !== 'active' || !deployment.modalities?.includes('image')) return false
  if (isSegmentation) return deployment.operations?.includes('segmentation')
  return deployment.operations?.includes('embedding') && deployment.modalities.includes('text') && deployment.dimension === 2560
}))
const rules = {
  modelProfileID: [{ required: true, message: () => t('manager.embeddingConfiguration.modelProfileRequired'), trigger: 'change' }]
}

async function load() {
  loaded.value = false
  loading.value = true
  try {
    const requests = [getInferenceBinding(scenario), listInferenceProfiles(), listInferenceDeployments()]
    if (!isSegmentation && isPlatform.value) requests.push(getEmbeddingConfiguration())
    const [bindingValue, profilePage, deploymentPage, policy] = await Promise.all(requests)
    bindingVersion.value = bindingValue.version || 0
    form.modelProfileID = bindingValue.model_profile_id || ''
    profiles.value = profilePage.data || []
    deployments.value = deploymentPage.data || []
    if (policy) {
      form.configurationVersion = policy.version
      form.maxDistance = policy.max_distance
      form.maxFileSizeMB = policy.max_file_size_mb
      form.batchConcurrency = policy.batch_concurrency
    }
    savedForm.value = captureForm()
    loaded.value = true
  } catch (_error) {
    ElMessage.error(t('manager.embeddingConfiguration.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function save() {
  if (!canSave.value) return
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid || !canSave.value) return
  saving.value = true
  try {
    const requests = [updateInferenceBinding({ version: bindingVersion.value, model_profile_id: form.modelProfileID }, scenario)]
    if (!isSegmentation && isPlatform.value) {
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
.page-header { display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; margin-bottom: 20px; }
.page-header h1 { margin: 0 0 10px; color: var(--addp-text-primary); font-size: 24px; font-weight: 600; letter-spacing: 0; }
.status-line { display: flex; align-items: center; flex-wrap: wrap; gap: 12px; color: var(--addp-text-secondary); font-size: 13px; }
.form-section { padding: 0 0 16px; margin-bottom: 20px; border-bottom: 1px solid var(--addp-border-color); }
.form-section h2 { margin: 0 0 18px; color: var(--addp-text-primary); font-size: 16px; font-weight: 600; letter-spacing: 0; }
.form-grid { display: grid; grid-template-columns: minmax(0, 2fr) minmax(180px, 1fr); gap: 0 20px; }
.form-grid--single { grid-template-columns: minmax(0, 480px); }
.form-grid--three { grid-template-columns: repeat(3, minmax(0, 1fr)); }
.profile-select { width: 100%; }
.form-actions { display: flex; justify-content: flex-end; }
@media (max-width: 720px) {
  .embedding-configuration { padding: 20px 16px 32px; }
  .form-grid, .form-grid--three { grid-template-columns: minmax(0, 1fr); }
}
.embedding-configuration--embedded { width: 100%; padding: 8px 0 24px; }
</style>
