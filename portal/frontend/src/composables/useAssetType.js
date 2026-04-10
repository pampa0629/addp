import { useI18n } from 'vue-i18n'

export function useAssetType() {
  const { t, te } = useI18n()

  function getTypeName(typeCode, typeName) {
    if (typeCode) {
      const key = `portal.assetType.${typeCode}`
      if (te(key)) return t(key)
    }
    return typeName || t('portal.common.unknownType')
  }

  return { getTypeName }
}
