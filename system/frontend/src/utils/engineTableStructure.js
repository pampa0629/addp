import { parseEngineCapabilities, engineCapabilityFamily, hasLiveEngineCatalogCapability, safePickerEngineID } from '@common-ui'
import { approvalTargetsEqual as catalogPathsEqual } from './engineApprovalCatalog'

export function supportsEngineTableStructure(engine) {
  const storage = parseEngineCapabilities(engine)?.storage, model = storage?.catalog_model
  return engineCapabilityFamily(engine) === 'tabular' && hasLiveEngineCatalogCapability(engine) &&
    storage.facts?.supported === true && storage.facts.field_info === true &&
    model.path_version === 'catalog.path/v1' && model.root_term === 'server' &&
    Array.isArray(model.levels) && model.levels.length > 0 &&
    model.levels.at(-1)?.role === 'leaf' && model.levels.at(-1)?.term === 'table' &&
    model.levels.every(level => level && Array.isArray(level.kinds)) &&
    model.levels.slice(0, -1).every(level => level.role === 'branch') && model.levels.at(-1).kinds.includes('table')
}

export function isStructureTableEntry(engine, entry) {
  if (!supportsEngineTableStructure(engine) || entry?.role !== 'leaf' || entry.term !== 'table') return false
  const model = parseEngineCapabilities(engine).storage.catalog_model, path = entry.path
  const segments = path?.segments
  return path?.version === model.path_version && path.engine_id === safePickerEngineID(engine.id) &&
    Array.isArray(segments) && segments.length === model.levels.length + 1 &&
    segments[0]?.term === model.root_term && segments[0].kind === 'server' && segments[0].name === '' &&
    model.levels.every((level, i) => level && segments[i + 1]?.term === level.term && level.kinds?.includes(segments[i + 1].kind) &&
      typeof segments[i + 1].name === 'string' && segments[i + 1].name.length > 0) &&
    entry.kind === segments.at(-1).kind
}

// Only structural fields cross into the view; do not retain sample/statistics or free-form attributes.
export function tableStructureFields(engine, entry, facts) {
  if (!isStructureTableEntry(engine, entry) || facts?.kind !== entry.kind ||
      !catalogPathsEqual(facts?.path, entry.path) ||
      !Array.isArray(facts?.table?.fields)) throw new Error('invalidTableStructure')
  const names = new Set()
  return facts.table.fields.map(field => {
    if (typeof field?.name !== 'string' || !field.name || names.has(field.name) || typeof field.nullable !== 'boolean' ||
        (field.primary_key !== undefined && typeof field.primary_key !== 'boolean')) throw new Error('invalidTableStructure')
    names.add(field.name)
    return { name: field.name, type: typeof field.type === 'string' ? field.type : '',
      native_type: typeof field.native_type === 'string' ? field.native_type : '', nullable: field.nullable,
      primary_key: field.primary_key === true || facts.table.primary_key?.includes(field.name) === true }
  })
}
