<template>
  <section class="iam-panel">
    <div class="iam-toolbar">
      <el-button :icon="Refresh" @click="load">{{ t('system.iam.common.refresh') }}</el-button>
      <el-button v-if="can('iam.tenant_role.create')" type="primary" :icon="Plus" @click="openCreate">
        {{ t('system.iam.roles.create') }}
      </el-button>
    </div>

    <el-table v-loading="loading" :data="rows" stripe>
      <el-table-column :label="t('system.iam.roles.role')" min-width="220">
        <template #default="{ row }">
          <div class="iam-primary-cell"><strong>{{ roleName(row) }}</strong><span>{{ row.role_key }}</span></div>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.roles.type')" width="150">
        <template #default="{ row }">{{ t(`system.iam.roles.types.${row.role_type}`) }}</template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.description')" min-width="260">
        <template #default="{ row }"><span class="iam-list-text">{{ roleDescription(row) || '-' }}</span></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.roles.scopes')" min-width="180">
        <template #default="{ row }"><el-tag v-for="scope in row.allowed_scope_types" :key="scope" class="iam-inline-tag" effect="plain">{{ scopeLabel(scope) }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.roles.permissions')" min-width="280">
        <template #default="{ row }"><span class="iam-list-text">{{ row.permission_keys.join(', ') }}</span></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="170" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.tenant_role.update') && !row.immutable" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="can('iam.tenant_role.delete') && !row.immutable" link type="danger" :icon="Delete" @click="removeRole(row)">{{ t('system.iam.common.delete') }}</el-button>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" :title="editing ? t('system.iam.roles.edit') : t('system.iam.roles.create')" width="min(680px, calc(100% - 24px))">
      <el-form ref="formRef" :model="form" :rules="rules" label-position="top">
        <el-form-item v-if="!editing" :label="t('system.iam.roles.key')" prop="roleKey"><el-input v-model="form.roleKey" /></el-form-item>
        <el-form-item :label="t('system.iam.common.name')" prop="name"><el-input v-model="form.name" /></el-form-item>
        <el-form-item :label="t('system.iam.common.description')"><el-input v-model="form.description" type="textarea" :rows="2" /></el-form-item>
        <el-form-item :label="t('system.iam.roles.scopes')" prop="scopeTypes">
          <el-checkbox-group v-model="form.scopeTypes">
            <el-checkbox v-for="scope in scopeOptions" :key="scope" :value="scope">{{ scopeLabel(scope) }}</el-checkbox>
          </el-checkbox-group>
        </el-form-item>
        <el-form-item :label="t('system.iam.roles.permissions')" prop="permissionKeys">
          <el-select v-model="form.permissionKeys" multiple filterable collapse-tags :max-collapse-tags="3" style="width: 100%">
            <el-option v-for="permission in permissions" :key="permission.permission_key" :label="permission.permission_key" :value="permission.permission_key" />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ t('system.iam.common.save') }}</el-button>
      </template>
    </el-dialog>
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Edit, Plus, Refresh } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import { resolveRoleDescription, resolveRoleName } from '../../utils/iamRoles'

const { t, te } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const scopeOptions = ['tenant', 'department', 'project_group']
const rows = ref([])
const permissions = ref([])
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const editing = ref(null)
const formRef = ref()
const form = reactive({ roleKey: '', name: '', description: '', scopeTypes: ['tenant'], permissionKeys: [] })
const rules = computed(() => ({
  roleKey: [{ required: !editing.value, message: t('system.iam.validation.required'), trigger: 'blur' }],
  name: [{ required: true, message: t('system.iam.validation.required'), trigger: 'blur' }],
  scopeTypes: [{ type: 'array', required: true, min: 1, message: t('system.iam.validation.required'), trigger: 'change' }],
  permissionKeys: [{ type: 'array', required: true, min: 1, message: t('system.iam.validation.required'), trigger: 'change' }]
}))

function roleName(role) {
  return resolveRoleName(role, t, te)
}
function roleDescription(role) { return resolveRoleDescription(role, t, te) }
function scopeLabel(scope) { return t(`system.iam.roles.scope.${scope}`) }
async function load() {
  loading.value = true
  try {
    const tasks = [iamAPI.tenantRoles.list()]
    if (can('iam.tenant_role.read')) tasks.push(iamAPI.tenantRoles.listAssignablePermissions())
    const [roleRows, permissionRows = []] = await Promise.all(tasks)
    rows.value = roleRows || []
    permissions.value = permissionRows || []
  } catch (error) {
    ElMessage.error(error.response?.data?.error || t('system.iam.common.loadFailed'))
  } finally {
    loading.value = false
  }
}
function openCreate() {
  editing.value = null
  Object.assign(form, { roleKey: '', name: '', description: '', scopeTypes: ['tenant'], permissionKeys: [] })
  dialogVisible.value = true
}
function openEdit(row) {
  editing.value = row
  Object.assign(form, { roleKey: row.role_key, name: row.name || '', description: row.description || '', scopeTypes: [...row.allowed_scope_types], permissionKeys: [...row.permission_keys] })
  dialogVisible.value = true
}
async function submit() {
  await formRef.value?.validate()
  submitting.value = true
  const payload = { role_key: form.roleKey, name: form.name, description: form.description, scope_types: form.scopeTypes, permission_keys: form.permissionKeys }
  try {
    if (editing.value) await iamAPI.tenantRoles.update(editing.value.id, payload)
    else await iamAPI.tenantRoles.create(payload)
    ElMessage.success(t('system.iam.common.saved'))
    dialogVisible.value = false
    await load()
  } catch (error) {
    if (error !== false) ElMessage.error(error.response?.data?.error || t('system.iam.common.saveFailed'))
  } finally {
    submitting.value = false
  }
}
async function removeRole(row) {
  try {
    const { value } = await ElMessageBox.prompt(t('system.iam.common.reasonPrompt'), t('system.iam.common.delete'), {
      inputValidator: (text) => Boolean(text?.trim()) || t('system.iam.validation.required'),
      confirmButtonText: t('system.iam.common.confirm'), cancelButtonText: t('system.iam.common.cancel'), type: 'warning'
    })
    await iamAPI.tenantRoles.remove(row.id, value.trim())
    ElMessage.success(t('system.iam.common.updated'))
    await load()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(error.response?.data?.error || t('system.iam.common.updateFailed'))
  }
}
onMounted(load)
</script>

<style scoped>
.iam-inline-tag { margin: 2px 6px 2px 0; }
.iam-list-text { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.5; }
</style>
