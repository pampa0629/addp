<template>
  <el-divider content-position="left">{{ t('security.roleOptions.title') }}</el-divider>
  <p class="field-help">{{ t('security.roleOptions.help') }}</p>
  <el-alert v-if="!canReadRoles" :title="t('security.roleOptions.noRoleRead')" type="info" :closable="false" />
  <el-alert v-else-if="loadError" :title="t('security.roleOptions.loadError')" type="error" :closable="false">
    <el-button link @click="loadRoles">{{ t('security.roleOptions.retry') }}</el-button>
  </el-alert>
  <section v-for="(option, index) in options" :key="index" class="role-option">
    <el-form-item :label="t('security.roleOptions.name')" required>
      <el-input v-model="option.name" maxlength="100" :disabled="!catalogReady" />
    </el-form-item>
    <el-form-item :label="t('security.roleOptions.roles')" required>
      <el-select v-model="option.role_keys" multiple filterable :disabled="!catalogReady" :loading="loading">
        <el-option v-for="role in roles" :key="role.role_key" :value="role.role_key" :label="resolveRoleName(role, t, te)" />
      </el-select>
      <div v-if="missingRoles(option).length" class="field-help" role="alert">{{ t('security.roleOptions.missingRoles', { roles: missingRoles(option).join(', ') }) }}</div>
    </el-form-item>
    <fieldset v-if="catalogReady">
      <ProtectionAlgorithmEditor :ref="element => { editors[index] = element }" :configuration="option.decision" :baseline="baseline" :field-type="fieldType" :path="`role_options.${index}.decision`" />
      <el-form-item :label="t('security.fields.invalid_value_effect')" required>
        <el-select v-model="option.decision.invalid_value_effect" :disabled="!catalogReady">
          <el-option v-if="baseline?.invalid_value_effect !== 'deny'" value="suppress" :label="t('security.options.effects.suppress')" />
          <el-option value="deny" :label="t('security.options.effects.deny')" />
        </el-select>
      </el-form-item>
    </fieldset>
    <p v-else class="field-help">{{ t('security.roleOptions.readOnlyMethod', { method: t(algorithmNameKey(option.decision.algorithm)), parameters: JSON.stringify(option.decision.parameters || {}), invalid: t(`security.options.effects.${option.decision.invalid_value_effect}`) }) }}</p>
    <el-button type="danger" link @click="options.splice(index, 1)">{{ t('security.roleOptions.remove') }}</el-button>
  </section>
  <el-alert v-if="invalid" :title="t('security.roleOptions.invalid')" type="error" :closable="false" />
  <el-button :disabled="!catalogReady || options.length >= 32" @click="add">{{ t('security.roleOptions.add') }}</el-button>
</template>

<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { resolveRoleName } from '@common-ui'
import { useAuthStore } from '../../store/auth'
import { policyRoleAPI } from '../../api/security'
import { algorithmNameKey } from '../../utils/protectionAlgorithm.mjs'
import ProtectionAlgorithmEditor from '../ProtectionAlgorithmEditor.vue'

const props = defineProps({ options: { type: Array, required: true }, baseline: Object, fieldType: String })
const { t, te } = useI18n()
const auth = useAuthStore()
const canReadRoles = computed(() => auth.hasPermission('iam.tenant_role.read'))
const roles = ref([])
const loading = ref(false)
const loadError = ref(false)
const loaded = ref(false)
const invalid = ref(false)
const editors = []
let generation = 0
const catalogReady = computed(() => canReadRoles.value && loaded.value && !loading.value && !loadError.value)
async function loadRoles() {
  const request = ++generation
  roles.value = []; loaded.value = false; loadError.value = false; loading.value = false
  if (!canReadRoles.value) return
  loading.value = true
  try {
    const result = await policyRoleAPI.list()
    if (request !== generation) return
    if (!Array.isArray(result)) throw new Error('Invalid role catalogue')
    roles.value = result.filter(role => role.allowed_principal_types?.includes('user') && role.allowed_scope_types?.includes('tenant'))
    loaded.value = true
  } catch { if (request === generation) loadError.value = true }
  finally { if (request === generation) loading.value = false }
}
function missingRoles(option) { return catalogReady.value ? option.role_keys.filter(key => !roles.value.some(role => role.role_key === key)) : [] }
function add() {
  if (!catalogReady.value || props.options.length >= 32) return
  props.options.push({ id: '', name: '', role_keys: [], decision: { effect: 'mask', algorithm: props.baseline.algorithm, parameters: JSON.parse(JSON.stringify(props.baseline.parameters || {})), invalid_value_effect: props.baseline.invalid_value_effect || 'suppress' } })
}
function validate() {
  if (!catalogReady.value) return true
  invalid.value = props.options.some((option, index) => !option.name.trim() || !option.role_keys.length || option.role_keys.length > 32 || missingRoles(option).length || !editors[index]?.validate())
  return !invalid.value
}
watch([canReadRoles, () => auth.authContext?.context?.tenant_id], loadRoles, { immediate: true })
onBeforeUnmount(() => { generation++ })
defineExpose({ validate })
</script>

<style scoped>
.role-option { margin: 12px 0; padding: 12px; border: 1px solid var(--addp-border-color); border-radius: 8px; }
.field-help { color: var(--addp-text-secondary); font-size: 12px; line-height: 1.6; }
fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
</style>
