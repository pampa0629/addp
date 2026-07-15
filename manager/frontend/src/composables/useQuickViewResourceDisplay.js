import { ref } from 'vue'
import { parseLocatorSafe } from '@addp/common-frontend'
import { dataExplorerAPI } from '../api/dataExplorer'
import {
  normalizeQuickViewEngines,
  quickViewDisplayText,
  quickViewEngineName,
  quickViewResourceLabel,
  quickViewResourcePath
} from '../utils/quickViewResourceDisplay'

export function useQuickViewResourceDisplay(t) {
  const engines = ref([])

  const loadQuickViewEngines = async () => {
    try {
      engines.value = normalizeQuickViewEngines(await dataExplorerAPI.getEngines())
    } catch (error) {
      console.error('加载快显引擎列表失败:', error)
      engines.value = []
    }
  }

  const engineName = (engineId) => (
    quickViewEngineName(engines.value, engineId) ||
    (Number(engineId || 0) ? t('manager.quickViewDisplay.unknownEngine') : '-')
  )

  const displayText = (value) => quickViewDisplayText(value, parseLocatorSafe) || '-'

  const resourcePath = (locator) => quickViewResourcePath(locator, parseLocatorSafe) || '-'

  const resourceLabel = (engineId, locator) => (
    quickViewResourceLabel(
      quickViewEngineName(engines.value, engineId) || (Number(engineId || 0) ? t('manager.quickViewDisplay.unknownEngine') : ''),
      quickViewResourcePath(locator, parseLocatorSafe)
    ) || '-'
  )

  return {
    displayText,
    engineName,
    loadQuickViewEngines,
    resourceLabel,
    resourcePath
  }
}
