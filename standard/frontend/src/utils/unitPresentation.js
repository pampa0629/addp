// 平台译名是运行时数据；Tenant 自定义文本保留原始输入。
export function presentUnitEntry(entry, language) {
  if (!entry) return entry
  const text = entry.scope_type === 'platform'
    ? (entry.translations?.[language] ?? entry.translations?.['zh-cn'])
    : null
  return {
    ...entry,
    ...(text ?? {}),
    ...(entry.category ? { category: presentUnitEntry(entry.category, language) } : {})
  }
}
export function copyUnitTranslations(value) {
  return Object.fromEntries(Object.entries(value ?? { 'zh-cn': { name: '', description: '', symbol: '' }, en: { name: '', description: '', symbol: '' } })
    .map(([language, text]) => [language, { ...text }]))
}
export function validUnitTranslations(value) {
  return ['zh-cn', 'en'].every(language => value?.[language]?.name?.trim())
    && Object.entries(value).every(([language, text]) => /^[a-z]{2,3}(-[a-z0-9]{2,8})*$/.test(language) && text.name?.trim())
}
