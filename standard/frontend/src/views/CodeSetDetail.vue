<template>
  <div class="page-shell" v-loading="loading">
    <StatusAnnouncer :message="announcement" />
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button>
        <h2>{{ revision.name || codeSet.code || $t('standard.codeSet.detailTitle') }}</h2>
        <el-tag v-if="revision.status" :type="statusType(revision.status)">
          R{{ revision.revision_no }} · {{ statusLabel(revision.status) }}
        </el-tag>
        <el-tag v-if="codeSet.origin">{{ $t(`standard.codeSet.originValue.${codeSet.origin}`) }}</el-tag>
      </div>
      <div class="actions">
        <el-button v-if="editable" type="primary" :loading="savingRevision" @click="saveRevision">
          {{ $t('standard.common.save') }}
        </el-button>
        <el-button v-if="editable" type="warning" @click="act('submit')">{{ $t('standard.revision.submit') }}</el-button>
        <el-button v-if="reviewing && canPublish" @click="act('return')">{{ $t('standard.revision.return') }}</el-button>
        <el-button v-if="reviewing && canPublish" type="success" @click="act('publish')">{{ $t('standard.revision.publish') }}</el-button>
        <el-button v-if="!codeSet.draft_revision && canUpdate && !platform" @click="newDraft">
          {{ $t('standard.revision.newDraft') }}
        </el-button>
        <el-button v-if="revision.status === 'published' && canPublish && !platform" type="danger" @click="act('withdraw')">
          {{ $t('standard.revision.withdraw') }}
        </el-button>
      </div>
    </div>

    <el-row :gutter="16">
      <el-col :xs="24" :lg="16">
        <el-card shadow="never" class="section">
          <template #header>
            <div class="card-header">
              <span>{{ $t('standard.common.governanceInfo') }}</span>
              <el-button
                v-if="identityEditable"
                type="primary"
                size="small"
                :loading="savingIdentity"
                @click="saveIdentity"
              >
                {{ $t('standard.common.save') }}
              </el-button>
            </div>
          </template>
          <el-form :model="codeSet" label-width="130px" :disabled="!identityEditable">
            <el-row :gutter="16">
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.codeSet.codeLabel')">
                  <el-input :model-value="codeSet.code" disabled />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.common.scopeLabel')">
                  <el-select v-model="codeSet.scope_type" class="field-control">
                    <el-option v-for="scope in scopeOptions" :key="scope" :label="scopeLabel(scope)" :value="scope" :disabled="scope === 'platform'" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col v-if="requiresOwnerDomain(codeSet.scope_type)" :xs="24" :sm="12">
                <el-form-item :label="$t('standard.common.ownerDomainLabel')" required>
                  <el-select v-model="codeSet.owner_domain_id" class="field-control">
                    <el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.common.stewardId')">
                  <el-input-number
                    v-model="codeSet.steward_id"
                    :min="1"
                    :controls="false"
                    :placeholder="$t('standard.common.stewardIdPlaceholder')"
                    class="field-control"
                  />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.common.tags')">
                  <el-select
                    v-model="codeSet.tags"
                    multiple
                    filterable
                    allow-create
                    default-first-option
                    :placeholder="$t('standard.common.tagsPlaceholder')"
                    class="field-control"
                  />
                </el-form-item>
              </el-col>
            </el-row>
          </el-form>
        </el-card>

        <el-card shadow="never" class="section">
          <template #header>{{ $t('standard.codeSet.basicInfo') }}</template>
          <el-form :model="revision" label-width="130px" :disabled="!editable">
            <el-row :gutter="16">
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.codeSet.nameLabel')">
                  <el-input v-model="revision.name" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.codeSet.valueType')">
                  <el-select v-model="revision.value_type" class="field-control">
                    <el-option value="string" />
                    <el-option value="int" />
                    <el-option value="bigint" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.revision.effectiveFrom')">
                  <el-date-picker
                    v-model="revision.effective_from"
                    type="datetime"
                    :value-format="dateTimeValueFormat"
                    class="field-control"
                  />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.revision.effectiveTo')">
                  <el-date-picker
                    v-model="revision.effective_to"
                    type="datetime"
                    :value-format="dateTimeValueFormat"
                    class="field-control"
                  />
                </el-form-item>
              </el-col>
            </el-row>
            <el-form-item :label="$t('standard.codeSet.descriptionLabel')">
              <el-input v-model="revision.description" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item :label="$t('standard.revision.changeSummary')">
              <el-input v-model="revision.change_summary" type="textarea" :rows="2" />
            </el-form-item>
          </el-form>
        </el-card>

        <el-card shadow="never" class="section">
          <template #header>
            <div class="card-header">
              <span>{{ $t('standard.codeSet.items') }}</span>
              <el-button v-if="editable" type="primary" size="small" :icon="Plus" @click="openItem()">
                {{ $t('standard.codeSet.addItem') }}
              </el-button>
            </div>
          </template>
          <el-table :data="revision.items || []" stripe>
            <el-table-column :label="$t('standard.codeSet.itemCode')" prop="code" width="150" />
            <el-table-column :label="$t('standard.codeSet.itemLabel')" prop="label" min-width="150" />
            <el-table-column :label="$t('standard.codeSet.itemDefinition')" prop="definition" min-width="180" show-overflow-tooltip />
            <el-table-column :label="$t('standard.codeSet.replacementItem')" min-width="160">
              <template #default="{ row }">{{ replacementLabel(row.replacement_item_id) }}</template>
            </el-table-column>
            <el-table-column :label="$t('standard.common.sort')" prop="sort_order" width="80" />
            <el-table-column :label="$t('standard.common.status')" width="100">
              <template #default="{ row }">
                <el-tag :type="row.status === 'active' ? 'success' : 'info'">
                  {{ $t(`standard.codeSet.itemStatus.${row.status}`) }}
                </el-tag>
              </template>
            </el-table-column>
            <el-table-column v-if="editable" :label="$t('standard.common.actions')" width="140" fixed="right">
              <template #default="{ row }">
                <el-button link type="primary" @click="openItem(row)">{{ $t('standard.common.edit') }}</el-button>
                <el-button link type="danger" @click="removeItem(row)">{{ $t('standard.common.delete') }}</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-card>
      </el-col>

      <el-col :xs="24" :lg="8">
        <el-card shadow="never" class="section">
          <template #header>{{ $t('standard.revision.history') }}</template>
          <el-timeline>
            <el-timeline-item
              v-for="item in revisions"
              :key="item.id"
              :timestamp="formatTime(item.created_at)"
            >
              <div class="history-row">
                <el-link @click="selectRevision(item)">R{{ item.revision_no }} · {{ item.name }}</el-link>
                <el-tag size="small" :type="statusType(item.status)">{{ statusLabel(item.status) }}</el-tag>
              </div>
              <small>{{ item.change_summary }}</small>
            </el-timeline-item>
          </el-timeline>
        </el-card>
      </el-col>
    </el-row>

    <el-dialog
      v-model="itemDialog"
      class="addp-dialog"
      :title="editingItem ? $t('standard.common.edit') : $t('standard.codeSet.addItem')"
      width="min(560px, calc(100vw - 24px))"
    >
      <el-form :model="itemForm" label-position="top">
        <el-form-item :label="$t('standard.codeSet.itemCode')" required>
          <el-input v-model="itemForm.code" :disabled="Boolean(editingItem)" />
        </el-form-item>
        <el-form-item :label="$t('standard.codeSet.itemLabel')" required>
          <el-input v-model="itemForm.label" />
        </el-form-item>
        <el-form-item :label="$t('standard.codeSet.itemDefinition')">
          <el-input v-model="itemForm.definition" type="textarea" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.sort')">
          <el-input-number v-model="itemForm.sort_order" :min="0" />
        </el-form-item>
        <el-form-item :label="$t('standard.common.status')">
          <el-radio-group v-model="itemForm.status" @change="handleItemStatusChange">
            <el-radio value="active">{{ $t('standard.codeSet.itemStatus.active') }}</el-radio>
            <el-radio value="deprecated">{{ $t('standard.codeSet.itemStatus.deprecated') }}</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item
          v-if="editingItem && itemForm.status === 'deprecated'"
          :label="$t('standard.codeSet.replacementItem')"
        >
          <el-select v-model="itemForm.replacement_item_id" clearable class="field-control">
            <el-option
              v-for="item in replacementItems"
              :key="item.id"
              :label="`${item.label} (${item.code})`"
              :value="item.id"
            />
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="itemDialog = false">{{ $t('standard.common.cancel') }}</el-button>
        <el-button type="primary" :loading="itemSaving" @click="saveItem">{{ $t('standard.common.confirm') }}</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft, Plus } from '@element-plus/icons-vue'
import { StatusAnnouncer, useConsolePageDescriptor } from '@common-ui'
import { codeSetAPI, domainAPI } from '../api/standard'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { formatStandardDateTime } from '../utils/dateTime'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { buildCodeSetRevisionPayload, listReplacementItems } from '../utils/standardRevisionForm'
import { EDITABLE_STANDARD_SCOPES, buildStandardOwnership, requiresOwnerDomain, standardScopeLabelKey } from '../utils/standardScope'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const { canUpdate, canPublish } = useStandardPermissions('code_set')

const loading = ref(false)
const savingIdentity = ref(false)
const savingRevision = ref(false)
const itemSaving = ref(false)
const itemDialog = ref(false)
const editingItem = ref(null)
const announcement = ref('')
const codeSet = ref({})
const revisions = ref([])
const domains = ref([])
const revision = reactive({})
const itemForm = reactive({
  code: '',
  label: '',
  definition: '',
  sort_order: 0,
  status: 'active',
  replacement_item_id: null
})

const dateTimeValueFormat = 'YYYY-MM-DDTHH:mm:ssZ'
const scopeOptions = ['platform', ...EDITABLE_STANDARD_SCOPES]
const platform = computed(() => codeSet.value.origin === 'platform')
const identityEditable = computed(() => (
  canUpdate.value &&
  Boolean(codeSet.value.id) &&
  !platform.value &&
  codeSet.value.lifecycle_state !== 'deleting'
))
const editable = computed(() => (
  canUpdate.value &&
  !platform.value &&
  revision.status === 'draft' &&
  codeSet.value.draft_revision_id === revision.id
))
const reviewing = computed(() => revision.status === 'in_review' && codeSet.value.draft_revision_id === revision.id)
const replacementItems = computed(() => listReplacementItems(revision.items, editingItem.value?.id))

useConsolePageDescriptor(router, 'standard', {
  title: computed(() => t('standard.codeSet.recentVisitTitle')),
  subject: computed(() => revision.name || codeSet.value.code || ''),
  ready: computed(() => Boolean(codeSet.value.id))
})

const flatten = nodes => nodes.flatMap(node => [node, ...flatten(node.children || [])])
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const statusType = status => ({
  draft: 'info',
  in_review: 'warning',
  published: 'success',
  withdrawn: 'danger'
}[status] || 'info')
const scopeLabel = scope => scope ? t(standardScopeLabelKey(scope)) : '-'
const formatTime = value => formatStandardDateTime(value, locale.value)

function setRevision(value) {
  Object.keys(revision).forEach(key => delete revision[key])
  Object.assign(revision, structuredClone(value || {}))
  revision.items ||= []
}

async function load() {
  loading.value = true
  try {
    const [aggregate, history] = await Promise.all([
      codeSetAPI.get(route.params.id),
      codeSetAPI.listRevisions(route.params.id)
    ])
    codeSet.value = aggregate
    codeSet.value.tags ||= []
    revisions.value = history || []
    setRevision(aggregate.draft_revision || aggregate.current_revision || history?.[0])
  } catch (error) {
    ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed'))
    goBack()
  } finally {
    loading.value = false
  }
}

async function loadDomains() {
  try {
    domains.value = flatten(await domainAPI.list() || [])
  } catch {
    domains.value = []
  }
}

async function saveIdentity() {
  if (requiresOwnerDomain(codeSet.value.scope_type) && !codeSet.value.owner_domain_id) {
    ElMessage.error(t('standard.common.ownerDomainRequired'))
    return
  }
  savingIdentity.value = true
  announcement.value = t('standard.common.saving')
  try {
    codeSet.value = await codeSetAPI.update(codeSet.value.id, {
      version: codeSet.value.version,
      ...buildStandardOwnership(codeSet.value.scope_type, codeSet.value.owner_domain_id),
      steward_id: codeSet.value.steward_id ?? null,
      tags: codeSet.value.tags || []
    })
    codeSet.value.tags ||= []
    announcement.value = t('standard.common.saveSuccess')
    ElMessage.success(announcement.value)
  } catch (error) {
    announcement.value = t('standard.common.saveFailed')
    ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.saveFailed'))
  } finally {
    savingIdentity.value = false
  }
}

watch(() => codeSet.value.scope_type, scope => {
  if (!requiresOwnerDomain(scope)) codeSet.value.owner_domain_id = null
})

async function saveRevision() {
  savingRevision.value = true
  announcement.value = t('standard.common.saving')
  try {
    codeSet.value = await codeSetAPI.updateRevision(
      codeSet.value.id,
      revision.id,
      buildCodeSetRevisionPayload(revision, codeSet.value.version)
    )
    setRevision(codeSet.value.draft_revision)
    announcement.value = t('standard.common.saveSuccess')
    ElMessage.success(announcement.value)
    await load()
  } catch (error) {
    announcement.value = t('standard.common.saveFailed')
    ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.saveFailed'))
  } finally {
    savingRevision.value = false
  }
}

async function act(action) {
  try {
    await ElMessageBox.confirm(
      t(`standard.revision.confirm.${action}`),
      t('standard.common.hint'),
      {
        customClass: 'addp-message-box',
        confirmButtonText: t('standard.common.confirm'),
        cancelButtonText: t('standard.common.cancel')
      }
    )
    const method = {
      submit: 'submitRevision',
      return: 'returnRevision',
      publish: 'publishRevision',
      withdraw: 'withdrawRevision'
    }[action]
    codeSet.value = await codeSetAPI[method](codeSet.value.id, revision.id, codeSet.value.version)
    ElMessage.success(t('standard.common.updateSuccess'))
    await load()
  } catch (error) {
    if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t))
  }
}

async function newDraft() {
  try {
    const { value } = await ElMessageBox.prompt(
      t('standard.revision.changeSummary'),
      t('standard.revision.newDraft'),
      {
        customClass: 'addp-message-box',
        confirmButtonText: t('standard.common.confirm'),
        cancelButtonText: t('standard.common.cancel'),
        inputPattern: /\S+/,
        inputErrorMessage: t('standard.revision.changeSummaryRequired')
      }
    )
    await codeSetAPI.createRevision(codeSet.value.id, {
      version: codeSet.value.version,
      change_summary: value.trim()
    })
    await load()
  } catch (error) {
    if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t))
  }
}

function openItem(item) {
  editingItem.value = item || null
  Object.assign(itemForm, item ? {
    code: item.code,
    label: item.label,
    definition: item.definition || '',
    sort_order: item.sort_order || 0,
    status: item.status,
    replacement_item_id: item.replacement_item_id ?? null
  } : {
    code: '',
    label: '',
    definition: '',
    sort_order: 0,
    status: 'active',
    replacement_item_id: null
  })
  itemDialog.value = true
}

function handleItemStatusChange(status) {
  if (status === 'active') itemForm.replacement_item_id = null
}

async function saveItem() {
  if (!itemForm.code.trim() || !itemForm.label.trim()) return
  itemSaving.value = true
  try {
    const result = editingItem.value
      ? await codeSetAPI.updateItem(codeSet.value.id, revision.id, editingItem.value.id, {
          version: codeSet.value.version,
          label: itemForm.label,
          definition: itemForm.definition,
          sort_order: itemForm.sort_order,
          status: itemForm.status,
          replacement_item_id: itemForm.replacement_item_id ?? null
        })
      : await codeSetAPI.createItem(codeSet.value.id, revision.id, {
          version: codeSet.value.version,
          code: itemForm.code,
          label: itemForm.label,
          definition: itemForm.definition,
          sort_order: itemForm.sort_order,
          status: itemForm.status
        })
    codeSet.value.version = result.version
    itemDialog.value = false
    await load()
    ElMessage.success(t('standard.common.updateSuccess'))
  } catch (error) {
    ElMessage.error(getStandardErrorMessage(error, t))
  } finally {
    itemSaving.value = false
  }
}

async function removeItem(item) {
  try {
    await ElMessageBox.confirm(
      t('standard.codeSet.confirmDeleteItem', { name: item.label }),
      t('standard.common.hint'),
      {
        customClass: 'addp-message-box',
        confirmButtonText: t('standard.common.confirm'),
        cancelButtonText: t('standard.common.cancel'),
        confirmButtonClass: 'el-button--danger'
      }
    )
    const result = await codeSetAPI.deleteItem(
      codeSet.value.id,
      revision.id,
      item.id,
      codeSet.value.version
    )
    codeSet.value.version = result.version
    await load()
  } catch (error) {
    if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t))
  }
}

function replacementLabel(replacementItemID) {
  if (!replacementItemID) return t('standard.codeSet.noReplacement')
  const item = revision.items?.find(candidate => candidate.id === replacementItemID)
  return item ? `${item.label} (${item.code})` : `#${replacementItemID}`
}

const selectRevision = item => setRevision(item)
const goBack = () => navigateStandardRoute(router, { path: '/code-sets', query: route.query }, { history: 'replace' })

watch(() => route.params.id, () => {
  load()
  loadDomains()
}, { immediate: true })
</script>

<style scoped>
.page-shell{min-height:100%;padding:20px;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}
.page-header,.header-left,.actions,.card-header,.history-row{display:flex;align-items:center}
.page-header,.card-header,.history-row{justify-content:space-between}
.page-header{gap:16px;margin-bottom:16px}
.header-left,.actions{gap:10px;flex-wrap:wrap}
.section{margin-bottom:16px}
.field-control{width:100%}
.history-row{gap:8px}
.page-shell :deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}
h2{margin:0;font-size:20px}
@media (max-width:768px){
  .page-shell{padding:16px}
  .page-header{align-items:flex-start;flex-direction:column}
}
</style>
