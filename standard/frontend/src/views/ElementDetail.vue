<template>
  <div class="page-shell" v-loading="loading">
    <StatusAnnouncer :message="announcement" />
    <div class="page-header">
      <div class="header-left">
        <el-button :icon="ArrowLeft" @click="goBack">{{ $t('standard.common.back') }}</el-button>
        <h2>{{ revision.name || element.code || $t('standard.element.detailTitle') }}</h2>
        <el-tag v-if="revision.status" :type="statusType(revision.status)">
          R{{ revision.revision_no }} · {{ statusLabel(revision.status) }}
        </el-tag>
      </div>
      <div class="actions">
        <el-button v-if="editable" type="primary" :loading="savingRevision" @click="saveRevision">
          {{ $t('standard.common.save') }}
        </el-button>
        <el-button v-if="editable" type="warning" @click="act('submit')">{{ $t('standard.revision.submit') }}</el-button>
        <el-button v-if="reviewing && canPublish" @click="act('return')">{{ $t('standard.revision.return') }}</el-button>
        <el-button v-if="reviewing && canPublish" type="success" @click="act('publish')">{{ $t('standard.revision.publish') }}</el-button>
        <el-button v-if="!element.draft_revision && canUpdate" @click="newDraft">
          {{ $t('standard.revision.newDraft') }}
        </el-button>
        <el-button v-if="revision.status === 'published' && canPublish" type="danger" @click="act('withdraw')">
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
          <el-form :model="element" label-width="130px" :disabled="!identityEditable">
            <el-row :gutter="16">
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.codeLabel')">
                  <el-input :model-value="element.code" disabled />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.glossary.domainLabel')">
                  <el-select v-model="element.domain_id" clearable class="field-control">
                    <el-option v-for="domain in domains" :key="domain.id" :label="domain.name" :value="domain.id" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.common.stewardId')">
                  <el-input-number
                    v-model="element.steward_id"
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
                    v-model="element.tags"
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
          <template #header>{{ $t('standard.element.basicInfo') }}</template>
          <el-form :model="revision" label-width="130px" :disabled="!editable">
            <el-row :gutter="16">
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.nameLabel')">
                  <el-input v-model="revision.name" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.dataTypeLabel')">
                  <el-select v-model="revision.data_type" class="field-control" @change="handleDataTypeChange">
                    <el-option v-for="type in ELEMENT_DATA_TYPES" :key="type" :label="type" :value="type" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col v-if="supportsLength(revision.data_type)" :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.lengthLabel')">
                  <el-input-number v-model="revision.length" :min="1" class="field-control" />
                </el-form-item>
              </el-col>
              <template v-if="revision.data_type === 'decimal'">
                <el-col :xs="24" :sm="12">
                  <el-form-item :label="$t('standard.element.precisionLabel')">
                    <el-input-number v-model="revision.precision_num" :min="1" class="field-control" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="12">
                  <el-form-item :label="$t('standard.element.scaleLabel')">
                    <el-input-number
                      v-model="revision.scale"
                      :min="0"
                      :max="revision.precision_num || undefined"
                      class="field-control"
                    />
                  </el-form-item>
                </el-col>
              </template>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.nullableLabel')">
                  <el-switch v-model="revision.nullable" />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.unitLabel')">
                  <el-select v-model="revision.unit_id" clearable filterable class="field-control">
                    <el-option v-for="unit in units" :key="unit.id" :label="`${unit.name} (${unit.symbol})`" :value="unit.id" />
                  </el-select>
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.classificationLabel')">
                  <el-tree-select
                    v-model="revision.classification_id"
                    :data="classificationTree"
                    :props="{ label: 'name', value: 'id', children: 'children' }"
                    clearable
                    class="field-control"
                  />
                </el-form-item>
              </el-col>
              <el-col :xs="24" :sm="12">
                <el-form-item :label="$t('standard.element.securityLevelLabel')">
                  <el-select v-model="revision.security_level" clearable class="field-control">
                    <el-option v-for="level in securityLevels" :key="level" :label="level" :value="level" />
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
            <el-form-item :label="$t('standard.element.definitionLabel')">
              <el-input v-model="revision.definition" type="textarea" :rows="3" />
            </el-form-item>
            <el-form-item v-if="supportsFormat(revision.data_type)" :label="$t('standard.element.formatLabel')">
              <el-input v-model="revision.format" />
            </el-form-item>
            <el-form-item :label="$t('standard.element.defaultValueLabel')">
              <el-input v-model="revision.default_value" />
            </el-form-item>
            <el-form-item :label="$t('standard.element.exampleValuesLabel')">
              <el-select
                v-model="revision.example_values"
                multiple
                filterable
                allow-create
                default-first-option
                class="field-control"
              />
            </el-form-item>
            <el-form-item :label="$t('standard.revision.changeSummary')">
              <el-input v-model="revision.change_summary" type="textarea" :rows="2" />
            </el-form-item>
          </el-form>
        </el-card>

        <el-card shadow="never" class="section">
          <template #header>{{ $t('standard.element.valueDomain') }}</template>
          <el-form :model="revision" label-width="130px" :disabled="!editable">
            <el-form-item :label="$t('standard.element.valueDomainKind')">
              <el-radio-group v-model="revision.value_domain_kind" @change="resetValueDomain">
                <el-radio-button value="unrestricted">{{ $t('standard.element.unrestricted') }}</el-radio-button>
                <el-radio-button value="range" :disabled="!isNumericDataType(revision.data_type)">
                  {{ $t('standard.element.range') }}
                </el-radio-button>
                <el-radio-button value="enumeration">{{ $t('standard.element.enumeration') }}</el-radio-button>
              </el-radio-group>
            </el-form-item>
            <template v-if="revision.value_domain_kind === 'range'">
              <el-row :gutter="16">
                <el-col :xs="24" :sm="12">
                  <el-form-item :label="$t('standard.element.rangeMin')">
                    <el-input-number v-model="revision.range_constraint.min" class="field-control" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="12">
                  <el-form-item :label="$t('standard.element.rangeMax')">
                    <el-input-number v-model="revision.range_constraint.max" class="field-control" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="12">
                  <el-form-item :label="$t('standard.element.minInclusive')">
                    <el-switch v-model="revision.range_constraint.min_inclusive" />
                  </el-form-item>
                </el-col>
                <el-col :xs="24" :sm="12">
                  <el-form-item :label="$t('standard.element.maxInclusive')">
                    <el-switch v-model="revision.range_constraint.max_inclusive" />
                  </el-form-item>
                </el-col>
              </el-row>
            </template>
            <el-form-item
              v-if="revision.value_domain_kind === 'enumeration'"
              :label="$t('standard.element.codeSetLabel')"
            >
              <el-select v-model="revision.code_set_revision_id" filterable class="field-control">
                <el-option
                  v-for="codeSet in compatibleCodeSets"
                  :key="codeSet.current_revision.id"
                  :label="`${codeSet.current_revision.name} (${codeSet.code}) · R${codeSet.current_revision.revision_no}`"
                  :value="codeSet.current_revision.id"
                />
              </el-select>
            </el-form-item>
          </el-form>
        </el-card>

        <el-card shadow="never" class="section">
          <template #header>{{ $t('standard.element.qualityRules') }}</template>
          <el-alert :title="$t('standard.element.compiledRuleHint')" type="info" :closable="false" />
          <el-checkbox v-model="uniqueEnabled" :disabled="!editable" class="unique-rule">
            {{ $t('standard.element.ruleUnique') }}
          </el-checkbox>
        </el-card>

        <DocumentPanel
          v-if="element.id"
          entity-type="element"
          :entity-id="element.id"
          v-model:entity-version="element.version"
        />
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
        <el-card shadow="never" class="section">
          <template #header>{{ $t('standard.common.metadata') }}</template>
          <el-descriptions :column="1">
            <el-descriptions-item :label="$t('standard.common.id')">{{ element.id }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.element.codeLabel')">{{ element.code }}</el-descriptions-item>
            <el-descriptions-item :label="$t('standard.common.createdAt')">{{ formatTime(element.created_at) }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ArrowLeft } from '@element-plus/icons-vue'
import { StatusAnnouncer, useConsolePageDescriptor } from '@common-ui'
import { classificationAPI, codeSetAPI, domainAPI, elementAPI, unitAPI } from '../api/standard'
import DocumentPanel from '../components/DocumentPanel.vue'
import { navigateStandardRoute } from '@/utils/moduleNavigation'
import { useStandardPermissions } from '../composables/useStandardPermissions'
import { getStandardErrorMessage, isCanceledInteraction } from '../utils/apiError'
import { formatStandardDateTime } from '../utils/dateTime'
import {
  ELEMENT_DATA_TYPES,
  buildElementRevisionPayload,
  isCodeSetCompatible,
  isNumericDataType,
  resetIncompatibleElementConstraints,
  supportsFormat,
  supportsLength
} from '../utils/standardRevisionForm'

const route = useRoute()
const router = useRouter()
const { t, locale } = useI18n()
const { canUpdate, canPublish } = useStandardPermissions('element')

const loading = ref(false)
const savingIdentity = ref(false)
const savingRevision = ref(false)
const announcement = ref('')
const element = ref({})
const revisions = ref([])
const domains = ref([])
const units = ref([])
const classifications = ref([])
const publishedCodeSets = ref([])
const revision = reactive({})
const uniqueEnabled = ref(false)
const uniqueRuleKey = ref('')

const securityLevels = ['L1', 'L2', 'L3', 'L4']
const dateTimeValueFormat = 'YYYY-MM-DDTHH:mm:ssZ'
const identityEditable = computed(() => canUpdate.value && Boolean(element.value.id) && element.value.lifecycle_state !== 'deleting')
const editable = computed(() => canUpdate.value && revision.status === 'draft' && element.value.draft_revision_id === revision.id)
const reviewing = computed(() => revision.status === 'in_review' && element.value.draft_revision_id === revision.id)
const classificationTree = computed(() => tree(classifications.value))
const compatibleCodeSets = computed(() => publishedCodeSets.value.filter(codeSet => (
  isCodeSetCompatible(revision.data_type, codeSet.current_revision?.value_type)
)))

useConsolePageDescriptor(router, 'standard', {
  title: computed(() => t('standard.element.recentVisitTitle')),
  subject: computed(() => revision.name || element.value.code || ''),
  ready: computed(() => Boolean(element.value.id))
})

function tree(list, parent = null) {
  return list
    .filter(item => (item.parent_id || null) === parent)
    .map(item => ({ ...item, children: tree(list, item.id) }))
}

const flatten = nodes => nodes.flatMap(node => [node, ...flatten(node.children || [])])
const statusLabel = status => status ? t(`standard.revision.status.${status}`) : '-'
const statusType = status => ({
  draft: 'info',
  in_review: 'warning',
  published: 'success',
  withdrawn: 'danger'
}[status] || 'info')
const formatTime = value => formatStandardDateTime(value, locale.value)

function setRevision(value) {
  Object.keys(revision).forEach(key => delete revision[key])
  Object.assign(revision, structuredClone(value || {}))
  revision.example_values ||= []
  revision.value_domain_kind ||= 'unrestricted'
  if (revision.value_domain_kind === 'range') {
    revision.range_constraint ||= { min: null, max: null, min_inclusive: true, max_inclusive: true }
  }
  const uniqueRule = revision.extra_quality_rules?.rules?.find(rule => rule.type === 'unique')
  uniqueRuleKey.value = uniqueRule?.rule_key || ''
  uniqueEnabled.value = Boolean(uniqueRule?.enabled)
}

async function load() {
  loading.value = true
  try {
    const [aggregate, history] = await Promise.all([
      elementAPI.get(route.params.id),
      elementAPI.listRevisions(route.params.id)
    ])
    element.value = aggregate
    element.value.tags ||= []
    revisions.value = history || []
    setRevision(aggregate.draft_revision || aggregate.current_revision || history?.[0])
  } catch (error) {
    ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.loadFailed'))
    goBack()
  } finally {
    loading.value = false
  }
}

async function loadOptions() {
  const [domainResult, unitResult, classificationResult] = await Promise.allSettled([
    domainAPI.list(),
    unitAPI.list({ page_size: 500 }),
    classificationAPI.list()
  ])
  domains.value = domainResult.status === 'fulfilled' ? flatten(domainResult.value || []) : []
  units.value = unitResult.status === 'fulfilled' ? unitResult.value || [] : []
  classifications.value = classificationResult.status === 'fulfilled' ? classificationResult.value || [] : []
  await loadCodeSetOptions()
}

async function loadCodeSetOptions() {
  const params = { status: 'published', page_size: 500 }
  if (revision.effective_from) params.as_of = revision.effective_from
  try {
    const result = await codeSetAPI.list(params)
    publishedCodeSets.value = (result.data || []).filter(item => item.current_revision)
  } catch {
    publishedCodeSets.value = []
  }
}

async function saveIdentity() {
  savingIdentity.value = true
  announcement.value = t('standard.common.saving')
  try {
    element.value = await elementAPI.update(element.value.id, {
      version: element.value.version,
      domain_id: element.value.domain_id ?? null,
      steward_id: element.value.steward_id ?? null,
      tags: element.value.tags || []
    })
    element.value.tags ||= []
    announcement.value = t('standard.common.saveSuccess')
    ElMessage.success(announcement.value)
  } catch (error) {
    announcement.value = t('standard.common.saveFailed')
    ElMessage.error(getStandardErrorMessage(error, t, 'standard.common.saveFailed'))
  } finally {
    savingIdentity.value = false
  }
}

async function saveRevision() {
  savingRevision.value = true
  announcement.value = t('standard.common.saving')
  try {
    if (uniqueEnabled.value && !uniqueRuleKey.value) uniqueRuleKey.value = crypto.randomUUID()
    const aggregate = await elementAPI.updateRevision(
      element.value.id,
      revision.id,
      buildElementRevisionPayload(
        revision,
        element.value.version,
        uniqueRuleKey.value,
        uniqueEnabled.value
      )
    )
    element.value = aggregate
    setRevision(aggregate.draft_revision)
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

function handleDataTypeChange(dataType) {
  resetIncompatibleElementConstraints(revision, dataType)
}

function resetValueDomain(kind) {
  revision.range_constraint = kind === 'range'
    ? { min: null, max: null, min_inclusive: true, max_inclusive: true }
    : null
  revision.code_set_revision_id = null
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
    element.value = await elementAPI[method](element.value.id, revision.id, element.value.version)
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
    element.value = await elementAPI.createRevision(element.value.id, {
      version: element.value.version,
      change_summary: value.trim()
    })
    await load()
  } catch (error) {
    if (!isCanceledInteraction(error)) ElMessage.error(getStandardErrorMessage(error, t))
  }
}

const selectRevision = item => setRevision(item)
const goBack = () => navigateStandardRoute(router, '/elements', { history: 'replace' })

watch(() => route.params.id, () => {
  load()
  loadOptions()
}, { immediate: true })

watch(() => revision.effective_from, () => loadCodeSetOptions())
</script>

<style scoped>
.page-shell{min-height:100%;padding:20px;background:var(--addp-bg-secondary);color:var(--addp-text-primary)}
.page-header,.header-left,.actions,.card-header,.history-row{display:flex;align-items:center}
.page-header,.card-header,.history-row{justify-content:space-between}
.page-header{gap:16px;margin-bottom:16px}
.header-left,.actions{gap:10px;flex-wrap:wrap}
.section{margin-bottom:16px}
.field-control{width:100%}
.unique-rule{margin-top:16px}
.history-row{gap:8px}
.page-shell :deep(.el-card){background:var(--addp-bg-primary);border-color:var(--addp-border-color)}
h2{margin:0;font-size:20px}
@media (max-width:768px){
  .page-shell{padding:16px}
  .page-header{align-items:flex-start;flex-direction:column}
}
</style>
