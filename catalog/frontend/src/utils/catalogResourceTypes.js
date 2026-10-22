export const CATALOG_ENTRY_TYPE_KEYS = Object.freeze({
 data_item: 'dataItem', business_entity: 'businessEntity', logical_model: 'logicalModel', metric: 'metric',
 data_service: 'dataService', development_artifact: 'developmentArtifact', data_application: 'dataApplication',
 glossary: 'glossary', element: 'element', code_set: 'codeSet', standard_document: 'standardDocument'
})
export function catalogEntryTypeLabel(type, t) {
 const key = CATALOG_ENTRY_TYPE_KEYS[type]
 return key ? t(`catalog.entryType.${key}`) : t('catalog.entries.unresolvedReference')
}
