<template>
  <div class="page-container">
    <div class="page-header">
      <el-button link @click="$router.push('/ontologies')">
        <el-icon><ArrowLeft /></el-icon> 返回
      </el-button>
      <h2>{{ isEdit ? '编辑本体' : '新建本体' }}</h2>
    </div>

    <el-card style="max-width: 600px">
      <el-form ref="formRef" :model="form" :rules="rules" label-width="100px">
        <el-form-item label="名称" prop="name">
          <el-input v-model="form.name" placeholder="本体名称（唯一标识）" />
        </el-form-item>
        <el-form-item label="描述" prop="description">
          <el-input v-model="form.description" type="textarea" :rows="3" placeholder="可选" />
        </el-form-item>
        <el-form-item v-if="isEdit" label="状态">
          <el-select v-model="form.status">
            <el-option label="启用" value="active" />
            <el-option label="归档" value="archived" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="saving" @click="handleSubmit">
            {{ saving ? '保存中...' : '保存' }}
          </el-button>
          <el-button @click="$router.back()">取消</el-button>
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

const route = useRoute()
const router = useRouter()
const formRef = ref(null)
const saving = ref(false)
const isEdit = computed(() => !!route.params.id)
const form = ref({ name: '', description: '', status: 'active' })
const rules = {
  name: [{ required: true, message: '请输入本体名称', trigger: 'blur' }]
}

onMounted(async () => {
  if (isEdit.value) {
    try {
      const res = await ontologyAPI.get(route.params.id)
      const d = res
      form.value = { name: d.name, description: d.description || '', status: d.status }
    } catch {
      ElMessage.error('加载失败')
    }
  }
})

const handleSubmit = async () => {
  await formRef.value.validate()
  saving.value = true
  try {
    if (isEdit.value) {
      await ontologyAPI.update(route.params.id, form.value)
      ElMessage.success('更新成功')
      router.push(`/ontologies/${route.params.id}`)
    } else {
      const res = await ontologyAPI.create(form.value)
      ElMessage.success('创建成功')
      router.push(`/ontologies/${res.id}`)
    }
  } catch (e) {
    ElMessage.error(e.response?.data?.error || '保存失败')
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
