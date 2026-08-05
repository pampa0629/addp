<template>
  <main class="embedding-configuration">
    <header class="page-header">
      <div>
        <h1>{{ t('manager.embeddingConfiguration.title') }}</h1>
        <div class="status-line">
          <el-tag :type="form.apiKeyConfigured ? 'success' : 'warning'" effect="plain">
            {{ form.apiKeyConfigured
              ? t('manager.embeddingConfiguration.apiKeyConfigured')
              : t('manager.embeddingConfiguration.apiKeyMissing') }}
          </el-tag>
          <span>{{ t('manager.embeddingConfiguration.version', { version: form.version }) }}</span>
        </div>
      </div>
      <el-button :icon="Refresh" circle :loading="loading" :title="t('manager.embeddingConfiguration.reload')" @click="load" />
    </header>

    <el-form ref="formRef" v-loading="loading" :model="form" :rules="rules" label-position="top">
      <section class="form-section">
        <h2>{{ t('manager.embeddingConfiguration.serviceSection') }}</h2>
        <div class="form-grid">
          <el-form-item :label="t('manager.embeddingConfiguration.baseURL')" prop="baseURL">
            <el-input v-model.trim="form.baseURL" placeholder="https://embedding.example.com" />
          </el-form-item>
          <el-form-item :label="t('manager.embeddingConfiguration.model')" prop="model">
            <el-input v-model.trim="form.model" />
          </el-form-item>
          <el-form-item :label="t('manager.embeddingConfiguration.timeout')" prop="timeoutSeconds">
            <el-input-number v-model="form.timeoutSeconds" :min="1" :max="300" controls-position="right" />
          </el-form-item>
          <el-form-item :label="t('manager.embeddingConfiguration.dimension')">
            <el-input-number v-model="form.dimension" disabled controls-position="right" />
          </el-form-item>
        </div>
      </section>

      <section class="form-section">
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
  </main>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Check, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { getEmbeddingConfiguration, updateEmbeddingConfiguration } from '../api/embeddingConfiguration'

const { t } = useI18n()
const formRef = ref(null)
const loading = ref(false)
const saving = ref(false)
const form = reactive({
  version: 0,
  baseURL: '',
  model: '',
  timeoutSeconds: 15,
  dimension: 2560,
  maxDistance: 0.78,
  maxFileSizeMB: 10,
  batchConcurrency: 5,
  apiKeyConfigured: false
})

const rules = {
  baseURL: [{ required: true, message: () => t('manager.embeddingConfiguration.baseURLRequired'), trigger: 'blur' }],
  model: [{ required: true, message: () => t('manager.embeddingConfiguration.modelRequired'), trigger: 'blur' }]
}

function applyValue(value) {
  form.version = value.version
  form.baseURL = value.base_url
  form.model = value.model
  form.timeoutSeconds = value.timeout_seconds
  form.dimension = value.dimension
  form.maxDistance = value.max_distance
  form.maxFileSizeMB = value.max_file_size_mb
  form.batchConcurrency = value.batch_concurrency
  form.apiKeyConfigured = value.api_key_configured
}

async function load() {
  loading.value = true
  try {
    applyValue(await getEmbeddingConfiguration())
  } catch (error) {
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
    const value = await updateEmbeddingConfiguration({
      version: form.version,
      base_url: form.baseURL,
      model: form.model,
      timeout_seconds: form.timeoutSeconds,
      max_distance: form.maxDistance,
      max_file_size_mb: form.maxFileSizeMB,
      batch_concurrency: form.batchConcurrency
    })
    applyValue(value)
    ElMessage.success(t('manager.embeddingConfiguration.saveSuccess'))
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
.embedding-configuration {
  width: min(960px, 100%);
  margin: 0 auto;
  padding: 28px 32px 40px;
}

.page-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: 28px;
}

.page-header h1 {
  margin: 0 0 10px;
  color: var(--addp-text-primary);
  font-size: 24px;
  font-weight: 600;
  letter-spacing: 0;
}

.status-line {
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--addp-text-secondary);
  font-size: 13px;
}

.form-section {
  padding: 0 0 24px;
  margin-bottom: 26px;
  border-bottom: 1px solid var(--addp-border-color);
}

.form-section h2 {
  margin: 0 0 18px;
  color: var(--addp-text-primary);
  font-size: 16px;
  font-weight: 600;
  letter-spacing: 0;
}

.form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 0 24px;
}

.form-grid--three {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.form-grid :deep(.el-input-number) { width: 100%; }
.form-actions { display: flex; justify-content: flex-end; }

@media (max-width: 760px) {
  .embedding-configuration { padding: 20px 16px 32px; }
  .form-grid, .form-grid--three { grid-template-columns: 1fr; }
  .status-line { align-items: flex-start; flex-direction: column; }
}
</style>
