import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'

const editor = await readFile(resolve('src/views/QueryEditor.vue'), 'utf8')
const api = await readFile(resolve('src/api/copilot.js'), 'utf8')

assert.match(api, /generateQueryFromNL/)
assert.match(api, /\/copilot\/query\/generate/)
assert.doesNotMatch(api, /\/copilot\/sql\/generate/)
assert.match(editor, /generateQueryFromNL\(\{[\s\S]*query:[\s\S]*engine_id:[\s\S]*query_language:[\s\S]*resources[\s\S]*current_query/)
assert.match(editor, /selectedEngineId\.value/)
assert.match(editor, /currentQueryLanguage\.value/)
assert.match(editor, /collectSelectedQueryResources/)
assert.match(editor, /isQueryInputResource\(parsed\)/)
assert.match(editor, /resolveMQLQueryResources/)
assert.match(
  editor,
  /currentQueryLanguage\.value === 'mql'[\s\S]{0,160}selectedLocator[\s\S]{0,160}queryContent\.value\.trim\(\)[\s\S]{0,160}resolveMQLQueryResources\(selectedLocator\)/
)
assert.match(editor, /matchMQLCollectionReferences/)
assert.doesNotMatch(editor, /resourceCandidatesWithinScope/)
assert.match(editor, /getResourceTreeNode\('\/api\/v1\/meta', selected\.engineId, databaseLocator\)/)
assert.doesNotMatch(editor, /canUseQueryContainerContext/)
assert.match(editor, /selectQueryResourceOrDeclareCollection/)
assert.match(editor, /!queryContent\.value\.trim\(\)[\s\S]{0,500}submitQueryGeneration\(\[\], \{ resourceScopeLocator: selectedLocator \}\)/)
assert.match(editor, /clarification_answers:/)
assert.match(editor, /resource_scope_locator: resourceScopeLocator \|\| undefined/)
assert.match(editor, /selectedQueryContext/)
assert.match(editor, /confirmedResources\(/)
assert.match(editor, /formatGeneratedQueryForEditor\(resolved\.query, generatedLanguage\)/)
assert.match(editor, /queryContent\.value = generatedQuery/)
assert.match(editor, /queryParameters\.value = resolved\.queryParameters\.map/)
assert.doesNotMatch(editor, /submitQuery\(\{\}\)[\s\S]{0,200}resolved\.query/)
assert.match(editor, /class="query-ai-fab"/)
assert.match(editor, /t\('develop\.query\.format'\)[\s\S]*?<el-icon><Operation \/><\/el-icon>/)
assert.match(editor, /class="query-ai-fab"[\s\S]*?<el-icon><MagicStick \/><\/el-icon>/)
assert.match(editor, /v-model="queryClarificationVisible"/)
assert.match(editor, /clarification\.control === 'single_choice'/)
assert.match(editor, /clarification\.control === 'text'/)
assert.match(editor, /clarification\.control === 'notice'/)
assert.doesNotMatch(editor, /class="query-resource-candidate-path">\{\{ candidate\.locator \}\}/)
assert.match(editor, /queryResourceCandidateDatabase\(candidate\)/)
assert.match(editor, /collection: 'mongodbCollection'/)
assert.match(editor, /resourceTypes\.\$\{typeKey\}/)
assert.match(editor, /confirmedResources\([\s\S]*queryResourceCandidates/)
assert.doesNotMatch(editor, /clarificationKey/)

console.log('queryCopilotBehavior tests passed')
