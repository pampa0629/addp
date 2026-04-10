<template>
  <div class="page-container">
    <el-card>
      <template #header>
        <div class="card-header">
          <span>{{ t('system.tenant.title') }}</span>
          <el-button type="primary" :icon="Plus" @click="openAddDialog">{{ t('system.tenant.add') }}</el-button>
        </div>
      </template>

      <el-table :data="tenants" v-loading="loading" stripe>
        <el-table-column prop="id" :label="t('system.tenant.columns.id')" width="80" />
        <el-table-column prop="name" :label="t('system.tenant.columns.name')" />
        <el-table-column prop="description" :label="t('system.tenant.columns.description')" />
        <el-table-column :label="t('system.tenant.columns.status')" width="100">
          <template #default="{ row }">
            <el-tag :type="row.is_active ? 'success' : 'danger'">
              {{ row.is_active ? t('system.tenant.status.active') : t('system.tenant.status.disabled') }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column :label="t('system.tenant.columns.createdAt')" width="180">
          <template #default="{ row }">
            {{ formatDate(row.created_at) }}
          </template>
        </el-table-column>
        <el-table-column :label="t('system.tenant.columns.actions')" width="180" fixed="right">
          <template #default="{ row }">
            <el-button size="small" type="primary" :icon="Edit" @click="openEditDialog(row)">{{ t('system.tenant.actions.edit') }}</el-button>
            <el-button
              size="small"
              type="danger"
              :icon="Delete"
              @click="handleDelete(row)"
            >{{ t('system.tenant.actions.delete') }}</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-pagination
        v-model:current-page="currentPage"
        v-model:page-size="pageSize"
        :total="total"
        :page-sizes="[10, 20, 50, 100]"
        layout="total, sizes, prev, pager, next, jumper"
        @size-change="fetchTenants"
        @current-change="fetchTenants"
        style="margin-top: 20px; justify-content: flex-end;"
      />
    </el-card>

    <!-- 新增/编辑租户对话框 -->
    <el-dialog
      v-model="dialogVisible"
      :title="isEdit ? t('system.tenant.dialog.edit') : t('system.tenant.dialog.add')"
      width="600px"
      @close="resetForm"
    >
      <el-form
        ref="formRef"
        :model="tenantForm"
        :rules="formRules"
        label-width="120px"
      >
        <el-form-item :label="t('system.tenant.form.name')" prop="name">
          <el-input v-model="tenantForm.name" :placeholder="t('system.tenant.form.namePlaceholder')" />
        </el-form-item>
        <el-form-item :label="t('system.tenant.form.description')" prop="description">
          <el-input
            v-model="tenantForm.description"
            type="textarea"
            :rows="3"
            :placeholder="t('system.tenant.form.descPlaceholder')"
          />
        </el-form-item>

        <!-- 新增租户时需要设置管理员 -->
        <template v-if="!isEdit">
          <el-divider content-position="left">{{ t('system.tenant.form.adminSection') }}</el-divider>
          <el-form-item :label="t('system.tenant.form.adminUsername')" prop="admin_username">
            <el-input v-model="tenantForm.admin_username" :placeholder="t('system.tenant.form.adminUsernamePlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('system.tenant.form.adminPassword')" prop="admin_password">
            <el-input v-model="tenantForm.admin_password" type="password" :placeholder="t('system.tenant.form.adminPasswordPlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('system.tenant.form.adminEmail')" prop="admin_email">
            <el-input v-model="tenantForm.admin_email" :placeholder="t('system.tenant.form.adminEmailPlaceholder')" />
          </el-form-item>
          <el-form-item :label="t('system.tenant.form.adminFullName')" prop="admin_full_name">
            <el-input v-model="tenantForm.admin_full_name" :placeholder="t('system.tenant.form.adminFullNamePlaceholder')" />
          </el-form-item>
        </template>
      </el-form>

      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.tenant.dialog.cancel') }}</el-button>
        <el-button type="primary" @click="handleSubmit" :loading="submitting">
          {{ isEdit ? t('system.tenant.dialog.update') : t('system.tenant.dialog.create') }}
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, nextTick } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Plus, Edit, Delete } from '@element-plus/icons-vue'
import { tenantAPI } from '../api/tenant'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const tenants = ref([])
const loading = ref(false)
const currentPage = ref(1)
const pageSize = ref(10)
const total = ref(0)

const dialogVisible = ref(false)
const isEdit = ref(false)
const editingTenantId = ref(null)
const submitting = ref(false)
const formRef = ref(null)

const tenantForm = ref({
  name: '',
  description: '',
  admin_username: '',
  admin_password: '',
  admin_email: '',
  admin_full_name: ''
})

const formRules = computed(() => ({
  name: [
    { required: true, message: t('system.tenant.rules.nameRequired'), trigger: 'blur' }
  ],
  admin_username: [
    { required: true, message: t('system.tenant.rules.adminUsernameRequired'), trigger: 'blur' }
  ],
  admin_password: [
    { required: true, message: t('system.tenant.rules.adminPasswordRequired'), trigger: 'blur' },
    { min: 6, message: t('system.tenant.rules.adminPasswordMin'), trigger: 'blur' }
  ]
}))

const fetchTenants = async () => {
  loading.value = true
  try {
    const response = await tenantAPI.list({
      page: currentPage.value,
      page_size: pageSize.value
    })
    tenants.value = response?.data || []
    total.value = response?.total || 0
  } catch (error) {
    ElMessage.error(t('system.tenant.msg.loadFailed'))
  } finally {
    loading.value = false
  }
}

const openAddDialog = () => {
  isEdit.value = false
  editingTenantId.value = null
  tenantForm.value = {
    name: '',
    description: '',
    admin_username: '',
    admin_password: '',
    admin_email: '',
    admin_full_name: ''
  }
  dialogVisible.value = true
  nextTick(() => {
    formRef.value?.clearValidate()
  })
}

const openEditDialog = (tenant) => {
  isEdit.value = true
  editingTenantId.value = tenant.id
  tenantForm.value = {
    name: tenant.name,
    description: tenant.description
  }
  dialogVisible.value = true
  nextTick(() => {
    formRef.value?.clearValidate()
  })
}

const resetForm = () => {
  formRef.value?.resetFields()
}

const handleSubmit = async () => {
  if (!formRef.value) return

  try {
    await formRef.value.validate()
    submitting.value = true

    if (isEdit.value) {
      await tenantAPI.update(editingTenantId.value, {
        name: tenantForm.value.name,
        description: tenantForm.value.description
      })
      ElMessage.success(t('system.tenant.msg.updateSuccess'))
    } else {
      await tenantAPI.create(tenantForm.value)
      ElMessage.success(t('system.tenant.msg.createSuccess'))
    }

    dialogVisible.value = false
    fetchTenants()
  } catch (error) {
    if (error.response?.data?.error) {
      ElMessage.error(error.response.data.error)
    } else {
      ElMessage.error(isEdit.value ? t('system.tenant.msg.updateFailed') : t('system.tenant.msg.createFailed'))
    }
  } finally {
    submitting.value = false
  }
}

const handleDelete = (tenant) => {
  ElMessageBox.confirm(
    t('system.tenant.msg.deleteConfirm', { name: tenant.name }),
    t('system.tenant.msg.deleteWarning'),
    {
      confirmButtonText: 'OK',
      cancelButtonText: 'Cancel',
      type: 'warning',
    }
  ).then(async () => {
    try {
      await tenantAPI.delete(tenant.id)
      ElMessage.success(t('system.tenant.msg.deleteSuccess'))
      fetchTenants()
    } catch (error) {
      ElMessage.error(t('system.tenant.msg.deleteFailed'))
    }
  }).catch(() => {
    // 用户取消删除
  })
}

const formatDate = (dateString) => {
  if (!dateString) return '-'
  return new Date(dateString).toLocaleString()
}

onMounted(() => {
  fetchTenants()
})
</script>

<style scoped>
.page-container {
  padding: 20px;
}

.card-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
</style>
