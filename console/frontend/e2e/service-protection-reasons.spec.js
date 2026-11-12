import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import zh from '../../../service/frontend/src/i18n/zh-cn.json' with { type: 'json' }
import en from '../../../service/frontend/src/i18n/en.json' with { type: 'json' }

test.use({ locale: 'en-US' })

const definition = { id: 41, version: 1, title: 'Protected Service fixture', service_name: 'protected_reason_fixture', status: 'active', config_type: 'sql', engine_id: 1, sql_query: 'SELECT id, phone FROM public.persons', named_parameters: [], stable_key: ['id'], max_features: 100, protocols: {rest_api:{enabled:true}}, data_config: {protection_choices:[]}, output_contract: {table:{fields:[{name:'id',type:'bigint',nullable:false},{name:'phone',type:'string'}],primary_key:['id']}} }

for (const language of ['zh-cn', 'en']) {
  test(`Service preview preserves precise protection refusals and clears stale rows (${language})`, async ({ page }) => {
    const words = (language === 'en' ? en : zh).service.query
    const translations = readFileSync(`../../service/backend/i18n/locales/${language}.toml`, 'utf8')
    const message = key => {
      const section = translations.split(`[service.${key}]`)[1]
      const match = section?.match(/other = "([^"\n]+)"/)
      expect(match, key).not.toBeNull()
      return match[1]
    }
    let response = {status:200,json:{data:[{id:'1',phone:'138****0001'}],page:{limit:20,has_more:true,next_cursor:'old-sensitive-cursor'},service_version:'fixture-v1'}}
    const requests = []
    await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
    page.on('pageerror', error => { throw error })
    await page.route(url => url.pathname.startsWith('/api/'), async route => {
      const path = new URL(route.request().url()).pathname
      if(path === '/api/v1/service/query/41') return route.fulfill({json:definition})
      if(path === '/api/v1/system/engine-catalog/engines') return route.fulfill({json:[{id:1,name:'fixture-postgres',engine_type:'postgresql'}]})
      if(path === '/api/query/protected_reason_fixture/query') {
        requests.push({
          body: route.request().postDataJSON(),
          language: route.request().headers()['accept-language']
        })
        return route.fulfill(response)
      }
      throw Error(`Unexpected request ${route.request().method()} ${path}`)
    })
    await page.route('**/e2e/service-language-shell.html', route => route.fulfill({
      contentType: 'text/html',
      body: '<iframe title="Service" style="width:100%;height:95vh;border:0" src="/e2e/service-fixture/e2e/navigation.html#/query-services/41"></iframe>'
    }))
    await page.goto('/e2e/service-language-shell.html')
    const frame = page.frameLocator('iframe[title="Service"]')
    const card = frame.locator('.el-card').filter({has:frame.getByText(words.cardPreview,{exact:true})})
    const alert = card.locator('.preview-error')
    await card.getByRole('button',{name:words.loadData,exact:true}).click()
    await expect(card.getByText('138****0001',{exact:true})).toBeVisible()
    const reasons = ['not_ready','expired','not_yet_valid','schema_mismatch','lineage_unverified','computed_output_unsupported','operation_unsupported']
    for(const reason of reasons) {
      response = {status:403,json:{error_code:'data_protection_required',error:message(`err.protection_${reason}`)}}
      await card.getByRole('button',{name:reason===reasons[0]?words.refreshData:words.loadData,exact:true}).click()
      await expect(alert).toContainText(response.json.error)
      await expect(card.getByText('138****0001',{exact:true})).toHaveCount(0)
      await expect(card.locator('.preview-empty')).toHaveCount(0)
      if(reason===reasons[0]) {
        await expect(frame.locator('.el-message')).toHaveCount(0,{timeout:10000})
        await expect(alert).toBeVisible() // The error outlives the transient toast.
      }
    }
    for(const [status,code,key] of [[403,'query_protection_roles_unavailable','protection.roles_unavailable'],[409,'query_protection_choice_invalid','protection.choice_invalid'],[403,'permission_denied',null]]) {
      response = {status,json:{error_code:code,error:key?message(key):'Source data read access denied'}}
      await card.getByRole('button',{name:words.loadData,exact:true}).click()
      await expect(alert).toContainText(response.json.error)
      await expect(alert).not.toContainText(message('err.protection_operation_unsupported'))
    }
    response={status:403,json:{error_code:'data_protection_required',error:message('err.protection_schema_mismatch')}}
    await card.getByRole('button',{name:words.loadData,exact:true}).click()
    await expect(alert).toContainText(response.json.error)
    await expect(frame.locator('.el-message')).toHaveCount(0,{timeout:10000})
    await card.screenshot({path:`/tmp/p145-service-reasons/preview-${language}.png`})
    response={status:200,json:{data:[{id:'1',phone:'138****0001'}],page:{limit:20,has_more:false,next_cursor:''},service_version:'fixture-v1'}}
    await card.getByRole('button',{name:words.loadData,exact:true}).click()
    await expect(alert).toHaveCount(0)
    await expect(card.getByText('138****0001',{exact:true})).toBeVisible()
    expect(requests).toHaveLength(13)
    expect(requests.every(({ body }) => !body.page.cursor)).toBe(true)
    expect(requests.map(request => request.language)).toEqual(Array(13).fill(language))

    // The same mounted Service client must follow each Console language change.
    for (const nextLanguage of [language === 'en' ? 'zh-cn' : 'en', language]) {
      await page.locator('iframe[title="Service"]').evaluate((iframe, lang) => {
        iframe.contentWindow.postMessage({ type: 'lang-change', source: 'addp-console', lang }, location.origin)
      }, nextLanguage)
      const nextWords = (nextLanguage === 'en' ? en : zh).service.query
      const preview = frame.locator('.el-card').filter({ has: frame.getByText(nextWords.cardPreview, { exact: true }) })
      const sent = page.waitForRequest(request => new URL(request.url()).pathname === '/api/query/protected_reason_fixture/query')
      await preview.getByRole('button', { name: nextWords.refreshData, exact: true }).click()
      expect((await sent).headers()['accept-language']).toBe(nextLanguage)
      await expect(preview.getByRole('button', { name: nextWords.refreshData, exact: true })).toBeEnabled()
    }
  })
}


for (const language of ['zh-cn', 'en']) {
  test(`Service preview rejects late rows after a newer protection refusal (${language})`, async ({ page }) => {
    const words = (language === 'en' ? en : zh).service.query
    const common = (language === 'en' ? en : zh).service.common
    const translations = readFileSync(`../../service/backend/i18n/locales/${language}.toml`, 'utf8')
    const refusal = translations.split('[service.err.protection_expired]')[1].match(/other = "([^"\n]+)"/)[1]
    let releaseEarlier
    const earlierPending = new Promise(resolve => { releaseEarlier = resolve })
    let requests = 0
    await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
    await page.route(url => url.pathname.startsWith('/api/'), async route => {
      const path = new URL(route.request().url()).pathname
      if (path === '/api/v1/service/query/41') return route.fulfill({ json: definition })
      if (path === '/api/v1/system/engine-catalog/engines') return route.fulfill({ json: [{ id: 1, name: 'fixture-postgres', engine_type: 'postgresql' }] })
      if (path === '/api/query/protected_reason_fixture/query') {
        requests++
        if (requests === 1) {
          await earlierPending
          return route.fulfill({ json: { data: [{ id: '1', phone: '138****0001' }], page: { limit: 20, has_more: true, next_cursor: 'obsolete-cursor' }, service_version: 'fixture-v1' } })
        }
        return route.fulfill({ status: 403, json: { error_code: 'data_protection_required', error: refusal } })
      }
      throw Error(`Unexpected request ${route.request().method()} ${path}`)
    })
    await page.goto('/e2e/service-fixture/e2e/navigation.html#/query-services/41')
    const card = page.locator('.el-card').filter({ has: page.getByText(words.cardPreview, { exact: true }) })
    await card.getByRole('button', { name: words.loadData, exact: true }).click()
    await expect.poll(() => requests).toBe(1)
    const endpoint = page.locator('.endpoint-item').filter({ has: page.getByText(words.restApiEndpointTitle, { exact: true }) })
    await endpoint.getByRole('button', { name: common.test, exact: true }).click()
    await expect(card.locator('.preview-error')).toContainText(refusal)
    const earlierResponse = page.waitForResponse(response => new URL(response.url()).pathname === '/api/query/protected_reason_fixture/query' && response.status() === 200)
    releaseEarlier()
    await (await earlierResponse).finished()
    await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))))
    await expect(card.getByText('138****0001', { exact: true })).toHaveCount(0)
    await expect(card.locator('.preview-error')).toContainText(refusal)
    await expect(card.locator('.preview-content')).toHaveCount(0)
    await expect(card.locator('.preview-empty')).toHaveCount(0)
    expect(requests).toBe(2)
  })
}
