const resolveLocale = locale => locale === 'en' ? 'en' : 'zh-CN'

const toValidDate = value => {
  if (!value) return null
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export const formatStandardDateTime = (value, locale) => {
  const date = toValidDate(value)
  return date ? date.toLocaleString(resolveLocale(locale)) : '-'
}

export const formatStandardDate = (value, locale) => {
  const date = toValidDate(value)
  return date ? date.toLocaleDateString(resolveLocale(locale)) : '-'
}
