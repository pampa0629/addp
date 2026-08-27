<template>
  <div class="asset-create">
    <div class="page-header">
      <el-button link @click="back"><el-icon><ArrowLeft /></el-icon>{{ t('asset.assetCreate.back') }}</el-button>
      <h2>{{ t('asset.assetCreate.title') }}</h2>
    </div>
    <el-form ref="formRef" :model="form" :rules="rules" label-width="110px" class="asset-form">
      <el-form-item :label="t('asset.assetDetail.assetName')" prop="name">
        <el-input v-model="form.name" maxlength="500" />
      </el-form-item>
      <el-form-item :label="t('asset.assetDetail.assetType')" prop="type_id">
        <el-select v-model="form.type_id" style="width: 100%">
          <el-option v-for="item in types" :key="item.id" :label="item.name" :value="item.id" />
        </el-select>
      </el-form-item>
      <el-form-item :label="t('asset.assetDetail.category')">
        <el-cascader v-model="form.catalog_id" :options="catalogs" :props="{ checkStrictly: true, value: 'id', label: 'name', children: 'children', emitPath: false }" clearable style="width: 100%" />
      </el-form-item>
      <el-form-item :label="t('asset.assetDetail.description')">
        <el-input v-model="form.description" type="textarea" :rows="4" maxlength="2000" />
      </el-form-item>
      <el-form-item :label="t('asset.assetCreate.components')" prop="components">
        <CatalogEntryPicker v-model="form.components" class="full-width" />
      </el-form-item>
      <el-form-item>
        <el-button type="primary" :loading="submitting" @click="submit">{{ t('asset.assetCreate.create') }}</el-button>
        <el-button @click="back">{{ t('asset.catalogPicker.cancel') }}</el-button>
      </el-form-item>
    </el-form>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { ArrowLeft } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import CatalogEntryPicker from '../components/CatalogEntryPicker.vue'
import { assetAPI, catalogAPI, typeDefinitionAPI } from '../api/asset'
import { navigateAssetRoute } from '../utils/moduleNavigation'

const { t } = useI18n()
const router = useRouter()
const formRef = ref()
const submitting = ref(false)
const types = ref([])
const catalogs = ref([])
const form = reactive({ name: '', description: '', type_id: null, catalog_id: null, tags: [], components: [] })
const rules = {
  name: [{ required: true, message: t('asset.assetDetail.assetNameRequired'), trigger: 'blur' }],
  type_id: [{ required: true, message: t('asset.assetCreate.typeRequired'), trigger: 'change' }],
  components: [{ type: 'array', min: 1, message: t('asset.assetCreate.componentRequired'), trigger: 'change' }]
}

onMounted(async () => {
  try {
    const [typeResult, catalogResult] = await Promise.all([typeDefinitionAPI.list(), catalogAPI.tree()])
    types.value = (typeResult || []).filter(item => item.enabled)
    catalogs.value = catalogResult || []
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('asset.assetDetail.loadFailed'))
  }
})

async function submit() {
  await formRef.value.validate()
  submitting.value = true
  try {
    const created = await assetAPI.create(form)
    ElMessage.success(t('asset.assetCreate.created'))
    await navigateAssetRoute(router, `/assets/${created.id}`, { history: 'replace' })
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('asset.assetCreate.createFailed'))
  } finally {
    submitting.value = false
  }
}

function back() { navigateAssetRoute(router, '/assets', { history: 'replace' }) }
</script>

<style scoped>
.asset-create { padding: 24px; max-width: 960px; }
.page-header { display: flex; align-items: center; gap: 16px; margin-bottom: 20px; }
.page-header h2 { margin: 0; font-size: 20px; }
.asset-form { max-width: 820px; }
.full-width { width: 100%; }
</style>
