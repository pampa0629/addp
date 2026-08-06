<template>
  <main class="settings-page">
    <header class="page-header">
      <div>
        <h1>{{ t('inference.settings.title') }}</h1>
        <el-tag effect="plain">{{ scopeLabel }}</el-tag>
      </div>
      <div class="header-actions">
        <el-button :icon="Refresh" circle :loading="loading" :title="t('inference.common.refresh')" @click="loadAll" />
        <el-button v-if="canQuickConnect" type="primary" :icon="Plus" @click="openOnboarding">
          {{ t('inference.onboarding.open') }}
        </el-button>
      </div>
    </header>

    <el-tabs v-model="pageMode" class="page-tabs">
      <el-tab-pane :label="t('inference.services.title')" name="services">
        <el-empty v-if="!loading && providers.length === 0" :description="t('inference.services.empty')">
          <el-button v-if="canQuickConnect" type="primary" :icon="Plus" @click="openOnboarding">
            {{ t('inference.onboarding.open') }}
          </el-button>
        </el-empty>

        <div v-else v-loading="loading" class="service-grid">
          <article v-for="provider in providers" :key="provider.id" class="service-card">
            <header class="service-card__header">
              <div class="service-identity">
                <el-icon class="service-icon"><component :is="providerIcon(provider)" /></el-icon>
                <div class="service-title">
                  <h2>{{ provider.name }}</h2>
                  <span>{{ templateName(provider) }}</span>
                </div>
              </div>
              <el-tag :type="statusType(provider.status)" effect="plain">{{ t(`inference.status.${provider.status}`) }}</el-tag>
            </header>

            <dl class="service-facts">
              <div>
                <dt>{{ t('inference.provider.endpoint') }}</dt>
                <dd :title="provider.endpoint">{{ provider.endpoint }}</dd>
              </div>
              <div>
                <dt>{{ t('inference.provider.credential') }}</dt>
                <dd>
                  <el-tag :type="provider.credential?.configured ? 'success' : 'info'" effect="plain">
                    {{ provider.credential?.configured ? t('inference.provider.configured', { version: provider.credential.version }) : t('inference.provider.notConfigured') }}
                  </el-tag>
                </dd>
              </div>
              <div>
                <dt>{{ t('inference.services.models') }}</dt>
                <dd>{{ deploymentCount(provider.id) }}</dd>
              </div>
              <div>
                <dt>{{ t('inference.services.profiles') }}</dt>
                <dd>{{ profileCount(provider.id) }}</dd>
              </div>
            </dl>

            <footer class="service-actions">
              <el-button
                v-if="canManageProvider(provider) && can('inference.deployment.create') && can('inference.profile.create')"
                :icon="Plus"
                @click="openAddModels(provider)"
              >
                {{ t('inference.services.addModel') }}
              </el-button>
              <el-button
                v-if="canManageProvider(provider) && can('inference.provider_credential.update')"
                :icon="Key"
                @click="openCredential(provider)"
              >
                {{ t('inference.provider.setCredential') }}
              </el-button>
              <el-button :icon="Setting" @click="openProviderAdvanced">
                {{ t('inference.services.advanced') }}
              </el-button>
            </footer>
          </article>
        </div>
      </el-tab-pane>

      <el-tab-pane :label="t('inference.services.advanced')" name="advanced">
        <div class="advanced-toolbar">
          <span>{{ t('inference.services.advancedScope') }}</span>
          <el-button v-if="canCreateAdvanced" type="primary" :icon="Plus" @click="openCreate">
            {{ createLabel }}
          </el-button>
        </div>

        <el-tabs v-model="activeTab" class="resource-tabs">
          <el-tab-pane :label="t('inference.provider.title')" name="providers">
            <el-table v-loading="loading" :data="providers" row-key="id">
              <el-table-column prop="name" :label="t('inference.common.name')" min-width="150" />
              <el-table-column prop="scope_type" :label="t('inference.common.scope')" width="110">
                <template #default="{ row }">{{ scopeTypeLabel(row.scope_type) }}</template>
              </el-table-column>
              <el-table-column prop="adapter_type" :label="t('inference.provider.adapter')" min-width="170" />
              <el-table-column prop="endpoint" :label="t('inference.provider.endpoint')" min-width="260" show-overflow-tooltip />
              <el-table-column :label="t('inference.provider.credential')" width="150">
                <template #default="{ row }">
                  <el-tag :type="row.credential?.configured ? 'success' : 'info'" effect="plain">
                    {{ row.credential?.configured ? t('inference.provider.configured', { version: row.credential.version }) : t('inference.provider.notConfigured') }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column :label="t('inference.common.status')" width="105">
                <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ t(`inference.status.${row.status}`) }}</el-tag></template>
              </el-table-column>
              <el-table-column :label="t('inference.common.actions')" width="260" fixed="right">
                <template #default="{ row }">
                  <el-button v-if="canManageProvider(row) && can('inference.provider.update')" link type="primary" @click="openEditProvider(row)">{{ t('inference.common.edit') }}</el-button>
                  <el-button v-if="canManageProvider(row) && can('inference.provider_credential.update')" link type="primary" @click="openCredential(row)">{{ t('inference.provider.setCredential') }}</el-button>
                  <el-button v-if="canManageProvider(row) && can('inference.provider_credential.update') && row.credential?.configured" link type="warning" @click="removeCredential(row)">{{ t('inference.provider.deleteCredential') }}</el-button>
                  <el-button v-if="canManageProvider(row) && can('inference.provider.delete')" link type="danger" @click="removeProvider(row)">{{ t('inference.common.delete') }}</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="t('inference.deployment.title')" name="deployments">
            <el-table v-loading="loading" :data="deployments" row-key="id">
              <el-table-column prop="name" :label="t('inference.common.name')" min-width="150" />
              <el-table-column :label="t('inference.deployment.provider')" min-width="150">
                <template #default="{ row }">{{ providerName(row.provider_connection_id) }}</template>
              </el-table-column>
              <el-table-column prop="upstream_model" :label="t('inference.deployment.upstreamModel')" min-width="180" />
              <el-table-column :label="t('inference.deployment.operations')" min-width="200">
                <template #default="{ row }"><el-tag v-for="item in row.operations" :key="item" class="value-tag" effect="plain">{{ item }}</el-tag></template>
              </el-table-column>
              <el-table-column :label="t('inference.deployment.modalities')" min-width="130">
                <template #default="{ row }"><el-tag v-for="item in row.modalities" :key="item" class="value-tag" type="info" effect="plain">{{ item }}</el-tag></template>
              </el-table-column>
              <el-table-column prop="dimension" :label="t('inference.deployment.dimension')" width="105" />
              <el-table-column :label="t('inference.common.status')" width="105">
                <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ t(`inference.status.${row.status}`) }}</el-tag></template>
              </el-table-column>
              <el-table-column :label="t('inference.common.actions')" width="190" fixed="right">
                <template #default="{ row }">
                  <el-button v-if="canManageDeployment(row) && can('inference.deployment.execute')" link type="success" @click="probe(row)">{{ t('inference.deployment.probe') }}</el-button>
                  <el-button v-if="canManageDeployment(row) && can('inference.deployment.update')" link type="primary" @click="openEditDeployment(row)">{{ t('inference.common.edit') }}</el-button>
                  <el-button v-if="canManageDeployment(row) && can('inference.deployment.delete')" link type="danger" @click="removeDeployment(row)">{{ t('inference.common.delete') }}</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>

          <el-tab-pane :label="t('inference.profile.title')" name="profiles">
            <el-table v-loading="loading" :data="profiles" row-key="id">
              <el-table-column prop="name" :label="t('inference.common.name')" min-width="160" />
              <el-table-column prop="code" :label="t('inference.profile.code')" min-width="150" />
              <el-table-column prop="scope_type" :label="t('inference.common.scope')" width="110">
                <template #default="{ row }">{{ scopeTypeLabel(row.scope_type) }}</template>
              </el-table-column>
              <el-table-column :label="t('inference.profile.deployment')" min-width="200">
                <template #default="{ row }">{{ deploymentName(row.model_deployment_id) }}</template>
              </el-table-column>
              <el-table-column prop="version" :label="t('inference.common.version')" width="90" />
              <el-table-column :label="t('inference.common.status')" width="105">
                <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ t(`inference.status.${row.status}`) }}</el-tag></template>
              </el-table-column>
              <el-table-column :label="t('inference.common.actions')" width="110" fixed="right">
                <template #default="{ row }">
                  <el-button v-if="canManageProfile(row) && can('inference.profile.update')" link type="primary" @click="openEditProfile(row)">{{ t('inference.common.edit') }}</el-button>
                </template>
              </el-table-column>
            </el-table>
          </el-tab-pane>
        </el-tabs>
      </el-tab-pane>
    </el-tabs>

    <el-dialog v-model="onboarding.visible" :title="onboardingTitle" width="min(920px, calc(100vw - 32px))" destroy-on-close :close-on-click-modal="false">
      <el-steps :active="onboarding.step" finish-status="success" simple class="onboarding-steps">
        <el-step :title="t('inference.onboarding.steps.template')" />
        <el-step :title="t('inference.onboarding.steps.connection')" />
        <el-step :title="t('inference.onboarding.steps.models')" />
        <el-step :title="t('inference.onboarding.steps.complete')" />
      </el-steps>
      <div class="mobile-step-label">
        {{ t('inference.onboarding.mobileStep', { current: onboarding.step + 1, title: currentStepTitle }) }}
      </div>

      <section v-if="onboarding.step === 0" class="onboarding-panel">
        <el-tabs v-model="templateCategory">
          <el-tab-pane v-for="category in templateCategories" :key="category" :label="t(`inference.templates.categories.${category}`)" :name="category">
            <div class="template-grid">
              <article
                v-for="template in templatesByCategory(category)"
                :key="template.code"
                class="template-card"
                :class="{ 'is-selected': onboarding.templateCode === template.code }"
                role="radio"
                :aria-checked="onboarding.templateCode === template.code"
                tabindex="0"
                @click="selectTemplate(template)"
                @keydown.enter.prevent="selectTemplate(template)"
              >
                <el-icon><component :is="templateIcon(template)" /></el-icon>
                <div>
                  <h3>{{ t(`inference.templates.items.${template.code}.name`) }}</h3>
                  <p>{{ t(`inference.templates.items.${template.code}.description`) }}</p>
                </div>
                <el-button
                  v-if="template.documentation_url"
                  class="template-doc"
                  :icon="Link"
                  text
                  circle
                  :title="t('inference.templates.documentation')"
                  @click.stop="openDocumentation(template.documentation_url)"
                />
              </article>
            </div>
          </el-tab-pane>
        </el-tabs>
      </section>

      <section v-else-if="onboarding.step === 1" class="onboarding-panel">
        <el-alert v-if="selectedTemplate?.category === 'local'" type="warning" :closable="false" show-icon :title="t('inference.onboarding.localEndpointNotice')" />
        <el-form label-position="top" class="connection-form">
          <div class="form-grid">
            <el-form-item :label="t('inference.common.name')" required>
              <el-input v-model="onboarding.providerName" />
            </el-form-item>
            <el-form-item :label="t('inference.common.scope')">
              <el-input :model-value="scopeLabel" disabled />
            </el-form-item>
          </div>
          <el-form-item :label="t('inference.provider.endpoint')" required>
            <el-input v-model="onboarding.endpoint" :disabled="!selectedTemplate?.endpoint_editable" />
          </el-form-item>
          <el-form-item :label="t('inference.provider.newCredential')" :required="selectedTemplate?.credential_mode === 'required'">
            <el-input v-model="onboarding.credential" type="password" show-password autocomplete="new-password" />
          </el-form-item>
          <template v-if="contextType === 'platform'">
            <el-form-item :label="t('inference.provider.allowAllTenants')">
              <el-switch v-model="onboarding.allowAllTenants" />
            </el-form-item>
            <el-form-item v-if="!onboarding.allowAllTenants" :label="t('inference.provider.allowedTenantIds')" required>
              <el-input v-model="onboarding.allowedTenantIDsText" :placeholder="t('inference.provider.allowedTenantIdsPlaceholder')" />
            </el-form-item>
          </template>
        </el-form>
      </section>

      <section v-else-if="onboarding.step === 2" class="onboarding-panel">
        <el-alert
          :type="onboarding.discoverySucceeded ? 'success' : 'warning'"
          :closable="false"
          show-icon
          :title="onboarding.discoverySucceeded
            ? t('inference.onboarding.discoverySuccess', { count: onboarding.availableModels.length })
            : t('inference.onboarding.discoveryManual')"
        />

        <div class="model-editor">
          <div class="model-editor__grid">
            <el-form-item :label="t('inference.deployment.upstreamModel')" required>
              <el-select v-model="onboarding.modelDraft.upstreamModel" filterable allow-create default-first-option>
                <el-option v-for="model in onboarding.availableModels" :key="model" :label="model" :value="model" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('inference.onboarding.purpose')" required>
              <el-select v-model="onboarding.modelDraft.preset" @change="changeDraftPreset">
                <el-option v-for="preset in presetCodes" :key="preset" :label="t(`inference.onboarding.presets.${preset}`)" :value="preset" />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('inference.profile.code')" required>
              <el-input v-model="onboarding.modelDraft.profileCode" />
            </el-form-item>
            <el-form-item v-if="draftCapability.operations.includes('embedding')" :label="t('inference.deployment.dimension')" required>
              <el-input-number v-model="onboarding.modelDraft.dimension" :min="1" controls-position="right" />
            </el-form-item>
          </div>
          <el-button :icon="Plus" @click="addSelectedModel">{{ t('inference.onboarding.addModel') }}</el-button>
        </div>

        <el-table :data="onboarding.selectedModels" row-key="upstreamModel" empty-text="">
          <el-table-column prop="upstreamModel" :label="t('inference.deployment.upstreamModel')" min-width="220" />
          <el-table-column :label="t('inference.onboarding.purpose')" min-width="180">
            <template #default="{ row }">{{ t(`inference.onboarding.presets.${row.preset}`) }}</template>
          </el-table-column>
          <el-table-column prop="profileCode" :label="t('inference.profile.code')" min-width="170" />
          <el-table-column prop="dimension" :label="t('inference.deployment.dimension')" width="110" />
          <el-table-column width="80" align="right">
            <template #default="{ row }">
              <el-button :icon="Delete" text circle type="danger" :title="t('inference.common.delete')" @click="removeSelectedModel(row)" />
            </template>
          </el-table-column>
        </el-table>
      </section>

      <section v-else class="onboarding-panel onboarding-complete">
        <el-result icon="success" :title="t('inference.onboarding.completeTitle')" :sub-title="t('inference.onboarding.completeSubtitle')" />
        <el-table :data="onboarding.createdResources" row-key="profileCode">
          <el-table-column prop="upstreamModel" :label="t('inference.deployment.upstreamModel')" min-width="240" />
          <el-table-column prop="profileCode" :label="t('inference.profile.code')" min-width="180" />
        </el-table>
      </section>

      <template #footer>
        <div class="onboarding-footer">
          <el-button @click="onboarding.visible = false">{{ onboarding.step === 3 ? t('inference.common.close') : t('inference.common.cancel') }}</el-button>
          <div v-if="onboarding.step < 3" class="onboarding-footer__actions">
            <el-button v-if="onboarding.step > 0 && !onboarding.providerId" @click="onboarding.step--">{{ t('inference.common.previous') }}</el-button>
            <el-button v-if="onboarding.step === 0" type="primary" :disabled="!selectedTemplate" @click="onboarding.step = 1">{{ t('inference.common.next') }}</el-button>
            <el-button v-else-if="onboarding.step === 1" type="primary" :loading="onboarding.processing" @click="connectProvider">{{ t('inference.onboarding.connect') }}</el-button>
            <el-button v-else type="primary" :loading="onboarding.processing" :disabled="onboarding.selectedModels.length === 0" @click="createSelectedModels">{{ t('inference.onboarding.finish') }}</el-button>
          </div>
        </div>
      </template>
    </el-dialog>

    <el-dialog v-model="dialog.visible" :title="dialogTitle" width="min(620px, calc(100vw - 32px))" destroy-on-close>
      <el-form ref="formRef" :model="form" label-position="top">
        <template v-if="dialog.kind === 'provider'">
          <el-form-item :label="t('inference.common.name')" required><el-input v-model="form.name" /></el-form-item>
          <div class="form-grid">
            <el-form-item :label="t('inference.common.scope')"><el-input :model-value="scopeLabel" disabled /></el-form-item>
            <el-form-item :label="t('inference.provider.adapter')" required>
              <el-select v-model="form.adapter_type"><el-option label="OpenAI Compatible" value="openai_compatible" /><el-option label="DashScope Multimodal" value="dashscope_multimodal" /></el-select>
            </el-form-item>
          </div>
          <el-form-item :label="t('inference.provider.endpoint')" required><el-input v-model="form.endpoint" placeholder="https://example.com/v1" /></el-form-item>
          <template v-if="contextType === 'platform'">
            <el-form-item :label="t('inference.provider.allowAllTenants')"><el-switch v-model="form.allow_all_tenants" /></el-form-item>
            <el-form-item v-if="!form.allow_all_tenants" :label="t('inference.provider.allowedTenantIds')"><el-input v-model="form.allowed_tenant_ids_text" /></el-form-item>
          </template>
          <el-form-item :label="t('inference.common.status')"><el-select v-model="form.status"><el-option :label="t('inference.status.active')" value="active" /><el-option :label="t('inference.status.disabled')" value="disabled" /></el-select></el-form-item>
        </template>

        <template v-else-if="dialog.kind === 'deployment'">
          <el-form-item :label="t('inference.deployment.provider')" required><el-select v-model="form.provider_connection_id" :disabled="dialog.editing"><el-option v-for="provider in manageableProviders" :key="provider.id" :label="provider.name" :value="provider.id" /></el-select></el-form-item>
          <div class="form-grid">
            <el-form-item :label="t('inference.common.name')" required><el-input v-model="form.name" /></el-form-item>
            <el-form-item :label="t('inference.deployment.upstreamModel')" required><el-input v-model="form.upstream_model" /></el-form-item>
          </div>
          <el-form-item :label="t('inference.deployment.operations')" required><el-checkbox-group v-model="form.operations"><el-checkbox value="chat">chat</el-checkbox><el-checkbox value="embedding">embedding</el-checkbox><el-checkbox value="rerank">rerank</el-checkbox></el-checkbox-group></el-form-item>
          <el-form-item :label="t('inference.deployment.modalities')" required><el-checkbox-group v-model="form.modalities"><el-checkbox value="text">text</el-checkbox><el-checkbox value="image">image</el-checkbox></el-checkbox-group></el-form-item>
          <el-form-item v-if="form.operations.includes('embedding')" :label="t('inference.deployment.dimension')"><el-input-number v-model="form.dimension" :min="0" /></el-form-item>
          <el-form-item :label="t('inference.common.status')"><el-select v-model="form.status"><el-option :label="t('inference.status.active')" value="active" /><el-option :label="t('inference.status.disabled')" value="disabled" /></el-select></el-form-item>
        </template>

        <template v-else-if="dialog.kind === 'profile'">
          <div class="form-grid">
            <el-form-item :label="t('inference.common.name')" required><el-input v-model="form.name" /></el-form-item>
            <el-form-item :label="t('inference.profile.code')" required><el-input v-model="form.code" :disabled="dialog.editing" /></el-form-item>
          </div>
          <el-form-item :label="t('inference.profile.deployment')" required><el-select v-model="form.model_deployment_id"><el-option v-for="deployment in deployments" :key="deployment.id" :label="deployment.name" :value="deployment.id" /></el-select></el-form-item>
          <el-form-item :label="t('inference.common.status')"><el-select v-model="form.status"><el-option :label="t('inference.status.active')" value="active" /><el-option :label="t('inference.status.disabled')" value="disabled" /></el-select></el-form-item>
        </template>

        <template v-else>
          <el-form-item :label="t('inference.provider.newCredential')" required><el-input v-model="form.credential" type="password" show-password autocomplete="new-password" /></el-form-item>
        </template>
      </el-form>
      <template #footer>
        <el-button @click="dialog.visible = false">{{ t('inference.common.cancel') }}</el-button>
        <el-button type="primary" :loading="submitting" @click="save">{{ t('inference.common.save') }}</el-button>
      </template>
    </el-dialog>
  </main>
</template>

<script setup>
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Cloudy, Connection, Cpu, Delete, Key, Link, Plus, Refresh, Setting } from '@element-plus/icons-vue'
import { deploymentAPI, profileAPI, providerAPI, providerTemplateAPI } from '../api/inference'
import { useAuthStore } from '../store/auth'
import {
  CAPABILITY_PRESETS,
  applyPreset,
  capabilityPreset,
  createModelDraft,
  isValidProfileCode,
  modelOptions,
  parseTenantIDs
} from '../utils/modelOnboarding'

const { t } = useI18n()
const authStore = useAuthStore()
const pageMode = ref('services')
const activeTab = ref('providers')
const templateCategory = ref('cloud')
const loading = ref(false)
const submitting = ref(false)
const templates = ref([])
const providers = ref([])
const deployments = ref([])
const profiles = ref([])
const formRef = ref()
const form = reactive({})
const dialog = reactive({ visible: false, kind: 'provider', editing: false, id: null })
const onboarding = reactive({
  visible: false,
  step: 0,
  processing: false,
  templateCode: '',
  providerId: '',
  providerName: '',
  endpoint: '',
  credential: '',
  allowAllTenants: true,
  allowedTenantIDsText: '',
  discoverySucceeded: false,
  availableModels: [],
  modelDraft: createModelDraft(),
  selectedModels: [],
  createdResources: []
})

const contextType = computed(() => authStore.contextType || 'tenant')
const scopeLabel = computed(() => t(`inference.scope.${contextType.value}`))
const manageableProviders = computed(() => providers.value.filter(item => item.scope_type === contextType.value))
const selectedTemplate = computed(() => templates.value.find(item => item.code === onboarding.templateCode))
const draftCapability = computed(() => capabilityPreset(onboarding.modelDraft.preset))
const presetCodes = Object.keys(CAPABILITY_PRESETS)
const templateCategories = ['cloud', 'local', 'custom']
const createPermissions = { providers: 'inference.provider.create', deployments: 'inference.deployment.create', profiles: 'inference.profile.create' }
const createKeys = { providers: 'inference.provider.create', deployments: 'inference.deployment.create', profiles: 'inference.profile.create' }
const canCreateAdvanced = computed(() => can(createPermissions[activeTab.value]))
const createLabel = computed(() => t(createKeys[activeTab.value]))
const canQuickConnect = computed(() => ['inference.provider.create', 'inference.deployment.create', 'inference.profile.create'].every(can))
const dialogTitle = computed(() => dialog.kind === 'credential'
  ? t('inference.provider.credentialDialog')
  : t(`inference.${dialog.kind}.${dialog.editing ? 'edit' : 'create'}`))
const onboardingTitle = computed(() => onboarding.providerId && onboarding.step === 2
  ? t('inference.onboarding.addModelsTitle')
  : t('inference.onboarding.title'))
const currentStepTitle = computed(() => t(`inference.onboarding.steps.${['template', 'connection', 'models', 'complete'][onboarding.step]}`))

function can(permission) { return authStore.hasPermission(permission) }
function canManageProvider(row) { return row.scope_type === contextType.value }
function canManageDeployment(row) { return canManageProvider(providers.value.find(item => item.id === row.provider_connection_id) || {}) }
function canManageProfile(row) { return row.scope_type === contextType.value }
function scopeTypeLabel(scopeType) { return ['platform', 'tenant'].includes(scopeType) ? t(`inference.scope.${scopeType}`) : '-' }
function statusType(status) { return status === 'active' ? 'success' : 'info' }
function providerName(id) { return providers.value.find(item => item.id === id)?.name || id }
function deploymentName(id) { return deployments.value.find(item => item.id === id)?.name || id }
function errorMessage(error) { return error?.response?.data?.error || error?.message || t('inference.common.failed') }
function deploymentCount(providerId) { return deployments.value.filter(item => item.provider_connection_id === providerId).length }
function profileCount(providerId) {
  const ids = new Set(deployments.value.filter(item => item.provider_connection_id === providerId).map(item => item.id))
  return profiles.value.filter(item => ids.has(item.model_deployment_id)).length
}
function templatesByCategory(category) { return templates.value.filter(item => item.category === category) }
function templateForProvider(provider) {
  return templates.value.find(item => item.adapter_type === provider.adapter_type && item.default_endpoint === provider.endpoint)
    || templates.value.find(item => item.code === 'custom-openai-compatible')
}
function templateName(provider) {
  const template = templateForProvider(provider)
  return template ? t(`inference.templates.items.${template.code}.name`) : provider.adapter_type
}
function templateIcon(template) { return template.category === 'cloud' ? Cloudy : template.category === 'local' ? Cpu : Connection }
function providerIcon(provider) { return templateIcon(templateForProvider(provider) || { category: 'custom' }) }

async function loadAll() {
  loading.value = true
  try {
    const [templateValues, providerPage, deploymentPage, profilePage] = await Promise.all([
      providerTemplateAPI.list(),
      providerAPI.list({ page: 1, page_size: 100 }),
      deploymentAPI.list({ page: 1, page_size: 100 }),
      profileAPI.list({ page: 1, page_size: 100 })
    ])
    templates.value = Array.isArray(templateValues) ? templateValues : []
    providers.value = providerPage.data || []
    deployments.value = deploymentPage.data || []
    profiles.value = profilePage.data || []
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    loading.value = false
  }
}

function resetOnboarding() {
  Object.assign(onboarding, {
    visible: true,
    step: 0,
    processing: false,
    templateCode: '',
    providerId: '',
    providerName: '',
    endpoint: '',
    credential: '',
    allowAllTenants: true,
    allowedTenantIDsText: '',
    discoverySucceeded: false,
    availableModels: [],
    modelDraft: createModelDraft(),
    selectedModels: [],
    createdResources: []
  })
  templateCategory.value = 'cloud'
}

function openOnboarding() { resetOnboarding() }

function selectTemplate(template) {
  onboarding.templateCode = template.code
  onboarding.providerName = t(`inference.templates.items.${template.code}.name`)
  onboarding.endpoint = template.default_endpoint || ''
}

function openDocumentation(url) { window.open(url, '_blank', 'noopener,noreferrer') }

async function openAddModels(provider) {
  resetOnboarding()
  const template = templateForProvider(provider)
  onboarding.templateCode = template?.code || ''
  onboarding.providerId = provider.id
  onboarding.providerName = provider.name
  onboarding.endpoint = provider.endpoint
  onboarding.allowAllTenants = provider.allow_all_tenants
  onboarding.allowedTenantIDsText = (provider.allowed_tenant_ids || []).join(',')
  onboarding.step = 2
  await discoverProviderModels(provider.id, template)
}

function openProviderAdvanced() {
  pageMode.value = 'advanced'
  activeTab.value = 'providers'
}

function providerPayloadFromOnboarding() {
  const allowedTenantIDs = parseTenantIDs(onboarding.allowedTenantIDsText)
  return {
    name: onboarding.providerName.trim(),
    scope_type: contextType.value,
    adapter_type: selectedTemplate.value.adapter_type,
    endpoint: onboarding.endpoint.trim(),
    allow_all_tenants: contextType.value === 'platform' && onboarding.allowAllTenants,
    allowed_tenant_ids: contextType.value === 'platform' && !onboarding.allowAllTenants ? allowedTenantIDs : [],
    status: 'active'
  }
}

async function connectProvider() {
  const template = selectedTemplate.value
  if (!template || !onboarding.providerName.trim() || !onboarding.endpoint.trim()) {
    ElMessage.warning(t('inference.onboarding.connectionRequired'))
    return
  }
  if (template.credential_mode === 'required' && !onboarding.credential.trim()) {
    ElMessage.warning(t('inference.provider.credentialRequired'))
    return
  }
  if (contextType.value === 'platform' && !onboarding.allowAllTenants && parseTenantIDs(onboarding.allowedTenantIDsText).length === 0) {
    ElMessage.warning(t('inference.provider.allowedTenantIdsRequired'))
    return
  }
  onboarding.processing = true
  try {
    const provider = onboarding.providerId
      ? await providerAPI.update(onboarding.providerId, providerPayloadFromOnboarding())
      : await providerAPI.create(providerPayloadFromOnboarding())
    onboarding.providerId = provider.id
    if (onboarding.credential.trim()) {
      await providerAPI.setCredential(provider.id, onboarding.credential.trim())
      onboarding.credential = ''
    }
    onboarding.step = 2
    await discoverProviderModels(provider.id, template)
    await loadAll()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    onboarding.processing = false
  }
}

async function discoverProviderModels(providerId, template) {
  onboarding.discoverySucceeded = false
  let discovered = []
  if (template?.model_discovery === 'openai_models') {
    try {
      const response = await providerAPI.discoverModels(providerId)
      discovered = response.models || []
      onboarding.discoverySucceeded = true
    } catch (error) {
      ElMessage.warning(t('inference.onboarding.discoveryFailed', { error: errorMessage(error) }))
    }
  }
  const suggested = template?.suggested_models || []
  onboarding.availableModels = modelOptions(discovered, suggested)
  onboarding.selectedModels = suggested.map(item => ({
    upstreamModel: item.upstream_model,
    preset: item.capability_preset,
    profileCode: item.profile_code,
    dimension: item.dimension || 0
  }))
  onboarding.modelDraft = createModelDraft()
}

function changeDraftPreset(preset) {
  onboarding.modelDraft = applyPreset(onboarding.modelDraft, preset)
}

function addSelectedModel() {
  const draft = onboarding.modelDraft
  const upstreamModel = draft.upstreamModel.trim()
  const profileCode = draft.profileCode.trim()
  if (!upstreamModel || !isValidProfileCode(profileCode)) {
    ElMessage.warning(t('inference.onboarding.modelRequired'))
    return
  }
  if (draftCapability.value.operations.includes('embedding') && (!Number.isInteger(draft.dimension) || draft.dimension <= 0)) {
    ElMessage.warning(t('inference.onboarding.dimensionRequired'))
    return
  }
  if (onboarding.selectedModels.some(item => item.upstreamModel === upstreamModel || item.profileCode === profileCode)) {
    ElMessage.warning(t('inference.onboarding.duplicateModel'))
    return
  }
  onboarding.selectedModels.push({
    upstreamModel,
    preset: draft.preset,
    profileCode,
    dimension: draftCapability.value.operations.includes('embedding') ? draft.dimension : 0
  })
  onboarding.modelDraft = createModelDraft()
}

function removeSelectedModel(row) {
  onboarding.selectedModels = onboarding.selectedModels.filter(item => item !== row)
}

async function createSelectedModels() {
  const profileCodes = new Set()
  for (const item of onboarding.selectedModels) {
    if (!isValidProfileCode(item.profileCode) || profileCodes.has(item.profileCode)) {
      ElMessage.warning(t('inference.onboarding.duplicateModel'))
      return
    }
    profileCodes.add(item.profileCode)
    const existing = profiles.value.find(profile => profile.scope_type === contextType.value && profile.code === item.profileCode)
    const existingDeployment = existing ? deployments.value.find(deployment => deployment.id === existing.model_deployment_id) : null
    const isSameResource = existingDeployment?.provider_connection_id === onboarding.providerId && existingDeployment?.upstream_model === item.upstreamModel
    if (existing && !isSameResource) {
      ElMessage.warning(t('inference.onboarding.profileExists', { code: item.profileCode }))
      return
    }
  }

  onboarding.processing = true
  onboarding.createdResources = []
  try {
    for (const item of onboarding.selectedModels) {
      const capability = capabilityPreset(item.preset)
      let deployment = deployments.value.find(value => value.provider_connection_id === onboarding.providerId && value.upstream_model === item.upstreamModel)
      if (!deployment) {
        deployment = await deploymentAPI.create({
          provider_connection_id: onboarding.providerId,
          name: item.upstreamModel,
          upstream_model: item.upstreamModel,
          operations: capability.operations,
          modalities: capability.modalities,
          dimension: capability.operations.includes('embedding') ? item.dimension : 0,
          status: 'active'
        })
      }
      const existingProfile = profiles.value.find(profile => profile.scope_type === contextType.value && profile.code === item.profileCode)
      if (!existingProfile) {
        await profileAPI.create({
          name: `${item.upstreamModel} - ${t(`inference.onboarding.presets.${item.preset}`)}`,
          code: item.profileCode,
          scope_type: contextType.value,
          model_deployment_id: deployment.id,
          status: 'active'
        })
      }
      onboarding.createdResources.push({ upstreamModel: item.upstreamModel, profileCode: item.profileCode })
    }
    onboarding.step = 3
    await loadAll()
  } catch (error) {
    await loadAll()
    ElMessage.error(errorMessage(error))
  } finally {
    onboarding.processing = false
  }
}

function assignForm(value) {
  for (const key of Object.keys(form)) delete form[key]
  Object.assign(form, value)
}

function openCreate() {
  const kind = activeTab.value === 'providers' ? 'provider' : activeTab.value === 'deployments' ? 'deployment' : 'profile'
  Object.assign(dialog, { kind, editing: false, id: null, visible: true })
  if (kind === 'provider') assignForm({ name: '', scope_type: contextType.value, adapter_type: 'openai_compatible', endpoint: '', allow_all_tenants: contextType.value === 'platform', allowed_tenant_ids_text: '', status: 'active' })
  if (kind === 'deployment') assignForm({ provider_connection_id: manageableProviders.value[0]?.id || '', name: '', upstream_model: '', operations: ['chat'], modalities: ['text'], dimension: 0, status: 'active' })
  if (kind === 'profile') assignForm({ name: '', code: '', scope_type: contextType.value, model_deployment_id: deployments.value[0]?.id || '', status: 'active' })
}

function openEditProvider(row) {
  Object.assign(dialog, { kind: 'provider', editing: true, id: row.id, visible: true })
  assignForm({ name: row.name, scope_type: row.scope_type, adapter_type: row.adapter_type, endpoint: row.endpoint, allow_all_tenants: row.allow_all_tenants, allowed_tenant_ids_text: (row.allowed_tenant_ids || []).join(','), status: row.status })
}

function openEditDeployment(row) {
  Object.assign(dialog, { kind: 'deployment', editing: true, id: row.id, visible: true })
  assignForm({ provider_connection_id: row.provider_connection_id, name: row.name, upstream_model: row.upstream_model, operations: [...(row.operations || [])], modalities: [...(row.modalities || [])], dimension: row.dimension || 0, status: row.status })
}

function openEditProfile(row) {
  Object.assign(dialog, { kind: 'profile', editing: true, id: row.id, visible: true })
  assignForm({ name: row.name, code: row.code, scope_type: row.scope_type, model_deployment_id: row.model_deployment_id, status: row.status })
}

function openCredential(row) {
  Object.assign(dialog, { kind: 'credential', editing: true, id: row.id, visible: true })
  assignForm({ credential: '' })
}

function providerPayload() {
  return {
    name: form.name,
    scope_type: form.scope_type,
    adapter_type: form.adapter_type,
    endpoint: form.endpoint,
    allow_all_tenants: form.allow_all_tenants,
    allowed_tenant_ids: parseTenantIDs(form.allowed_tenant_ids_text),
    status: form.status
  }
}

async function save() {
  submitting.value = true
  try {
    if (dialog.kind === 'credential') {
      if (!form.credential?.trim()) throw new Error(t('inference.provider.credentialRequired'))
      await providerAPI.setCredential(dialog.id, form.credential)
    } else if (dialog.kind === 'provider') {
      if (dialog.editing) await providerAPI.update(dialog.id, providerPayload())
      else await providerAPI.create(providerPayload())
    } else if (dialog.kind === 'deployment') {
      const payload = { provider_connection_id: form.provider_connection_id, name: form.name, upstream_model: form.upstream_model, operations: form.operations, modalities: form.modalities, dimension: form.operations.includes('embedding') ? form.dimension : 0, status: form.status }
      if (dialog.editing) await deploymentAPI.update(dialog.id, payload)
      else await deploymentAPI.create(payload)
    } else {
      const payload = { name: form.name, code: form.code, scope_type: form.scope_type, model_deployment_id: form.model_deployment_id, status: form.status }
      if (dialog.editing) await profileAPI.update(dialog.id, payload)
      else await profileAPI.create(payload)
    }
    dialog.visible = false
    ElMessage.success(t('inference.common.saved'))
    await loadAll()
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    submitting.value = false
  }
}

async function confirmDelete(messageKey, action) {
  try {
    await ElMessageBox.confirm(t(messageKey), t('inference.common.confirmTitle'), { type: 'warning' })
    await action()
    ElMessage.success(t('inference.common.deleted'))
    await loadAll()
  } catch (error) {
    if (error !== 'cancel' && error !== 'close') ElMessage.error(errorMessage(error))
  }
}

function removeProvider(row) { return confirmDelete('inference.provider.deleteConfirm', () => providerAPI.remove(row.id)) }
function removeDeployment(row) { return confirmDelete('inference.deployment.deleteConfirm', () => deploymentAPI.remove(row.id)) }
function removeCredential(row) { return confirmDelete('inference.provider.deleteCredentialConfirm', () => providerAPI.deleteCredential(row.id)) }

async function probe(row) {
  try {
    const result = await deploymentAPI.probe(row.id)
    ElMessage.success(t('inference.deployment.probeSuccess', { status: result.status_code }))
  } catch (error) {
    ElMessage.error(errorMessage(error))
  }
}

onMounted(loadAll)
</script>

<style scoped>
.settings-page { box-sizing: border-box; min-height: 100%; padding: 20px 24px 40px; background: var(--addp-bg-primary); }
.page-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 12px; }
.page-header h1 { margin: 0 0 8px; color: var(--addp-text-primary); font-size: 22px; font-weight: 600; letter-spacing: 0; }
.header-actions, .service-actions, .onboarding-footer, .onboarding-footer__actions { display: flex; align-items: center; gap: 8px; }
.page-tabs, .resource-tabs { width: 100%; }
.service-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(420px, 100%), 1fr)); gap: 14px; padding-top: 8px; }
.service-card { min-width: 0; padding: 18px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); }
.service-card__header, .service-identity { display: flex; align-items: center; gap: 12px; }
.service-card__header { justify-content: space-between; }
.service-icon { flex: 0 0 auto; width: 36px; height: 36px; border-radius: 6px; background: var(--addp-bg-secondary); color: var(--el-color-primary); font-size: 20px; }
.service-title { min-width: 0; }
.service-title h2 { overflow: hidden; margin: 0 0 4px; color: var(--addp-text-primary); font-size: 16px; font-weight: 600; letter-spacing: 0; text-overflow: ellipsis; white-space: nowrap; }
.service-title span { color: var(--addp-text-secondary); font-size: 13px; }
.service-facts { display: grid; grid-template-columns: minmax(0, 2fr) minmax(120px, 1fr); gap: 16px 20px; margin: 20px 0; }
.service-facts div { min-width: 0; }
.service-facts dt { margin-bottom: 6px; color: var(--addp-text-tertiary); font-size: 12px; }
.service-facts dd { overflow: hidden; margin: 0; color: var(--addp-text-primary); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.service-actions { flex-wrap: wrap; padding-top: 14px; border-top: 1px solid var(--addp-border-color-light); }
.advanced-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 16px; min-height: 40px; color: var(--addp-text-secondary); font-size: 13px; }
.value-tag { margin-right: 4px; }
.form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.onboarding-steps { margin-bottom: 22px; }
.mobile-step-label { display: none; }
.onboarding-panel { min-height: 360px; }
.template-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
.template-card { position: relative; display: grid; grid-template-columns: 34px minmax(0, 1fr); gap: 12px; min-height: 92px; padding: 16px 46px 16px 16px; border: 1px solid var(--addp-border-color); border-radius: 6px; background: var(--addp-bg-primary); cursor: pointer; }
.template-card:hover, .template-card:focus-visible { border-color: var(--el-color-primary); outline: none; }
.template-card.is-selected { border-color: var(--el-color-primary); background: var(--addp-bg-secondary); }
.template-card > .el-icon { margin-top: 2px; color: var(--el-color-primary); font-size: 24px; }
.template-card h3 { margin: 0 0 8px; color: var(--addp-text-primary); font-size: 15px; font-weight: 600; letter-spacing: 0; }
.template-card p { margin: 0; color: var(--addp-text-secondary); font-size: 13px; line-height: 1.5; }
.template-doc { position: absolute; top: 8px; right: 8px; }
.connection-form { margin-top: 18px; }
.model-editor { padding: 18px 0; border-bottom: 1px solid var(--addp-border-color-light); }
.model-editor__grid { display: grid; grid-template-columns: minmax(0, 2fr) minmax(180px, 1fr); gap: 0 16px; }
.onboarding-complete { display: flex; flex-direction: column; justify-content: center; }
.onboarding-footer { justify-content: space-between; width: 100%; }
:deep(.el-select) { width: 100%; }
@media (max-width: 720px) {
  .settings-page { padding: 16px; }
  .page-header { align-items: flex-start; flex-direction: column; }
  .service-grid, .template-grid, .form-grid, .model-editor__grid, .service-facts { grid-template-columns: minmax(0, 1fr); }
  .onboarding-footer { align-items: stretch; flex-direction: column-reverse; }
  .onboarding-footer__actions { justify-content: flex-end; flex-wrap: wrap; }
  .onboarding-steps { display: none; }
  .mobile-step-label { display: block; margin-bottom: 18px; color: var(--addp-text-secondary); font-size: 13px; }
}
</style>
