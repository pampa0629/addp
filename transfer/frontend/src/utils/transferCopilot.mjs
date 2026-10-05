import { resourceFact } from '@addp/common-frontend/basic/src/utils/resourceCandidateSelection.mjs'
import { parseLocator } from '@addp/common-frontend/basic/src/types/resourceLocator.js'

export {
  groupResourceCandidates,
  resourceCandidateKey,
  resourceFact
} from '@addp/common-frontend/basic/src/utils/resourceCandidateSelection.mjs'

function invalidOwnerResponse() {
  return new Error('transfer_source_owner_response_invalid')
}

export async function verifyTransferSource(candidate, { ancestors, facts }) {
  const engineID = Number(candidate?.engine_id)
  let locatorEngineID
  try { locatorEngineID = parseLocator(candidate?.locator).engineId } catch { throw invalidOwnerResponse() }
  if (!Number.isInteger(engineID) || engineID <= 0 || locatorEngineID !== engineID) {
    throw invalidOwnerResponse()
  }
  const [path, schema] = await Promise.all([
    ancestors(engineID, candidate.locator), facts(candidate.locator)
  ])
  if (path?.target_locator !== candidate.locator || schema?.locator !== candidate.locator ||
      schema.engine_id !== engineID || !schema.data_type ||
      (schema.fields !== undefined && (!Array.isArray(schema.fields) || schema.fields.length > 200))) {
    throw invalidOwnerResponse()
  }
  // 不复制 Owner 响应中的身份或凭据字段，只转交规范 ResourceFact。
  return {
    ...candidate,
    ...resourceFact({ ...schema, role: candidate.role, fields: schema.fields ?? [] }),
    ancestors: Array.isArray(path.ancestors) ? path.ancestors : []
  }
}

export async function discoverTransferSources(intents, owners, engineIDs = []) {
  if (!Array.isArray(intents) || intents.length !== 1 || !intents[0]?.role ||
      !Array.isArray(intents[0].search_queries) || !intents[0].search_queries.length || intents[0].search_queries.length > 8) {
    throw new Error('transfer_source_intent_invalid')
  }
  const intent = intents[0]
  const candidates = []
  const seen = new Set()
  const scopes = engineIDs.length ? [...new Set(engineIDs)] : [undefined]
  for (const query of intent.search_queries) {
    if (typeof query !== 'string' || !query.trim()) throw new Error('transfer_source_intent_invalid')
    for (const engineID of scopes) {
      const result = await owners.search(query, engineID)
      if (!Array.isArray(result?.results)) throw invalidOwnerResponse()
      for (const hit of result.results.slice(0, 20)) {
        if (!hit?.locator || !Number.isInteger(hit.engine_id) || hit.engine_id <= 0 ||
            (engineID !== undefined && hit.engine_id !== engineID) || seen.has(hit.locator)) continue
        const candidate = await verifyTransferSource({
          role: intent.role, locator: hit.locator, engine_id: hit.engine_id,
          name: hit.name || hit.full_name || hit.locator, engine_name: hit.engine_name,
          asset_type: hit.asset_type, path: hit.path,
          representation: hit.attributes?.representation, format: hit.attributes?.format
        }, owners)
        seen.add(hit.locator)
        candidates.push(candidate)
        if (candidates.length >= 20) return candidates
      }
    }
  }
  return candidates
}

export function inferTargetEngineFromPrompt(query, engines) {
  const targetText = targetClause(query)
  if (!targetText) return null
  return uniqueBestEngine(targetText, engines)
}

export function inferSourceEngineFromPrompt(query, engines) {
  const matches = inferSourceEnginesFromPrompt(query, engines)
  return matches.length === 1 ? matches[0] : null
}

export function inferSourceEnginesFromPrompt(query, engines) {
  const sourceText = sourceClause(query)
  if (!sourceText) return []
  return bestEngines(sourceText, engines)
}

export function inferTransferSyncMode(query, { sourceEngineType = '', sourceLocator = '' } = {}) {
  if (isKafkaTopic(sourceEngineType, sourceLocator)) return 'kafka'

  const text = String(query || '')
    .trim()
    .toLowerCase()
    .replace(/非实时|not\s+real[- ]?time/g, '')

  if (/(?:实时|持续|不间断|\bcdc\b|change\s+data\s+capture|\bcontinuous\b|real[- ]?time)/i.test(text)) {
    return 'cdc'
  }
  if (/(?:增量|水位|\bwatermark\b|\bincremental\b)/i.test(text)) {
    return 'incremental'
  }
  return 'snapshot'
}

export function resolveAuthoritativeSourceFields(candidateFields, metadataFields, itemID) {
  if (Number(itemID) > 0) {
    return Array.isArray(metadataFields) ? metadataFields : []
  }
  return Array.isArray(candidateFields) ? candidateFields : []
}

function uniqueBestEngine(text, engines) {
  const matches = bestEngines(text, engines)
  return matches.length === 1 ? matches[0] : null
}

function bestEngines(text, engines) {
  const ranked = (Array.isArray(engines) ? engines : [])
    .map(engine => ({ engine, score: targetEngineScore(text, engine) }))
    .filter(item => item.score > 0)
    .sort((left, right) => right.score - left.score)
  if (!ranked.length) return []
  return ranked.filter(item => item.score === ranked[0].score).map(item => item.engine)
}

export function needsTargetConfiguration(result) {
  return result?.status === 'need_clarification' && result?.clarification_reason === 'target_configuration_required'
}

export function inferTargetEngineForClarification(result, query, engines) {
  if (!needsTargetConfiguration(result)) return null
  return inferTargetEngineFromPrompt(query, engines)
}

function targetClause(query) {
  const text = String(query || '').trim().toLowerCase()
  if (!text) return ''
  const marker = /(?:目标(?:引擎|数据库|库)?\s*(?:是|为|到)?|目的地\s*(?:是|为|到)?|(?:到|至)\s*|\b(?:to|into)\s+)/g
  let lastMatch = null
  let current
  while ((current = marker.exec(text)) !== null) lastMatch = current
  return lastMatch ? text.slice(lastMatch.index + lastMatch[0].length) : ''
}

function sourceClause(query) {
  const text = String(query || '').trim().toLowerCase()
  if (!text) return ''
  const chinese = text.match(/(?:从|自)\s*(.+?)(?=\s*(?:到|至|->|向|同步|迁移|写入|导入)|[,，;；]|$)/)
  if (chinese?.[1]) return chinese[1]
  const english = text.match(/\bfrom\s+(.+?)\s+(?=to|into|sync|copy|transfer)\b/)
  return english?.[1] || ''
}

function targetEngineScore(targetText, engine) {
  const phrase = normalizeEngineText(targetText)
  if (!phrase || !engine) return 0
  const terms = [engine.engine_type, engine.name, engine.code, engine.display_name]
    .flatMap(value => normalizeEngineText(value).split(' '))
    .filter(term => term.length >= 2)
  let score = 0
  for (const term of new Set(terms)) {
    if (phrase.includes(term)) score = Math.max(score, term.length)
  }
  const shortTerms = phrase.split(' ').filter(term => term.length >= 2 && term.length <= 3)
  const engineValues = [engine.engine_type, engine.name, engine.code, engine.display_name]
    .map(normalizeEngineText)
    .filter(Boolean)
  for (const term of shortTerms) {
    if (engineValues.some(value => isSubsequence(term, value.replace(/\s+/g, '')))) {
      score = Math.max(score, term.length)
    }
  }
  const engineType = normalizeEngineText(engine.engine_type)
  if (engineType && phrase.includes(engineType)) score += 100
  return score
}

function isSubsequence(needle, haystack) {
  let index = 0
  for (const character of haystack) {
    if (character === needle[index]) index += 1
    if (index === needle.length) return true
  }
  return false
}

function normalizeEngineText(value) {
  return String(value || '')
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9\u4e00-\u9fff]+/g, ' ')
    .replace(/\s+/g, ' ')
}

function isKafkaTopic(engineType, locator) {
  if (normalizeEngineText(engineType) !== 'kafka') return false
  try {
    return String(new URL(String(locator || '')).searchParams.get('type') || '').trim().toLowerCase() === 'topic'
  } catch {
    return false
  }
}
