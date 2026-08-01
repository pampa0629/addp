<template>
  <div class="page-container">
    <div class="page-header">
      <el-button link @click="returnToList">
        <el-icon><ArrowLeft /></el-icon> {{ t('graph.common.back') }}
      </el-button>
      <h2>{{ isEdit ? t('graph.ontology.edit') : t('graph.ontology.create') }}</h2>
    </div>

    <el-card style="max-width: 600px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item :label="t('graph.ontology.nameLabel')" prop="name">
          <el-input v-model="form.name" :placeholder="t('graph.ontology.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('graph.common.description')" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="3" :placeholder="t('graph.ontology.descPlaceholder')" />
        </el-form-item>
        <el-form-item v-if="isEdit" :label="t('graph.ontology.statusLabel')">
          <el-select v-model="form.status">
            <el-option :label="t('graph.common.active')" value="active" />
            <el-option :label="t('graph.common.archived')" value="archived" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="handleSubmit">
            {{ saving ? t('graph.common.saving') : t('graph.common.save') }}
          </el-button>
          <el-button @click="returnToList">{{ t('graph.common.cancel') }}</el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ontologyAPI } from '../api/ontology'
import { useI18n } from 'vue-i18n'
import { navigateGraphRoute } from '@/utils/moduleNavigation'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
const formRef = ref(null)
const saving = ref(false)
const isEdit = computed(() => !!route.params.id)
const form = ref({ name: '', description: '', status: 'active' })
const rules = computed(() => ({
  name: [{ required: true, message: t('graph.ontology.nameRequired'), trigger: 'blur' }]
}))
const returnToList = () => navigateGraphRoute(router, '/ontologies', { history: 'replace' })

onMounted(async () => {
  if (isEdit.value) {
    try {
      const res = await ontologyAPI.get(route.params.id)
      const d = res
      form.value = { name: d.name, description: d.description || '', status: d.status }
    } catch {
      ElMessage.error(t('graph.common.loadFailed'))
    }
  }
})

const handleSubmit = async () => {
  await formRef.value.validate()
  saving.value = true
  try {
    if (isEdit.value) {
      await ontologyAPI.update(route.params.id, form.value)
      ElMessage.success(t('graph.common.updateSuccess'))
      await navigateGraphRoute(router, `/ontologies/${route.params.id}`, { history: 'replace' })
    } else {
      const res = await ontologyAPI.create(form.value)
      ElMessage.success(t('graph.common.createSuccess'))
      await navigateGraphRoute(router, `/ontologies/${res.id}`, { history: 'replace' })
    }
  } catch (e) {
    ElMessage.error(e.response?.data?.error || t('graph.common.saveFailed'))
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.page-container { padding: 20px; }
.page-header { display: flex; align-items: center; gap: 8px; margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 18px; }
</style>
