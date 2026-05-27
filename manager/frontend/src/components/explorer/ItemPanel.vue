<template>
  <div class="item-panel">
    <div class="item-tab-bar" role="tablist">
      <button
        class="item-tab-button"
        :class="{ active: activeTab === 'preview' }"
        type="button"
        role="tab"
        :aria-selected="activeTab === 'preview'"
        @click="activeTab = 'preview'"
      >
        {{ t('manager.explorer.previewTab') }}
      </button>
      <button
        v-if="itemMeta"
        class="item-tab-button"
        :class="{ active: activeTab === 'attributes' }"
        type="button"
        role="tab"
        :aria-selected="activeTab === 'attributes'"
        @click="activeTab = 'attributes'"
      >
        {{ t('manager.explorer.attributesTab') }}
      </button>
    </div>

    <div class="item-tab-content">
      <div v-show="activeTab === 'preview'" class="preview-tab-pane">
        <PreviewPanel
          :selected-node="selectedNode"
          :preview-data="previewData"
          :loading="loading"
          @page-change="$emit('page-change', $event)"
          @navigate="$emit('navigate', $event)"
          @child-change="$emit('child-change', $event)"
        />
      </div>

      <div
        v-if="itemMeta"
        v-show="activeTab === 'attributes'"
        class="attributes-tab-pane"
      >
        <div class="attributes-panel">
          <div class="attributes-toolbar">
            <el-button size="small" @click="jsonDialogVisible = true">
              {{ t('manager.explorer.viewAttributeJson') }}
            </el-button>
          </div>

          <section class="meta-overview">
            <div
              v-for="item in overviewItems"
              :key="item.key"
              class="overview-item"
            >
              <span class="overview-label">{{ item.label }}</span>
              <span class="overview-value">{{ item.value }}</span>
            </div>
          </section>

          <el-empty
            v-if="attributeSections.length === 0"
            :description="t('manager.explorer.noAdditionalAttributes')"
          />

          <div v-else class="attribute-sections">
            <section
              v-for="section in attributeSections"
              :key="section.key"
              class="attribute-card"
            >
              <div class="attribute-card-header">
                <div>
                  <div class="attribute-section-title">{{ section.title }}</div>
                  <div class="attribute-section-key">{{ section.key }}</div>
                </div>
                <el-tag size="small" effect="plain" type="info">
                  {{ t('manager.explorer.attributeCount', { count: section.count }) }}
                </el-tag>
              </div>

              <div v-if="section.entries.length" class="attribute-items">
                <div
                  v-for="entry in section.entries"
                  :key="entry.path"
                  class="attribute-item"
                >
                  <span class="attribute-key">{{ entry.label }}</span>
                  <span class="attribute-value" :title="entry.title">
                    {{ entry.display }}
                  </span>
                </div>
              </div>

              <div v-if="section.groups.length" class="attribute-groups">
                <div
                  v-for="group in section.groups"
                  :key="group.path"
                  class="attribute-group"
                >
                  <div class="attribute-group-header">
                    <span class="attribute-group-title">{{ group.title }}</span>
                    <span class="attribute-group-key">{{ group.key }}</span>
                  </div>

                  <div v-if="group.entries.length" class="attribute-items">
                    <div
                      v-for="entry in group.entries"
                      :key="entry.path"
                      class="attribute-item"
                    >
                      <span class="attribute-key">{{ entry.label }}</span>
                      <span class="attribute-value" :title="entry.title">
                        {{ entry.display }}
                      </span>
                    </div>
                  </div>

                  <div v-if="group.subgroups.length" class="attribute-subgroups">
                    <div
                      v-for="subgroup in group.subgroups"
                      :key="subgroup.path"
                      class="attribute-subgroup"
                    >
                      <div class="attribute-group-header">
                        <span class="attribute-group-title">{{ subgroup.title }}</span>
                        <span class="attribute-group-key">{{ subgroup.key }}</span>
                      </div>

                      <div v-if="subgroup.entries.length" class="attribute-items attribute-subgroup-items">
                        <div
                          v-for="entry in subgroup.entries"
                          :key="entry.path"
                          class="attribute-item"
                        >
                          <span class="attribute-key">{{ entry.label }}</span>
                          <span class="attribute-value" :title="entry.title">
                            {{ entry.display }}
                          </span>
                        </div>
                      </div>

                      <div
                        v-for="table in subgroup.tables"
                        :key="table.path"
                        class="attribute-table-wrap"
                      >
                        <div class="attribute-table-title">{{ table.title }}</div>
                        <el-table
                          :data="table.rows"
                          size="small"
                          border
                          class="attribute-table"
                        >
                          <el-table-column
                            v-for="column in table.columns"
                            :key="column.key"
                            :prop="column.key"
                            :label="column.label"
                            :min-width="column.minWidth"
                            :class-name="column.className"
                            show-overflow-tooltip
                          />
                        </el-table>
                      </div>
                    </div>
                  </div>

                  <div
                    v-for="table in group.tables"
                    :key="table.path"
                    class="attribute-table-wrap"
                  >
                    <div class="attribute-table-title">{{ table.title }}</div>
                    <el-table
                      :data="table.rows"
                      size="small"
                      border
                      class="attribute-table"
                    >
                        <el-table-column
                          v-for="column in table.columns"
                          :key="column.key"
                          :prop="column.key"
                          :label="column.label"
                          :min-width="column.minWidth"
                          :class-name="column.className"
                          show-overflow-tooltip
                        />
                    </el-table>
                  </div>
                </div>
              </div>
            </section>
          </div>
        </div>
      </div>
    </div>

    <el-dialog
      v-model="jsonDialogVisible"
      :title="t('manager.explorer.attributeJsonTitle')"
      width="760px"
      append-to-body
      destroy-on-close
    >
      <pre class="attribute-json">{{ rawAttributesJson }}</pre>
    </el-dialog>
  </div>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import PreviewPanel from '@/components/explorer/PreviewPanel.vue'

const { t } = useI18n()

const props = defineProps({
  selectedNode: {
    type: Object,
    default: null
  },
  previewData: {
    type: Object,
    default: null
  },
  loading: {
    type: Boolean,
    default: false
  }
})

defineEmits(['page-change', 'navigate', 'child-change'])

const activeTab = ref('preview')
const jsonDialogVisible = ref(false)

watch(() => props.selectedNode?.locator, () => {
  activeTab.value = 'preview'
  jsonDialogVisible.value = false
})

const itemMeta = computed(() => props.previewData?.item_meta)

const sectionOrder = [
  'item',
  'storage',
  'type_info',
  'capabilities',
  'format_info',
  'content_index',
  'schema_version'
]

const groupOrder = [
  'table',
  'container',
  'media',
  'document',
  'graph',
  'spatial',
  'indexing',
  'statistics',
  'extraction',
  'shapefile',
  'csv',
  'json',
  'geojson',
  'parquet'
]

const fieldOrder = [
  'data_type',
  'format',
  'layout',
  'file_count',
  'refs',
  'physical_path',
  'total_size',
  'bucket',
  'path',
  'row_count',
  'column_count',
  'fields',
  'native',
  'source',
  'mode',
  'encoding',
  'line_ending',
  'version',
  'page_size',
  'page_count',
  'table_count',
  'view_count',
  'index_count',
  'indexes',
  'is_sampled',
  'schema_type',
  'entry_count',
  'directory_count',
  'sampled_children',
  'children_truncated',
  'sheet_count',
  'default_sheet',
  'sampled_sheets',
  'geometry_columns',
  'extent',
  'extent_srid',
  'primary_geometry_column',
  'has_spatial_index',
  'extractor_available',
  'srid',
  'dimension',
  'geometry_type',
  'name',
  'nullable',
  'shape_type',
  'encoding',
  'base_name',
  'ref_extensions',
  'dbf_version',
  'has_cpg',
  'has_prj'
]

const sectionLabelKeys = {
  item: 'manager.explorer.attributes.sections.item',
  storage: 'manager.explorer.attributes.sections.storage',
  type_info: 'manager.explorer.attributes.sections.typeInfo',
  capabilities: 'manager.explorer.attributes.sections.capabilities',
  format_info: 'manager.explorer.attributes.sections.formatInfo',
  content_index: 'manager.explorer.attributes.sections.contentIndex',
  schema_version: 'manager.explorer.attributes.sections.schemaVersion'
}

const groupLabelKeys = {
  table: 'manager.explorer.attributes.groups.table',
  container: 'manager.explorer.attributes.groups.container',
  media: 'manager.explorer.attributes.groups.media',
  document: 'manager.explorer.attributes.groups.document',
  graph: 'manager.explorer.attributes.groups.graph',
  spatial: 'manager.explorer.attributes.groups.spatial',
  indexing: 'manager.explorer.attributes.groups.indexing',
  extraction: 'manager.explorer.attributes.groups.extraction',
  shapefile: 'manager.explorer.attributes.groups.shapefile',
  csv: 'manager.explorer.attributes.groups.csv',
  json: 'manager.explorer.attributes.groups.json',
  geojson: 'manager.explorer.attributes.groups.geojson',
  parquet: 'manager.explorer.attributes.groups.parquet',
  excel: 'manager.explorer.attributes.groups.excel',
  sqlite: 'manager.explorer.attributes.groups.sqlite',
  zip: 'manager.explorer.attributes.groups.zip',
  statistics: 'manager.explorer.attributes.groups.statistics',
  native: 'manager.explorer.attributes.groups.native',
  source: 'manager.explorer.attributes.groups.source',
  refs: 'manager.explorer.attributes.groups.refs'
}

const fieldLabelKeys = {
  item_type: 'meta.itemType',
  full_name: 'meta.fullName',
  scanned_at: 'meta.scannedAt',
  data_type: 'manager.explorer.attributes.fields.dataType',
  format: 'manager.explorer.attributes.fields.format',
  layout: 'manager.explorer.attributes.fields.layout',
  file_count: 'manager.explorer.attributes.fields.fileCount',
  refs: 'manager.explorer.attributes.fields.refs',
  physical_path: 'manager.explorer.attributes.fields.physicalPath',
  total_size: 'manager.explorer.attributes.fields.totalSize',
  bucket: 'manager.explorer.attributes.fields.bucket',
  path: 'manager.explorer.attributes.fields.path',
  row_count: 'manager.explorer.attributes.fields.rowCount',
  column_count: 'manager.explorer.attributes.fields.columnCount',
  fields: 'manager.explorer.attributes.fields.fields',
  geometry_columns: 'manager.explorer.attributes.fields.geometryColumns',
  extent: 'manager.explorer.attributes.fields.extent',
  primary_geometry_column: 'manager.explorer.attributes.fields.primaryGeometryColumn',
  has_spatial_index: 'manager.explorer.attributes.fields.hasSpatialIndex',
  srid: 'manager.explorer.attributes.fields.srid',
  dimension: 'manager.explorer.attributes.fields.dimension',
  geometry_type: 'manager.explorer.attributes.fields.geometryType',
  name: 'manager.explorer.attributes.fields.name',
  nullable: 'manager.explorer.attributes.fields.nullable',
  shape_type: 'manager.explorer.attributes.fields.shapeType',
  encoding: 'manager.explorer.attributes.fields.encoding',
  line_ending: 'manager.explorer.attributes.fields.lineEnding',
  mode: 'manager.explorer.attributes.fields.mode',
  version: 'manager.explorer.attributes.fields.version',
  base_name: 'manager.explorer.attributes.fields.baseName',
  ref_extensions: 'manager.explorer.attributes.fields.refExtensions',
  dbf_version: 'manager.explorer.attributes.fields.dbfVersion',
  has_cpg: 'manager.explorer.attributes.fields.hasCpg',
  has_prj: 'manager.explorer.attributes.fields.hasPrj',
  extent_srid: 'manager.explorer.attributes.fields.extentSrid',
  extractor_available: 'manager.explorer.attributes.fields.extractorAvailable',
  relkind: 'manager.explorer.attributes.fields.relkind',
  table_type: 'manager.explorer.attributes.fields.tableType',
  kind: 'manager.explorer.attributes.fields.kind',
  size_bytes: 'manager.explorer.attributes.fields.sizeBytes',
  step: 'manager.explorer.attributes.fields.step',
  unit: 'manager.explorer.attributes.fields.unit',
  offset_unit: 'manager.explorer.attributes.fields.offsetUnit',
  header_bytes: 'manager.explorer.attributes.fields.headerBytes',
  index_count: 'manager.explorer.attributes.fields.indexCount',
  indexes: 'manager.explorer.attributes.fields.indexes',
  is_sampled: 'manager.explorer.attributes.fields.isSampled',
  schema_type: 'manager.explorer.attributes.fields.schemaType',
  sample_size: 'manager.explorer.attributes.fields.sampleSize',
  avg_doc_size: 'manager.explorer.attributes.fields.avgDocSize',
  page_size: 'manager.explorer.attributes.fields.pageSize',
  page_count: 'manager.explorer.attributes.fields.pageCount',
  table_count: 'manager.explorer.attributes.fields.tableCount',
  view_count: 'manager.explorer.attributes.fields.viewCount',
  entry_count: 'manager.explorer.attributes.fields.entryCount',
  directory_count: 'manager.explorer.attributes.fields.directoryCount',
  sampled_children: 'manager.explorer.attributes.fields.sampledChildren',
  children_truncated: 'manager.explorer.attributes.fields.childrenTruncated',
  sheet_count: 'manager.explorer.attributes.fields.sheetCount',
  default_sheet: 'manager.explorer.attributes.fields.defaultSheet',
  sampled_sheets: 'manager.explorer.attributes.fields.sampledSheets',
  files: 'manager.explorer.attributes.fields.files',
  anchors: 'manager.explorer.attributes.fields.anchors',
  content_index: 'manager.explorer.attributes.fields.contentIndex',
  schema_version: 'manager.explorer.attributes.fields.schemaVersion',
  model: 'manager.explorer.attributes.fields.graphModel',
  directed: 'manager.explorer.attributes.fields.graphDirected',
  node_count: 'manager.explorer.attributes.fields.graphNodeCount',
  relationship_count: 'manager.explorer.attributes.fields.graphRelationshipCount'
}

const boxedNestedGroupKeys = new Set(['native', 'source'])

const tableColumnLabelKeys = {
  name: 'manager.explorer.attributes.tableColumns.name',
  type: 'manager.explorer.attributes.tableColumns.type',
  native_type: 'manager.explorer.attributes.tableColumns.nativeType',
  child_kind: 'manager.explorer.attributes.tableColumns.childKind',
  data_type: 'manager.explorer.attributes.tableColumns.dataType',
  format: 'manager.explorer.attributes.tableColumns.format',
  path: 'manager.explorer.attributes.tableColumns.path',
  role: 'manager.explorer.attributes.tableColumns.role',
  extension: 'manager.explorer.attributes.tableColumns.extension',
  required: 'manager.explorer.attributes.tableColumns.required',
  primary: 'manager.explorer.attributes.tableColumns.primary',
  row_count: 'manager.explorer.attributes.tableColumns.rowCount',
  column_count: 'manager.explorer.attributes.tableColumns.columnCount',
  count: 'manager.explorer.attributes.tableColumns.count',
  labels: 'manager.explorer.attributes.tableColumns.labels',
  patterns: 'manager.explorer.attributes.tableColumns.patterns',
  properties: 'manager.explorer.attributes.tableColumns.properties',
  geometry_type: 'manager.explorer.attributes.tableColumns.geometryType',
  srid: 'manager.explorer.attributes.tableColumns.srid',
  dimension: 'manager.explorer.attributes.tableColumns.dimension',
  nullable: 'manager.explorer.attributes.tableColumns.nullable',
  primary_key: 'manager.explorer.attributes.tableColumns.primaryKey',
  comment: 'manager.explorer.attributes.tableColumns.comment'
}

const tableColumnLabelProfiles = {
  fields: {
    name: 'manager.explorer.attributes.tableColumns.fieldName',
    type: 'manager.explorer.attributes.tableColumns.fieldType',
    native_type: 'manager.explorer.attributes.tableColumns.nativeType'
  },
  children: {
    name: 'manager.explorer.attributes.tableColumns.childName',
    child_kind: 'manager.explorer.attributes.tableColumns.childKind',
    data_type: 'manager.explorer.attributes.tableColumns.childDataType',
    table: 'manager.explorer.attributes.tableColumns.tableName'
  },
  graphNodeShapes: {
    name: 'manager.explorer.attributes.tableColumns.nodeShape',
    count: 'manager.explorer.attributes.tableColumns.count',
    labels: 'manager.explorer.attributes.tableColumns.labels',
    properties: 'manager.explorer.attributes.tableColumns.properties'
  },
  graphRelationshipShapes: {
    type: 'manager.explorer.attributes.tableColumns.relationshipType',
    count: 'manager.explorer.attributes.tableColumns.count',
    patterns: 'manager.explorer.attributes.tableColumns.patterns',
    properties: 'manager.explorer.attributes.tableColumns.properties'
  }
}

const tableTitleLabelKeys = {
  'type_info.graph.node_shapes': 'manager.explorer.attributes.tables.graphNodeShapes',
  'type_info.graph.relationship_shapes': 'manager.explorer.attributes.tables.graphRelationshipShapes'
}

const itemTypeLabel = computed(() => {
  const typeI18nKey = itemMeta.value?.item_type_i18n_key
  const itemType = itemMeta.value?.item_type
  const key = typeI18nKey || (itemType ? `engine.term.${itemType}` : '')
  if (!key) return '-'

  const translated = t(key)
  return translated === key ? (itemType || '-') : translated
})

const overviewItems = computed(() => {
  if (!itemMeta.value) return []
  const items = [
    {
      key: 'item_type',
      label: t('meta.itemType'),
      value: itemTypeLabel.value
    },
    {
      key: 'full_name',
      label: t('meta.fullName'),
      value: formatScalar(itemMeta.value.full_name)
    },
    {
      key: 'scanned_at',
      label: t('meta.scannedAt'),
      value: formatScalar(itemMeta.value.scanned_at)
    }
  ]
  if (itemRowCount.value !== null) {
    items.splice(2, 0, {
      key: 'row_count',
      label: t('manager.explorer.attributes.fields.rowCount'),
      value: formatScalar(itemRowCount.value)
    })
  }
  return items
})

const itemAttributesMap = computed(() => {
  const attrs = {}
  for (const attr of itemMeta.value?.attributes || []) {
    attrs[attr.key] = attr.value
  }
  return attrs
})

const itemRowCount = computed(() => {
  const directRowCount = Number(itemMeta.value?.row_count)
  if (Number.isFinite(directRowCount)) return directRowCount
  const previewRowCount = Number(props.previewData?.total)
  if (Number.isFinite(previewRowCount) && previewRowCount > 0) return previewRowCount
  return pickNestedNumber(itemAttributesMap.value, [
    ['type_info', 'table', 'row_count']
  ])
})

const rawAttributesJson = computed(() => {
  const attrs = itemAttributesMap.value
  return JSON.stringify(attrs, null, 2)
})

const attributeSections = computed(() => {
  const attrs = itemMeta.value?.attributes || []
  return [...attrs]
    .sort((a, b) => compareKeys(a.key, b.key, sectionOrder))
    .map(buildAttributeSection)
    .filter(Boolean)
})

const buildAttributeSection = (attr) => {
  if (isEmptyAttributeValue(attr.value)) return null

  const pathParts = [attr.key]
  const directEntries = []
  const groups = []

  if (isPlainObject(attr.value)) {
    const keys = Object.keys(attr.value).sort((a, b) => compareKeys(a, b, groupOrder))
    keys.forEach(key => {
      const childValue = attr.value[key]
      if (isEmptyAttributeValue(childValue)) return
      if (isStructuredGroupValue(childValue)) {
        const group = buildAttributeGroup([...pathParts, key], childValue)
        if (group.count > 0) groups.push(group)
      } else {
        directEntries.push(...flattenAttributeValue(childValue, [...pathParts, key], pathParts))
      }
    })
  } else {
    directEntries.push(...flattenAttributeValue(attr.value, pathParts, []))
  }

  const count = directEntries.length + groups.reduce((total, group) => total + group.count, 0)
  if (count === 0) return null

  return {
    key: attr.key,
    title: translateFromMap(sectionLabelKeys, attr.key, humanizeKey(attr.key)),
    count,
    entries: directEntries,
    groups
  }
}

const buildAttributeGroup = (pathParts, value) => {
  if (isGraphInfoGroup(pathParts, value)) {
    return buildGraphAttributeGroup(pathParts, value)
  }

  const tables = []
  const subgroups = []
  let groupValue = value

  if (isTableRows(value)) {
    tables.push(buildObjectArrayTable(pathParts, value, preferredColumnsForObjectTable(pathParts)))
    groupValue = []
  } else if (isPlainObject(value)) {
    groupValue = { ...value }
    if (isTableRows(value.fields)) {
      tables.push(buildFieldTable([...pathParts, 'fields'], value.fields))
      delete groupValue.fields
    }
    Object.keys(value).forEach(key => {
      if (key === 'fields') return
      const childValue = value[key]
      if (!isTableRows(childValue)) return
      tables.push(buildObjectArrayTable(
        [...pathParts, key],
        childValue,
        preferredColumnsForObjectTable([...pathParts, key])
      ))
      delete groupValue[key]
    })
    boxedNestedGroupKeys.forEach(key => {
      const childValue = groupValue[key]
      if (!isStructuredGroupValue(childValue)) return
      const subgroup = buildAttributeGroup([...pathParts, key], childValue)
      if (subgroup.count > 0) subgroups.push(subgroup)
      delete groupValue[key]
    })
  }

  const entries = flattenAttributeValue(groupValue, pathParts, pathParts)
  const tableCount = tables.reduce((total, table) => total + table.rows.length, 0)
  const subgroupCount = subgroups.reduce((total, subgroup) => total + subgroup.count, 0)
  const count = entries.length + tableCount + subgroupCount

  return {
    key: pathParts.join('.'),
    path: pathParts.join('.'),
    title: translateFromMap(groupLabelKeys, pathParts[pathParts.length - 1], humanizeKey(pathParts[pathParts.length - 1])),
    count,
    entries,
    tables,
    subgroups
  }
}

const buildGraphAttributeGroup = (pathParts, value) => {
  const entries = ['model', 'directed', 'node_count', 'relationship_count']
    .filter(key => !isEmptyAttributeValue(value[key]))
    .map(key => buildEntry([...pathParts, key], value[key], pathParts))

  const tables = []
  const nodeShapes = Array.isArray(value.node_shapes)
    ? value.node_shapes.map(normalizeGraphNodeShapeRow).filter(isPlainObject)
    : []
  if (nodeShapes.length) {
    tables.push(buildObjectArrayTable(
      [...pathParts, 'node_shapes'],
      nodeShapes,
      preferredColumnsForObjectTable([...pathParts, 'node_shapes'])
    ))
  }

  const relationshipShapes = Array.isArray(value.relationship_shapes)
    ? value.relationship_shapes.map(normalizeGraphRelationshipShapeRow).filter(isPlainObject)
    : []
  if (relationshipShapes.length) {
    tables.push(buildObjectArrayTable(
      [...pathParts, 'relationship_shapes'],
      relationshipShapes,
      preferredColumnsForObjectTable([...pathParts, 'relationship_shapes'])
    ))
  }

  const tableCount = tables.reduce((total, table) => total + table.rows.length, 0)
  return {
    key: pathParts.join('.'),
    path: pathParts.join('.'),
    title: translateFromMap(groupLabelKeys, 'graph', humanizeKey('graph')),
    count: entries.length + tableCount,
    entries,
    tables,
    subgroups: []
  }
}

const normalizeGraphNodeShapeRow = (shape) => {
  if (!isPlainObject(shape)) return null
  return {
    name: shape.name,
    count: shape.count,
    labels: shape.labels,
    properties: visibleGraphShapeProperties(shape.properties)
  }
}

const normalizeGraphRelationshipShapeRow = (shape) => {
  if (!isPlainObject(shape)) return null
  return {
    type: shape.type,
    count: shape.count,
    patterns: shape.patterns,
    properties: visibleGraphShapeProperties(shape.properties)
  }
}

const flattenAttributeValue = (value, pathParts = [], groupRoot = []) => {
  if (isEmptyAttributeValue(value)) {
    return []
  }

  if (value === null || value === undefined) {
    return pathParts.length ? [buildEntry(pathParts, value, groupRoot)] : []
  }

  if (Array.isArray(value)) {
    if (value.length === 0) return pathParts.length ? [buildEntry(pathParts, value, groupRoot)] : []
    if (value.every(isScalar)) {
      return pathParts.length ? [buildEntry(pathParts, value, groupRoot)] : []
    }
    const includeIndex = value.length > 1
    return value.flatMap((item, index) => flattenAttributeValue(
      item,
      includeIndex ? [...pathParts, String(index)] : pathParts,
      groupRoot
    ))
  }

  if (typeof value === 'object') {
    const keys = Object.keys(value).sort((a, b) => compareKeys(a, b, fieldOrder))
    if (keys.length === 0) return pathParts.length ? [buildEntry(pathParts, value, groupRoot)] : []
    return keys.flatMap(key => {
      return flattenAttributeValue(value[key], [...pathParts, key], groupRoot)
    })
  }

  return pathParts.length ? [buildEntry(pathParts, value, groupRoot)] : []
}

const buildEntry = (pathParts, value, groupRoot = []) => {
  const display = formatScalar(value)
  const relativeParts = trimPathPrefix(pathParts, groupRoot)
  return {
    path: pathParts.join('.'),
    label: formatAttributePath(relativeParts.length ? relativeParts : pathParts),
    display,
    title: display
  }
}

const buildFieldTable = (pathParts, rows) => {
  const preferredColumns = ['name', 'type', 'native_type', 'nullable', 'primary_key', 'comment']
  return buildObjectArrayTable(pathParts, rows, preferredColumns, 'fields')
}

const preferredColumnsForObjectTable = (pathParts) => {
  const key = pathParts[pathParts.length - 1]
  if (isGraphRelationshipShapesTable(pathParts)) {
    return ['type', 'count', 'patterns', 'properties']
  }
  if (isGraphNodeShapesTable(pathParts)) {
    return ['name', 'count', 'labels', 'properties']
  }
  switch (key) {
    case 'refs':
      return ['path', 'role', 'extension', 'required', 'primary']
    case 'geometry_columns':
      return ['name', 'type', 'geometry_type', 'srid', 'dimension', 'nullable', 'primary']
    case 'indexes':
      return ['name', 'fields', 'is_unique', 'index_type']
    case 'children':
      return ['name', 'child_kind', 'data_type', 'row_count', 'column_count', 'table', 'format', 'path']
    default:
      return ['name', 'type', 'path', 'role', 'format', 'data_type']
  }
}

const tableLabelProfileForPath = (pathParts) => {
  if (isGraphNodeShapesTable(pathParts)) return 'graphNodeShapes'
  if (isGraphRelationshipShapesTable(pathParts)) return 'graphRelationshipShapes'
  const key = pathParts[pathParts.length - 1]
  if (key === 'fields') return 'fields'
  if (key === 'children') return 'children'
  return ''
}

const buildObjectArrayTable = (pathParts, rows, preferredColumns = [], labelProfile = tableLabelProfileForPath(pathParts)) => {
  const rowObjects = rows.filter(isPlainObject)
  const discoveredColumns = [...new Set(rowObjects.flatMap(row => Object.keys(row)))]
  const columns = [
    ...preferredColumns.filter(key => discoveredColumns.includes(key)),
    ...discoveredColumns.filter(key => !preferredColumns.includes(key)).sort()
  ]

  return {
    path: pathParts.join('.'),
    title: tableTitleForPath(pathParts),
    rows: rowObjects.map(row => {
      const formatted = {}
      columns.forEach(column => {
        formatted[column] = formatTableCell(row[column], pathParts, column)
      })
      return formatted
    }),
    columns: columns.map(column => ({
      key: column,
      label: tableColumnLabel(labelProfile, column),
      minWidth: tableColumnMinWidth(pathParts, column),
      className: tableColumnClassName(pathParts, column)
    }))
  }
}

const tableTitleForPath = (pathParts) => {
  const path = pathParts.join('.')
  const i18nKey = tableTitleLabelKeys[path]
  if (i18nKey) {
    const translated = t(i18nKey)
    if (translated !== i18nKey) return translated
  }
  return formatAttributeSegment(pathParts[pathParts.length - 1])
}

const tableColumnLabel = (profile, column) => {
  const profileMap = tableColumnLabelProfiles[profile]
  if (profileMap?.[column]) {
    const translated = t(profileMap[column])
    if (translated !== profileMap[column]) return translated
  }
  return translateFromMap(tableColumnLabelKeys, column, formatAttributeSegment(column))
}

const tableColumnMinWidth = (pathParts, column) => {
  if (isGraphNodeShapesTable(pathParts)) {
    return ({
      name: 180,
      count: 96,
      labels: 180,
      properties: 260
    })[column] || 140
  }
  if (isGraphRelationshipShapesTable(pathParts)) {
    return ({
      type: 170,
      count: 96,
      patterns: 320,
      properties: 260
    })[column] || 140
  }
  return 120
}

const tableColumnClassName = (pathParts, column) => {
  if ((isGraphNodeShapesTable(pathParts) || isGraphRelationshipShapesTable(pathParts)) && ['properties', 'patterns'].includes(column)) {
    return 'attribute-table-cell-muted'
  }
  return ''
}

const formatTableCell = (value, pathParts, column) => {
  if (isGraphRelationshipShapesTable(pathParts) && column === 'patterns') {
    return formatGraphPatterns(value)
  }
  if (isGraphNodeShapesTable(pathParts) && column === 'name') {
    return formatScalar(value)
  }
  if (isGraphNodeShapesTable(pathParts) && column === 'labels') {
    return formatGraphLabels(value)
  }
  if ((isGraphNodeShapesTable(pathParts) || isGraphRelationshipShapesTable(pathParts)) && column === 'properties') {
    return formatGraphProperties(value)
  }
  return formatScalar(value)
}

const isGraphInfoGroup = (pathParts, value) => {
  return pathParts.join('.') === 'type_info.graph' && isPlainObject(value)
}

const isGraphNodeShapesTable = (pathParts) => pathParts.join('.') === 'type_info.graph.node_shapes'
const isGraphRelationshipShapesTable = (pathParts) => pathParts.join('.') === 'type_info.graph.relationship_shapes'

const formatGraphLabels = (labels) => {
  if (!Array.isArray(labels) || labels.length === 0) return '-'
  const values = labels.map(value => String(value || '').trim()).filter(Boolean)
  return values.length ? values.join('+') : '-'
}

const formatGraphPatterns = (patterns) => {
  if (!Array.isArray(patterns) || patterns.length === 0) return '-'
  const formatted = patterns.map(pattern => {
    const from = formatGraphEndpoint(pattern?.from)
    const to = formatGraphEndpoint(pattern?.to)
    const count = Number(pattern?.count)
    const suffix = Number.isFinite(count) ? ` (${count})` : ''
    if (!from && !to) return suffix ? suffix.trim() : ''
    return `${from || '-'} -> ${to || '-'}${suffix}`
  }).filter(Boolean)
  if (formatted.length === 0) return '-'
  if (formatted.length <= 3) return formatted.join('; ')
  return `${formatted.slice(0, 3).join('; ')} ... +${formatted.length - 3}`
}

const formatGraphEndpoint = (endpoint) => {
  if (!endpoint || typeof endpoint !== 'object') return ''
  if (endpoint.shape_name) return String(endpoint.shape_name)
  if (Array.isArray(endpoint.labels) && endpoint.labels.length > 0) {
    return endpoint.labels.map(String).filter(Boolean).join('+')
  }
  return ''
}

const formatGraphProperties = (properties) => {
  if (!Array.isArray(properties) || properties.length === 0) return '-'
  const names = properties
    .map(prop => isPlainObject(prop) ? prop.name : prop)
    .map(value => String(value || '').trim())
    .filter(value => value && isVisibleGraphPropertyName(value))
  if (names.length === 0) return '-'
  if (names.length <= 8) return names.join(', ')
  return `${names.slice(0, 8).join(', ')} ... +${names.length - 8}`
}

const visibleGraphShapeProperties = (properties) => {
  if (!Array.isArray(properties)) return []
  return properties
    .filter(prop => {
      const name = isPlainObject(prop) ? prop.name : prop
      return isVisibleGraphPropertyName(name)
    })
    .map(prop => isPlainObject(prop) ? prop : { name: String(prop) })
}

const isVisibleGraphPropertyName = (name) => {
  const normalized = String(name || '').trim().toLowerCase()
  if (!normalized) return false
  if (['name', 'title', 'label', 'id'].includes(normalized)) return false
  if (normalized.startsWith('_')) return false
  if (normalized.endsWith('_at') || normalized.endsWith('_time')) return false
  if (normalized.includes('encoder') || normalized.includes('geocoder')) return false
  if (normalized.includes('config')) return false
  return true
}

const isPlainObject = (value) => {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
}

const isStructuredGroupValue = (value) => {
  return isPlainObject(value) || (Array.isArray(value) && value.some(item => item && typeof item === 'object'))
}

const isTableRows = (value) => {
  return Array.isArray(value) && value.length > 0 && value.every(isPlainObject)
}

const isScalar = (value) => {
  return value === null || value === undefined || typeof value !== 'object'
}

const isEmptyAttributeValue = (value) => {
  if (value === null || value === undefined || value === '') return true
  if (Array.isArray(value)) return value.length === 0 || value.every(isEmptyAttributeValue)
  if (isPlainObject(value)) return Object.keys(value).length === 0 || Object.values(value).every(isEmptyAttributeValue)
  return false
}

const formatScalar = (value) => {
  if (value === null || value === undefined || value === '') return '-'
  if (Array.isArray(value)) return value.length ? value.map(formatScalar).join(', ') : '-'
  if (typeof value === 'object') return JSON.stringify(value)
  if (typeof value === 'boolean') {
    return value ? t('manager.explorer.booleanYes') : t('manager.explorer.booleanNo')
  }
  if (typeof value === 'string' && /[\r\n\t]/.test(value)) {
    return JSON.stringify(value).slice(1, -1)
  }
  return String(value)
}

const trimPathPrefix = (pathParts, prefixParts) => {
  if (!prefixParts.length) return pathParts
  const matches = prefixParts.every((part, index) => pathParts[index] === part)
  return matches ? pathParts.slice(prefixParts.length) : pathParts
}

const formatAttributePath = (pathParts) => {
  if (!pathParts.length) return '-'
  return pathParts
    .map(part => formatAttributeSegment(part))
    .join(' / ')
}

const formatAttributeSegment = (segment) => {
  if (/^\d+$/.test(segment)) {
    return `#${Number(segment) + 1}`
  }
  return translateFromMap(fieldLabelKeys, segment, humanizeKey(segment))
}

const translateFromMap = (map, key, fallback) => {
  const i18nKey = map[key]
  if (!i18nKey) return fallback
  const translated = t(i18nKey)
  return translated === i18nKey ? fallback : translated
}

const humanizeKey = (key) => {
  if (!key) return '-'
  return String(key)
    .replace(/[_-]+/g, ' ')
    .replace(/\b\w/g, char => char.toUpperCase())
}

const compareKeys = (a, b, order) => {
  const indexA = order.indexOf(a)
  const indexB = order.indexOf(b)
  if (indexA !== -1 && indexB === -1) return -1
  if (indexA === -1 && indexB !== -1) return 1
  if (indexA !== -1 && indexB !== -1) return indexA - indexB
  return String(a).localeCompare(String(b))
}

const pickNestedNumber = (source, paths) => {
  for (const path of paths) {
    let current = source
    for (const segment of path) {
      current = current?.[segment]
    }
    if (current === null || current === undefined || current === '') continue
    const value = Number(current)
    if (Number.isFinite(value)) return value
  }
  return null
}
</script>

<style scoped>
.item-panel {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  background: var(--addp-bg-primary);
}

.item-tab-bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  gap: 4px;
  min-height: 42px;
  padding: 0 14px;
  background: var(--addp-bg-primary);
  border-bottom: 1px solid var(--addp-border-color);
}

.item-tab-button {
  height: 42px;
  padding: 0 14px;
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
  color: var(--addp-text-secondary);
  font: inherit;
  font-weight: 600;
  cursor: pointer;
}

.item-tab-button:hover {
  color: var(--el-color-primary);
}

.item-tab-button.active {
  color: var(--el-color-primary);
  border-bottom-color: var(--el-color-primary);
}

.item-tab-content {
  flex: 1;
  min-height: 0;
  overflow: hidden;
}

.preview-tab-pane {
  height: 100%;
  min-height: 0;
}

.attributes-tab-pane {
  height: 100%;
  overflow: auto;
  background: var(--addp-bg-primary);
}

.attributes-panel {
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.attributes-toolbar {
  display: flex;
  justify-content: flex-end;
}

.meta-overview {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 10px;
}

.overview-item {
  min-width: 0;
  padding: 10px 12px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 8px;
  background: var(--addp-bg-secondary);
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.overview-label {
  font-size: 12px;
  color: var(--addp-text-secondary);
}

.overview-value {
  color: var(--addp-text-primary);
  font-weight: 600;
  line-height: 1.5;
  word-break: break-word;
}

.attribute-sections {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.attribute-card {
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-primary);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.attribute-card-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.attribute-section-title {
  font-size: 14px;
  font-weight: 600;
  color: var(--addp-text-primary);
  line-height: 1.5;
}

.attribute-section-key {
  margin-top: 2px;
  font-size: 12px;
  color: var(--addp-text-tertiary);
  word-break: break-word;
}

.attribute-items {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
  gap: 10px;
}

.attribute-groups {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.attribute-group {
  padding: 12px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 8px;
  background: var(--addp-bg-secondary);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.attribute-subgroups {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.attribute-subgroup {
  padding: 10px;
  border: 1px solid var(--addp-border-color);
  border-radius: 6px;
  background: var(--addp-bg-primary);
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.attribute-subgroup-items .attribute-item {
  background: var(--addp-bg-secondary);
}

.attribute-table-wrap {
  overflow: auto;
  border-radius: 6px;
}

.attribute-table-title {
  margin-bottom: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--addp-text-secondary);
}

.attribute-table {
  width: 100%;
  background: var(--addp-bg-primary);
}

.attribute-table :deep(.el-table__header th),
.attribute-table :deep(.el-table__body td) {
  background: var(--addp-bg-primary);
  color: var(--addp-text-primary);
}

.attribute-table :deep(.el-table__body tr:hover > td) {
  background: var(--addp-bg-secondary);
}

.attribute-table :deep(.attribute-table-cell-muted) {
  color: var(--addp-text-secondary);
}

.attribute-group-header {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 10px;
}

.attribute-group-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--addp-text-primary);
}

.attribute-group-key {
  font-size: 12px;
  color: var(--addp-text-tertiary);
  word-break: break-word;
}

.attribute-item {
  min-width: 0;
  padding: 10px;
  border: 1px solid var(--addp-border-color-light);
  border-radius: 6px;
  background: var(--addp-bg-primary);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.attribute-key {
  font-size: 12px;
  font-weight: 600;
  color: var(--addp-text-primary);
  line-height: 1.5;
  word-break: break-word;
}

.attribute-value {
  color: var(--addp-text-secondary);
  font-size: 12px;
  line-height: 1.5;
  word-break: break-word;
  display: -webkit-box;
  -webkit-line-clamp: 4;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.attribute-json {
  max-height: 62vh;
  overflow: auto;
  margin: 0;
  padding: 12px;
  border: 1px solid var(--addp-border-color);
  border-radius: 8px;
  background: var(--addp-bg-secondary);
  color: var(--addp-text-primary);
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
</style>
