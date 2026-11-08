import { expect, test } from '@playwright/test'
import { readFileSync } from 'node:fs'
import zh from '../../../service/frontend/src/i18n/zh-cn.json' with { type: 'json' }
import en from '../../../service/frontend/src/i18n/en.json' with { type: 'json' }

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
    const definition = { id: 41, version: 1, title: 'Protected Service fixture', service_name: 'protected_reason_fixture', status: 'active', config_type: 'sql', engine_id: 1, sql_query: 'SELECT id, phone FROM public.persons', named_parameters: [], stable_key: ['id'], max_features: 100, protocols: {rest_api:{enabled:true}}, data_config: {protection_choices:[]}, output_contract: {table:{fields:[{name:'id',type:'bigint',nullable:false},{name:'phone',type:'string'}],primary_key:['id']}} }
    let response = {status:200,json:{data:[{id:'1',phone:'138****0001'}],page:{limit:20,has_more:true,next_cursor:'old-sensitive-cursor'},service_version:'fixture-v1'}}
    const requests = []
    await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
    page.on('pageerror', error => { throw error })
    await page.route(url => url.pathname.startsWith('/api/'), async route => {
      const path = new URL(route.request().url()).pathname
      if(path === '/api/v1/service/query/41') return route.fulfill({json:definition})
      if(path === '/api/v1/system/engine-catalog/engines') return route.fulfill({json:[{id:1,name:'fixture-postgres',engine_type:'postgresql'}]})
      if(path === '/api/query/protected_reason_fixture/query') {
        requests.push(route.request().postDataJSON())
        return route.fulfill(response)
      }
      throw Error(`Unexpected request ${route.request().method()} ${path}`)
    })
    await page.goto('/e2e/service-fixture/e2e/navigation.html#/query-services/41')
    const card = page.locator('.el-card').filter({has:page.getByText(words.cardPreview,{exact:true})})
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
        await expect(page.locator('.el-message')).toHaveCount(0,{timeout:10000})
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
    await expect(page.locator('.el-message')).toHaveCount(0,{timeout:10000})
    await card.screenshot({path:`/tmp/p145-service-reasons/preview-${language}.png`})
    response={status:200,json:{data:[{id:'1',phone:'138****0001'}],page:{limit:20,has_more:false,next_cursor:''},service_version:'fixture-v1'}}
    await card.getByRole('button',{name:words.loadData,exact:true}).click()
    await expect(alert).toHaveCount(0)
    await expect(card.getByText('138****0001',{exact:true})).toBeVisible()
    expect(requests).toHaveLength(13)
    expect(requests.every(x=>!x.page.cursor)).toBe(true)
  })
}
