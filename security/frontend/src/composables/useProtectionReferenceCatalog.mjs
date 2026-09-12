import { ref } from 'vue'

function normalizeList(value) {
  return Array.isArray(value) ? value : []
}

export function useProtectionReferenceCatalog({
  listSensitiveTypes,
  listClassifications,
  listGrades,
  listBaselines,
  listDetectorCapabilities,
  listEngines
}) {
  const sensitiveTypes = ref([])
  const securityClassifications = ref([])
  const securityGrades = ref([])
  const protectionBaselines = ref([])
  const detectorCapabilities = ref([])
  const engineNames = ref(new Map())

  let disposed = false
  let lifecycleRevision = 0
  let baselineRevision = 0
  let definitionsLoaded = false
  let detectorCapabilitiesLoaded = false
  let enginesLoaded = false
  let definitionsPromise = null
  let detectorCapabilitiesPromise = null
  let enginesPromise = null

  function isCurrent(request) {
    return !disposed && request === lifecycleRevision
  }

  function loadDefinitions() {
    if (disposed) return Promise.resolve(false)
    if (definitionsLoaded) return Promise.resolve(true)
    if (definitionsPromise) return definitionsPromise

    const request = lifecycleRevision
    const requestedBaselineRevision = baselineRevision
    const pending = Promise.all([
      listSensitiveTypes(),
      listClassifications(),
      listGrades(),
      listBaselines()
    ]).then(([types, classifications, grades, baselines]) => {
      if (!isCurrent(request)) return false
      sensitiveTypes.value = normalizeList(types)
      securityClassifications.value = normalizeList(classifications)
      securityGrades.value = normalizeList(grades)
      if (requestedBaselineRevision === baselineRevision) {
        protectionBaselines.value = normalizeList(baselines)
      }
      definitionsLoaded = true
      return true
    })
    const tracked = pending.finally(() => {
      if (definitionsPromise === tracked) definitionsPromise = null
    })
    definitionsPromise = tracked
    return tracked
  }

  function loadDetectorCapabilities() {
    if (disposed) return Promise.resolve(false)
    if (detectorCapabilitiesLoaded) return Promise.resolve(true)
    if (detectorCapabilitiesPromise) return detectorCapabilitiesPromise

    const request = lifecycleRevision
    const pending = Promise.resolve()
      .then(() => listDetectorCapabilities())
      .then(capabilities => {
        if (!isCurrent(request)) return false
        detectorCapabilities.value = normalizeList(capabilities)
        detectorCapabilitiesLoaded = true
        return true
      })
    const tracked = pending.finally(() => {
      if (detectorCapabilitiesPromise === tracked) detectorCapabilitiesPromise = null
    })
    detectorCapabilitiesPromise = tracked
    return tracked
  }

  function loadEngines() {
    if (disposed) return Promise.resolve(false)
    if (enginesLoaded) return Promise.resolve(true)
    if (enginesPromise) return enginesPromise

    const request = lifecycleRevision
    const pending = Promise.resolve()
      .then(() => listEngines())
      .then(engines => {
        if (!isCurrent(request)) return false
        engineNames.value = new Map(normalizeList(engines).map(engine => [Number(engine.id), engine.name]))
        enginesLoaded = true
        return true
      })
      .catch(() => {
        if (isCurrent(request)) engineNames.value = new Map()
        return false
      })
    const tracked = pending.finally(() => {
      if (enginesPromise === tracked) enginesPromise = null
    })
    enginesPromise = tracked
    return tracked
  }

  function setProtectionBaselines(baselines) {
    if (disposed) return false
    baselineRevision += 1
    protectionBaselines.value = normalizeList(baselines)
    return true
  }

  function dispose() {
    if (disposed) return
    lifecycleRevision += 1
    disposed = true
  }

  return {
    sensitiveTypes,
    securityClassifications,
    securityGrades,
    protectionBaselines,
    detectorCapabilities,
    engineNames,
    loadDefinitions,
    loadDetectorCapabilities,
    loadEngines,
    setProtectionBaselines,
    dispose
  }
}
