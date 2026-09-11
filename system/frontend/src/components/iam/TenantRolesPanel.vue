<template>
  <section class="iam-panel">
    <div class="iam-role-audience">
      <div class="iam-role-audience__heading">
        <strong>{{ t('system.iam.roles.audience.title') }}</strong>
        <span>{{ audienceHint }}</span>
      </div>
      <el-radio-group v-model="filters.principalType" class="iam-role-audience__options">
        <el-radio-button value="user">
          {{ t('system.iam.roles.audience.user') }}
          <span class="iam-role-audience__count">{{ audienceCounts.user }}</span>
        </el-radio-button>
        <el-radio-button value="service_principal">
          {{ t('system.iam.roles.audience.service_principal') }}
          <span class="iam-role-audience__count">{{ audienceCounts.service_principal }}</span>
        </el-radio-button>
        <el-radio-button value="">
          {{ t('system.iam.roles.audience.all') }}
          <span class="iam-role-audience__count">{{ audienceCounts.all }}</span>
        </el-radio-button>
      </el-radio-group>
    </div>

    <div class="iam-toolbar">
      <div class="iam-filters">
        <el-input v-model="filters.search" :placeholder="t('system.iam.roles.search')" clearable :prefix-icon="Search" />
        <el-select v-model="filters.roleType" :placeholder="t('system.iam.roles.type')" clearable>
          <el-option v-for="roleType in roleTypes" :key="roleType" :label="t(`system.iam.roles.types.${roleType}`)" :value="roleType" />
        </el-select>
        <el-select v-model="filters.scopeType" :placeholder="t('system.iam.roles.scopes')" clearable>
          <el-option v-for="scope in scopeOptions" :key="scope" :label="scopeLabel(scope)" :value="scope" />
        </el-select>
        <el-button :icon="Refresh" @click="load">{{ t('system.iam.common.refresh') }}</el-button>
      </div>
      <el-button v-if="can('iam.tenant_role.create')" type="primary" :icon="Plus" @click="openCreate">
        {{ t('system.iam.roles.create') }}
      </el-button>
    </div>

    <el-table v-loading="loading" :data="filteredRows" stripe>
      <el-table-column :label="t('system.iam.roles.role')" min-width="300">
        <template #default="{ row }">
          <div class="iam-primary-cell">
            <el-button class="iam-role-name-button" link type="primary" @click="openRoleDetails(row)">
              <strong>{{ roleName(row) }}</strong>
            </el-button>
            <span>{{ row.role_key }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.roles.type')" width="150">
        <template #default="{ row }"><el-tag effect="plain">{{ t(`system.iam.roles.types.${row.role_type}`) }}</el-tag></template>
      </el-table-column>
      <el-table-column :label="t('system.iam.roles.applicableContext')" min-width="240">
        <template #default="{ row }">
          <div class="iam-context-tags">
            <div>
              <span class="iam-context-label">{{ t('system.iam.roles.scopes') }}</span>
              <el-tag v-for="scope in row.allowed_scope_types" :key="scope" class="iam-inline-tag" effect="plain">{{ scopeLabel(scope) }}</el-tag>
            </div>
            <div>
              <span class="iam-context-label">{{ t('system.iam.roles.applicableMembers') }}</span>
              <el-tag v-for="principalType in row.allowed_principal_types" :key="principalType" class="iam-inline-tag" type="info" effect="plain">{{ memberTypeLabel(principalType) }}</el-tag>
            </div>
          </div>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.roles.permissionOverview')" min-width="220">
        <template #default="{ row }">
          <el-button class="iam-permission-summary-button" link type="primary" :icon="View" @click="openRoleDetails(row)">
            <span class="iam-permission-summary">
              <strong>{{ t('system.iam.roles.permissionCount', { count: (row.permission_keys || []).length }) }}</strong>
              <span>{{ rolePermissionModuleSummary(row) }}</span>
            </span>
          </el-button>
        </template>
      </el-table-column>
      <el-table-column :label="t('system.iam.common.actions')" width="170" fixed="right">
        <template #default="{ row }">
          <el-button v-if="can('iam.tenant_role.update') && !row.immutable" link type="primary" :icon="Edit" @click="openEdit(row)">{{ t('system.iam.common.edit') }}</el-button>
          <el-button v-if="can('iam.tenant_role.delete') && !row.immutable" link type="danger" :icon="Delete" @click="removeRole(row)">{{ t('system.iam.common.delete') }}</el-button>
          <span v-if="row.immutable" class="iam-role-immutable-hint">{{ t('system.iam.roles.immutableHint') }}</span>
        </template>
      </el-table-column>
    </el-table>

    <el-dialog v-model="dialogVisible" class="addp-dialog" :title="editing ? t('system.iam.roles.edit') : t('system.iam.roles.create')" width="min(980px, calc(100vw - 24px))">
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
          <div class="iam-permission-picker">
            <div class="iam-permission-picker__toolbar">
              <el-input v-model="permissionSearch" :placeholder="t('system.iam.roles.searchPermissions')" clearable :prefix-icon="Search" />
              <span class="iam-role-selection-detail">{{ t('system.iam.roles.selectedPermissionCount', { count: form.permissionKeys.length }) }}</span>
              <el-button v-if="form.permissionKeys.length" link type="primary" @click="clearPermissionSelection">{{ t('system.iam.roles.clearSelection') }}</el-button>
            </div>
            <el-alert
              v-if="incompatiblePermissionKeys.length"
              class="iam-permission-picker__alert"
              type="warning"
              :closable="false"
              show-icon
              :title="t('system.iam.roles.scopeMismatch', { count: incompatiblePermissionKeys.length })"
            >
              <template #default>
                <el-button link type="warning" @click="clearIncompatiblePermissions">{{ t('system.iam.roles.removeIncompatible') }}</el-button>
              </template>
            </el-alert>
            <div v-if="permissionGroups.length" class="iam-permission-picker__body">
              <nav class="iam-permission-modules" :aria-label="t('system.iam.roles.permissionModules')">
                <button
                  v-for="group in permissionGroups"
                  :key="group.namespace"
                  type="button"
                  class="iam-permission-module"
                  :class="{ 'is-active': activePermissionNamespace === group.namespace }"
                  @click="activePermissionNamespace = group.namespace"
                >
                  <span>{{ moduleLabel(group.namespace) }}</span>
                  <small>{{ t('system.iam.roles.moduleSelection', { selected: group.selectedCount, total: group.permissions.length }) }}</small>
                </button>
              </nav>
              <div v-if="activePermissionGroup" class="iam-permission-list">
                <div class="iam-permission-list__heading">
                  <div>
                    <strong>{{ permissionGroupTitle(activePermissionGroup) }}</strong>
                    <span>{{ t('system.iam.roles.permissionPickerHint') }}</span>
                  </div>
                  <div>
                    <el-button link type="primary" @click="selectActivePermissionGroup">{{ t('system.iam.roles.selectVisible') }}</el-button>
                    <el-button link @click="clearActivePermissionGroup">{{ t('system.iam.roles.clearModule') }}</el-button>
                  </div>
                </div>
                <el-scrollbar max-height="360px">
                  <el-checkbox-group v-model="form.permissionKeys" class="iam-permission-options">
                    <section v-for="resourceGroup in activePermissionResourceGroups" :key="resourceGroup.resource" class="iam-permission-resource">
                      <div class="iam-permission-resource__heading">
                        <strong>{{ permissionResourceName(resourceGroup.permissions[0]) }}</strong>
                        <small>{{ t('system.iam.roles.moduleSelection', { selected: resourceGroup.selectedCount, total: resourceGroup.permissions.length }) }}</small>
                      </div>
                      <el-checkbox v-for="permission in resourceGroup.permissions" :key="permission.permission_key" :value="permission.permission_key" class="iam-permission-option">
                        <span class="iam-permission-option__content">
                          <span class="iam-permission-option__title">
                            <strong>{{ actionLabel(permissionIdentity(permission).action) }}</strong>
                            <el-tag size="small" effect="plain" :type="riskTagType(permission.risk_level)">{{ t('system.iam.roles.risk', { level: riskLabel(permission.risk_level) }) }}</el-tag>
                          </span>
                          <span class="iam-permission-option__key">{{ permission.permission_key }}</span>
                        </span>
                      </el-checkbox>
                    </section>
                  </el-checkbox-group>
                </el-scrollbar>
              </div>
            </div>
            <el-empty v-else :description="t('system.iam.roles.noMatchingPermissions')" :image-size="64" />
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="dialogVisible = false">{{ t('system.iam.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="submit">{{ t('system.iam.common.save') }}</el-button>
      </template>
    </el-dialog>

    <el-drawer v-model="roleDetailVisible" :title="t('system.iam.roles.details')" size="min(620px, 92vw)">
      <template v-if="roleDetailRole">
        <div class="iam-role-detail-heading">
          <div>
            <strong>{{ roleName(roleDetailRole) }}</strong>
            <span>{{ roleDetailRole.role_key }}</span>
          </div>
          <el-tag effect="plain">{{ t(`system.iam.roles.types.${roleDetailRole.role_type}`) }}</el-tag>
        </div>
        <p v-if="roleDescription(roleDetailRole)" class="iam-role-detail-description">{{ roleDescription(roleDetailRole) }}</p>
        <div class="iam-role-detail-context">
          <div>
            <span>{{ t('system.iam.roles.applicableMembers') }}</span>
            <div>
              <el-tag
                v-for="principalType in roleDetailRole.allowed_principal_types"
                :key="principalType"
                class="iam-inline-tag"
                type="info"
                effect="plain"
              >{{ memberTypeLabel(principalType) }}</el-tag>
            </div>
          </div>
          <div>
            <span>{{ t('system.iam.roles.scopes') }}</span>
            <div>
              <el-tag
                v-for="scope in roleDetailRole.allowed_scope_types"
                :key="scope"
                class="iam-inline-tag"
                effect="plain"
              >{{ scopeLabel(scope) }}</el-tag>
            </div>
          </div>
        </div>
        <div class="iam-role-detail-section-heading">
          <strong>{{ t('system.iam.roles.permissionDetails') }}</strong>
          <span>{{ t('system.iam.roles.permissionCount', { count: (roleDetailRole.permission_keys || []).length }) }}</span>
        </div>
        <div v-for="group in permissionDetailGroups" :key="group.namespace" class="iam-permission-group">
          <div class="iam-permission-group__heading">
            <strong>{{ permissionGroupTitle(group) }}</strong>
          </div>
          <div class="iam-permission-detail-list">
            <section v-for="resourceGroup in group.resources" :key="resourceGroup.resource" class="iam-permission-detail-resource">
              <div class="iam-permission-detail-resource__heading">
                <strong>{{ permissionResourceName(resourceGroup.permissions[0]) }}</strong>
                <span>{{ t('system.iam.roles.permissionCount', { count: resourceGroup.permissions.length }) }}</span>
              </div>
              <div v-for="permission in resourceGroup.permissions" :key="permission.permission_key" class="iam-permission-detail-row">
                <div>
                  <strong>{{ actionLabel(permissionIdentity(permission).action) }}</strong>
                  <span>{{ permission.permission_key }}</span>
                </div>
                <div class="iam-permission-detail-row__meta">
                  <el-tag v-if="permission.risk_level" size="small" effect="plain" :type="riskTagType(permission.risk_level)">{{ t('system.iam.roles.risk', { level: riskLabel(permission.risk_level) }) }}</el-tag>
                </div>
              </div>
            </section>
          </div>
        </div>
      </template>
    </el-drawer>
  </section>
</template>

<script setup>
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Delete, Edit, Plus, Refresh, Search, View } from '@element-plus/icons-vue'
import { useI18n } from 'vue-i18n'
import { iamAPI } from '../../api/iam'
import { useAuthStore } from '../../store/auth'
import { resolveRoleDescription, resolveRoleName } from '../../utils/iamRoles'
import {
  buildPermissionGroups,
  groupPermissionsByNamespace,
  groupPermissionsByResource,
  permissionIdentity,
  permissionMatchesScopes,
  permissionResourceI18nKey,
  rolePrincipalTypeCounts,
  roleSupportsPrincipalType,
  resolveIAMModuleName
} from '../../utils/iamPresentation'

const { t, te } = useI18n()
const authStore = useAuthStore()
const can = (permission) => authStore.hasPermission(permission)
const scopeOptions = ['tenant', 'department', 'project_group']
const roleTypes = ['tenant_builtin', 'tenant_custom']
const rows = ref([])
const permissions = ref([])
const loading = ref(false)
const submitting = ref(false)
const dialogVisible = ref(false)
const roleDetailVisible = ref(false)
const roleDetailRole = ref(null)
const editing = ref(null)
const formRef = ref()
const permissionSearch = ref('')
const activePermissionNamespace = ref('')
const filters = reactive({ search: '', roleType: '', principalType: 'user', scopeType: '' })
const form = reactive({ roleKey: '', name: '', description: '', scopeTypes: ['tenant'], permissionKeys: [] })
const audienceCounts = computed(() => rolePrincipalTypeCounts(rows.value))
const audienceHint = computed(() => t(`system.iam.roles.audience.hints.${filters.principalType || 'all'}`))
const filteredRows = computed(() => {
  const search = filters.search.trim().toLocaleLowerCase()
  return rows.value.filter((role) =>
    (!search || [roleName(role), role.role_key, roleDescription(role)].some((value) => String(value || '').toLocaleLowerCase().includes(search))) &&
    (!filters.roleType || role.role_type === filters.roleType) &&
    roleSupportsPrincipalType(role, filters.principalType) &&
    (!filters.scopeType || (role.allowed_scope_types || []).includes(filters.scopeType))
  )
})
const permissionCatalogByKey = computed(() => new Map(permissions.value.map((permission) => [permission.permission_key, permission])))
const permissionGroups = computed(() => buildPermissionGroups(permissions.value, {
  search: permissionSearch.value,
  scopeTypes: form.scopeTypes,
  selectedKeys: form.permissionKeys,
  getSearchValues: (permission) => [
    permissionDisplayName(permission),
    actionLabel(permissionIdentity(permission).action),
    moduleLabel(permissionIdentity(permission).ownerModule)
  ]
}))
const activePermissionGroup = computed(() => permissionGroups.value.find((group) => group.namespace === activePermissionNamespace.value) || permissionGroups.value[0] || null)
const activePermissionResourceGroups = computed(() => groupPermissionsByResource(
  activePermissionGroup.value?.permissions || [],
  form.permissionKeys
))
const incompatiblePermissionKeys = computed(() => form.permissionKeys.filter((permissionKey) => {
  const permission = permissionCatalogByKey.value.get(permissionKey)
  return permission && !permissionMatchesScopes(permission, form.scopeTypes)
}))
const permissionDetailGroups = computed(() => groupPermissionsByNamespace(
  (roleDetailRole.value?.permission_keys || []).map(permissionMetadata),
  (permission) => permissionIdentity(permission).ownerModule
).map((group) => ({
  ...group,
  resources: groupPermissionsByResource(group.permissions)
})))
const rules = computed(() => ({
  roleKey: [{ required: !editing.value, message: t('system.iam.validation.required'), trigger: 'blur' }],
  name: [{ required: true, message: t('system.iam.validation.required'), trigger: 'blur' }],
  scopeTypes: [{ type: 'array', required: true, min: 1, message: t('system.iam.validation.required'), trigger: 'change' }],
  permissionKeys: [
    { type: 'array', required: true, min: 1, message: t('system.iam.validation.required'), trigger: 'change' },
    {
      validator: (_rule, _value, callback) => incompatiblePermissionKeys.value.length
        ? callback(new Error(t('system.iam.roles.scopeMismatch', { count: incompatiblePermissionKeys.value.length })))
        : callback(),
      trigger: 'change'
    }
  ]
}))

watch(permissionGroups, (groups) => {
  if (!groups.some((group) => group.namespace === activePermissionNamespace.value)) {
    activePermissionNamespace.value = groups[0]?.namespace || ''
  }
}, { immediate: true })

function roleName(role) {
  return resolveRoleName(role, t, te)
}
function roleDescription(role) { return resolveRoleDescription(role, t, te) }
function scopeLabel(scope) { return t(`system.iam.roles.scope.${scope}`) }
function memberTypeLabel(principalType) { return t(`system.iam.principalType.${principalType}`) }
function moduleLabel(namespace) { return resolveIAMModuleName(namespace, t, te) }
function permissionGroupTitle(group) {
  return `${moduleLabel(group.namespace)} (${group.permissions.length})`
}
function rolePermissionModuleSummary(role) {
  const groups = groupPermissionsByNamespace(role?.permission_keys || [])
  const labels = groups.slice(0, 2).map((group) => moduleLabel(group.namespace))
  if (groups.length > 2) labels.push(`+${groups.length - 2}`)
  return labels.join(' · ') || '-'
}
function permissionMetadata(permissionKey) {
  const existing = permissionCatalogByKey.value.get(permissionKey)
  if (existing) return existing
  const identity = permissionIdentity(permissionKey)
  return {
    permission_key: identity.permissionKey,
    owner_module: identity.ownerModule,
    action: identity.action,
    risk_level: '',
    allowed_scope_types: []
  }
}
function permissionDisplayName(permission) {
  return t(permissionResourceI18nKey(permission))
}
function permissionResourceName(permission) {
  const key = permissionResourceI18nKey(permission)
  return te(key) ? t(key) : permissionIdentity(permission).resource || permissionIdentity(permission).permissionKey
}
function actionLabel(action) {
  const key = `system.iam.roles.actions.${action}`
  return action && te(key) ? t(key) : action || '-'
}
function riskLabel(risk) { return t(`system.iam.status.${risk}`) }
function riskTagType(risk) { return ({ low: 'info', medium: 'warning', high: 'danger', critical: 'danger' })[risk] || 'info' }
function clearPermissionSelection() { form.permissionKeys = [] }
function selectActivePermissionGroup() {
  const keys = activePermissionGroup.value?.permissions.map((permission) => permission.permission_key) || []
  form.permissionKeys = [...new Set([...form.permissionKeys, ...keys])].sort()
}
function clearActivePermissionGroup() {
  const keys = new Set(activePermissionGroup.value?.permissions.map((permission) => permission.permission_key) || [])
  form.permissionKeys = form.permissionKeys.filter((permissionKey) => !keys.has(permissionKey))
}
function clearIncompatiblePermissions() {
  const keys = new Set(incompatiblePermissionKeys.value)
  form.permissionKeys = form.permissionKeys.filter((permissionKey) => !keys.has(permissionKey))
}
function openRoleDetails(role) { roleDetailRole.value = role; roleDetailVisible.value = true }
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
  permissionSearch.value = ''
  Object.assign(form, { roleKey: '', name: '', description: '', scopeTypes: ['tenant'], permissionKeys: [] })
  dialogVisible.value = true
}
function openEdit(row) {
  editing.value = row
  permissionSearch.value = ''
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
.iam-role-audience { display: flex; margin-bottom: 16px; padding: 16px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); align-items: center; justify-content: space-between; gap: 16px; }
.iam-role-audience__heading { display: flex; min-width: 0; flex-direction: column; gap: 5px; }
.iam-role-audience__heading strong { color: var(--addp-text-primary); }
.iam-role-audience__heading span { color: var(--addp-text-secondary); font-size: 13px; line-height: 1.45; }
.iam-role-audience__options { flex-shrink: 0; }
.iam-role-audience__count { display: inline-flex; min-width: 22px; height: 18px; margin-left: 5px; padding: 0 5px; border-radius: 9px; background: var(--addp-bg-primary); align-items: center; justify-content: center; font-size: 12px; }
.iam-inline-tag { margin: 2px 6px 2px 0; }
.iam-role-name-button { display: inline-flex; width: fit-content; height: auto; padding: 0; font-size: inherit; }
.iam-context-tags { display: flex; flex-direction: column; gap: 6px; }
.iam-context-label { display: inline-block; min-width: 56px; margin-right: 6px; color: var(--addp-text-secondary); font-size: 12px; }
.iam-permission-summary-button { height: auto; max-width: 100%; padding: 4px 0; }
.iam-permission-summary { display: flex; min-width: 0; flex-direction: column; align-items: flex-start; gap: 3px; line-height: 1.35; }
.iam-permission-summary span { max-width: 190px; overflow: hidden; color: var(--addp-text-secondary); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.iam-role-selection-detail { color: var(--addp-text-secondary); font-size: 12px; white-space: nowrap; }
.iam-role-immutable-hint { color: var(--addp-text-secondary); font-size: 12px; }
.iam-permission-picker { width: 100%; overflow: hidden; border: 1px solid var(--addp-border-color); border-radius: 8px; }
.iam-permission-picker__toolbar { display: flex; align-items: center; gap: 12px; padding: 12px; border-bottom: 1px solid var(--addp-border-color); }
.iam-permission-picker__toolbar .el-input { flex: 1; }
.iam-permission-picker__alert { border-radius: 0; }
.iam-permission-picker__body { display: grid; grid-template-columns: minmax(180px, 220px) 1fr; min-height: 390px; }
.iam-permission-modules { padding: 8px; overflow-y: auto; border-right: 1px solid var(--addp-border-color); background: var(--addp-bg-secondary); }
.iam-permission-module { display: flex; width: 100%; padding: 10px 12px; border: 0; border-radius: 6px; background: transparent; color: var(--addp-text-primary); cursor: pointer; flex-direction: column; align-items: flex-start; gap: 4px; text-align: left; }
.iam-permission-module:hover { background: var(--addp-bg-secondary); }
.iam-permission-module.is-active { background: var(--el-color-primary-light-9); color: var(--el-color-primary); }
.iam-permission-module small { color: var(--addp-text-secondary); }
.iam-permission-list { min-width: 0; }
.iam-permission-list__heading { display: flex; min-height: 64px; padding: 10px 14px; border-bottom: 1px solid var(--addp-border-color); align-items: center; justify-content: space-between; gap: 12px; }
.iam-permission-list__heading > div:first-child { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.iam-permission-list__heading span { color: var(--addp-text-secondary); font-size: 12px; }
.iam-permission-options { display: flex; padding: 4px 14px 14px; flex-direction: column; }
.iam-permission-resource { padding: 12px 0 4px; border-bottom: 1px solid var(--addp-border-color); }
.iam-permission-resource:last-child { border-bottom: 0; }
.iam-permission-resource__heading { display: flex; padding: 0 0 6px; color: var(--addp-text-primary); align-items: center; justify-content: space-between; gap: 12px; font-size: 14px; line-height: 1.4; }
.iam-permission-resource__heading small { color: var(--addp-text-secondary); font-size: 12px; }
.iam-permission-option { width: 100%; height: auto; margin: 0; padding: 8px 0 8px 12px; align-items: flex-start; }
.iam-permission-option :deep(.el-checkbox__input) { margin-top: 3px; }
.iam-permission-option :deep(.el-checkbox__label) { min-width: 0; padding-left: 10px; white-space: normal; }
.iam-permission-option__content { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.iam-permission-option__title { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; color: var(--addp-text-primary); }
.iam-permission-option__key { color: var(--addp-text-secondary); font-family: var(--addp-font-family-mono, monospace); font-size: 12px; overflow-wrap: anywhere; }
.iam-role-detail-heading { display: flex; padding-bottom: 14px; border-bottom: 1px solid var(--addp-border-color); align-items: flex-start; justify-content: space-between; gap: 12px; }
.iam-role-detail-heading > div { display: flex; min-width: 0; flex-direction: column; gap: 4px; }
.iam-role-detail-heading strong { color: var(--addp-text-primary); font-size: 18px; }
.iam-role-detail-heading span { color: var(--addp-text-secondary); font-size: 12px; overflow-wrap: anywhere; }
.iam-role-detail-description { margin: 14px 0 0; color: var(--addp-text-secondary); line-height: 1.6; }
.iam-role-detail-context { display: grid; margin-top: 14px; padding: 12px 14px; border: 1px solid var(--addp-border-color); border-radius: 8px; background: var(--addp-bg-secondary); grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.iam-role-detail-context > div { display: flex; min-width: 0; flex-direction: column; gap: 6px; }
.iam-role-detail-context > div > span { color: var(--addp-text-secondary); font-size: 12px; }
.iam-role-detail-section-heading { display: flex; margin-top: 20px; padding-bottom: 10px; border-bottom: 1px solid var(--addp-border-color); align-items: center; justify-content: space-between; gap: 12px; }
.iam-role-detail-section-heading strong { color: var(--addp-text-primary); }
.iam-role-detail-section-heading span { color: var(--addp-text-secondary); font-size: 12px; }
.iam-permission-group { padding: 16px 0; border-bottom: 1px solid var(--addp-border-color); }
.iam-permission-group__heading { margin-bottom: 10px; color: var(--addp-text-primary); }
.iam-permission-detail-list { display: flex; flex-direction: column; }
.iam-permission-detail-resource { padding: 10px 12px; border-top: 1px solid var(--addp-border-color); background: var(--addp-bg-secondary); }
.iam-permission-detail-resource__heading { display: flex; padding-bottom: 4px; color: var(--addp-text-primary); align-items: center; justify-content: space-between; gap: 12px; }
.iam-permission-detail-resource__heading span { color: var(--addp-text-secondary); font-size: 12px; }
.iam-permission-detail-row { display: flex; padding: 10px 0; border-top: 1px solid var(--addp-border-color); align-items: center; justify-content: space-between; gap: 12px; }
.iam-permission-detail-row > div:first-child { display: flex; min-width: 0; flex-direction: column; gap: 3px; }
.iam-permission-detail-row span { color: var(--addp-text-secondary); font-family: var(--addp-font-family-mono, monospace); font-size: 12px; overflow-wrap: anywhere; }
.iam-permission-detail-row__meta { display: flex; flex-shrink: 0; gap: 6px; }
@media (max-width: 760px) {
  .iam-role-audience { align-items: stretch; flex-direction: column; }
  .iam-role-audience__options { display: flex; }
  .iam-role-audience__options :deep(.el-radio-button) { flex: 1; }
  .iam-role-audience__options :deep(.el-radio-button__inner) { width: 100%; padding-right: 8px; padding-left: 8px; }
  .iam-permission-picker__toolbar { align-items: stretch; flex-wrap: wrap; }
  .iam-permission-picker__body { display: flex; flex-direction: column; }
  .iam-permission-modules { display: flex; max-height: 150px; border-right: 0; border-bottom: 1px solid var(--addp-border-color); flex-wrap: wrap; }
  .iam-permission-module { width: auto; min-width: 150px; flex: 1; }
  .iam-permission-list__heading { align-items: flex-start; flex-direction: column; }
  .iam-role-detail-context { grid-template-columns: 1fr; }
}
</style>
