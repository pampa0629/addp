import { expect, test } from '@playwright/test'
import zh from '../../../service/frontend/src/i18n/zh-cn.json' with { type: 'json' }
import en from '../../../service/frontend/src/i18n/en.json' with { type: 'json' }
import commonZh from '../../../common-frontend/basic/src/i18n/zh-cn.json' with { type: 'json' }
import commonEn from '../../../common-frontend/basic/src/i18n/en.json' with { type: 'json' }

for (const language of ['zh-cn', 'en']) {
  test(`SQL role choices use source fields, survive failed saves and restore defaults (${language})`, async ({ page }) => {
    const words = (language === 'en' ? en : zh).service.query
    const common = (language === 'en' ? commonEn : commonZh).fieldMaskingChoices
    const target = { owner_module: 'meta', resource_type: 'data_item', resource_identity: 'sql-persons-source' }
    const field = { target, component_key: 'phone', policy_id: 'sql-role-policy', policy_revision: 1, assessment_revision: 1, options: [{ id: 'full-mask', name: 'SQL full mask' }] }
    const choice = { target, component_key: 'phone', selection: { policy_id: field.policy_id, policy_revision: 1, assessment_revision: 1, option_id: 'full-mask' } }
    let definition = { id: 41, version: 3, service_name: 'sql_masking_fixture', title: 'SQL role masking', config_type: 'sql', engine_id: 1, sql_query: 'SELECT id, phone AS phone_alias, phone AS phone_copy FROM public.persons', named_parameters: [], stable_key: ['id'], max_features: 100, protocols: { rest_api: { enabled: true }, ogc_features: { enabled: false } }, output_contract: { table: { fields: [{name:'id',type:'bigint',nullable:false},{name:'phone_alias',type:'string'},{name:'phone_copy',type:'string'}], primary_key: ['id'] } }, data_config: { protection_choices: [] } }
    let failSave = true
    let revoked = false
    const writes = [], candidateRequests = []
    await page.addInitScript(language => localStorage.setItem('addp-lang', language), language)
    await page.route(url => url.pathname.startsWith('/api/'), async route => {
      const request = route.request(), path = new URL(request.url()).pathname
      if (path === '/api/v1/meta/engines') return route.fulfill({json:[]})
      if (path === '/api/v1/system/engine-catalog/engines') return route.fulfill({json:[{id:1,name:'fixture-postgres',engine_type:'postgresql',lifecycle_state:'active',connection_status:'online',capabilities:{compute:{query:{supported:true,languages:['sql']}}}}]})
      if (path === '/api/v1/service/query-protection-options') {
        expect(request.method()).toBe('POST')
        const body = request.postDataJSON()
        expect(body).toEqual({config_type:'sql',engine_id:1,sql:definition.sql_query,parameters:{}})
        expect(request.url()).not.toContain('SELECT')
        candidateRequests.push(body)
        return route.fulfill({json:{fields:[{...field,options:revoked?[]:field.options}]}})
      }
      if (path === '/api/v1/service/query/41' && request.method() === 'GET') return route.fulfill({json:definition})
      if (path === '/api/v1/service/query/41' && request.method() === 'PUT') {
        const body = request.postDataJSON(); writes.push(body)
        if (failSave) return route.fulfill({status:409,json:{error_code:'query_protection_choice_invalid',error:'fixture method unavailable'}})
        definition = {...definition,...body,version:body.version+1}
        return route.fulfill({json:definition})
      }
      throw new Error(`Unexpected SQL masking request ${request.method()} ${path}`)
    })
    const editURL = '/e2e/fixtures/leave-guard.html?editor=query-service#/service/query-services/41/edit'
    const editor = page.frameLocator('iframe')
    await page.goto(editURL)
    await page.addStyleTag({content:'iframe { width:100%; height:960px; border:0; }'})
    await expect(editor.getByRole('heading',{name:words.formEditTitle,exact:true})).toBeVisible()
    const panel = editor.locator('.field-masking-choices')
    await panel.getByRole('button',{name:common.refresh,exact:true}).click()
    await panel.locator('.el-select').click()
    await editor.getByRole('option',{name:'SQL full mask',exact:true}).click()
    await editor.getByRole('button',{name:words.updateBtn,exact:true}).click()
    await expect(editor.getByText(words.submitFailed + ': fixture method unavailable',{exact:true})).toBeVisible()
    await expect(panel.locator('.el-select')).toContainText('SQL full mask')
    expect(writes[0].version).toBe(3)
    expect(writes[0].data_config.protection_choices).toEqual([choice])
    failSave = false
    await expect(editor.getByText(words.submitFailed + ': fixture method unavailable',{exact:true})).toBeHidden()
    await panel.screenshot({path:`/tmp/p145-service-sql-role-ui-${language}.png`})
    await editor.getByRole('button',{name:words.updateBtn,exact:true}).click()
    await expect(editor.getByRole('heading',{name:'Query service list',exact:true})).toBeVisible()
    expect(writes[1].version).toBe(3)
    expect(writes[1].data_config.protection_choices).toEqual([choice])
    revoked = true
    await page.goto(editURL)
    await page.addStyleTag({content:'iframe { width:100%; height:960px; border:0; }'})
    await panel.getByRole('button',{name:common.refresh,exact:true}).click()
    await expect(panel.locator('.el-select')).toContainText(common.stale)
    await panel.locator('.el-select').click()
    await editor.getByRole('option',{name:common.default,exact:true}).click()
    await editor.getByRole('button',{name:words.updateBtn,exact:true}).click()
    await expect(editor.getByRole('heading',{name:'Query service list',exact:true})).toBeVisible()
    expect(writes[2].version).toBe(4)
    expect(writes[2].data_config.protection_choices).toEqual([])
    expect(candidateRequests).toHaveLength(2)
  })
}

for (const language of ['zh-cn', 'en']) for (const fixedOptions of [false, true]) {
  test(`published SQL default edits preserve immutable declarations (${language}, ${fixedOptions ? 'options' : 'scalar'})`, async ({page}) => {
    const words=(language==='en'?en:zh).service.query
    let definition={id:41,version:3,service_name:'default_edit_fixture',title:'SQL optional defaults',config_type:'sql',engine_id:1,sql_query:'SELECT id FROM public.persons WHERE id >= :minimum AND :subject IS NOT NULL',named_parameters:[{name:'minimum',type:'int',required:false,default:1},{name:'subject',type:'string',required:true}],stable_key:['id'],max_features:100,protocols:{rest_api:{enabled:true},ogc_features:{enabled:false}},output_contract:{table:{fields:[{name:'id',type:'bigint',nullable:false}],primary_key:['id']}},data_config:{protection_choices:[]}}
    const options=[{value:1,labels:{'zh-cn':'第一组',en:'First range'}},{value:2,labels:{'zh-cn':'第二组',en:'Second range'}}]
    if(fixedOptions)definition.named_parameters[0].options=options
    const selectedLabel=language==='en'?'Second range':'第二组'
    const writes=[];let fail=true
    await page.addInitScript(language=>localStorage.setItem('addp-lang',language),language)
    await page.route(url=>url.pathname.startsWith('/api/'),async route=>{
      const request=route.request(),path=new URL(request.url()).pathname
      if(path==='/api/v1/meta/engines')return route.fulfill({json:[]})
      if(path==='/api/v1/system/engine-catalog/engines')return route.fulfill({json:[{id:1,name:'fixture-postgres',engine_type:'postgresql',lifecycle_state:'active',connection_status:'online',capabilities:{compute:{query:{supported:true,languages:['sql']}}}}]})
      if(path==='/api/v1/service/query/41'&&request.method()==='GET')return route.fulfill({json:definition})
      if(path==='/api/v1/service/query/41'&&request.method()==='PUT'){
        const body=request.postDataJSON();writes.push(body)
        expect(body.named_parameter_defaults).toEqual({minimum:2})
        expect(body).not.toHaveProperty('named_parameters');expect(body).not.toHaveProperty('sql_query')
        if(fail)return route.fulfill({status:400,json:{error_code:'invalid_named_parameter_defaults',error:'fixture default invalid'}})
        definition={...definition,version:4,named_parameters:definition.named_parameters.map(p=>p.name==='minimum'?{...p,default:2}:p)}
        return route.fulfill({json:definition})
      }
      throw Error(`Unexpected default edit request ${request.method()} ${path}`)
    })
    const url='/e2e/fixtures/leave-guard.html?editor=query-service#/service/query-services/41/edit'
    const editor=page.frameLocator('iframe')
    await page.goto(url)
    await page.addStyleTag({content:'iframe { width:100%; height:960px; border:0; }'})
    await expect(editor.getByRole('heading',{name:words.formEditTitle,exact:true})).toBeVisible()
    const rows=editor.locator('.named-parameter-row');await expect(rows).toHaveCount(2)
    await expect(editor.getByText(words.namedParametersEditHelp,{exact:true})).toBeVisible()
    await expect(rows.nth(0).locator('.el-input__inner').first()).toBeDisabled()
    await expect(rows.nth(0).getByRole('checkbox')).toBeDisabled()
    await expect(rows.nth(0).getByText(words.namedParameterDefault,{exact:true})).toBeVisible()
    await expect(rows.nth(1).locator('.named-parameter-value input')).toBeDisabled()
    await expect(rows.nth(0).locator('.named-parameter-type .el-select__wrapper')).toHaveClass(/is-disabled/)
    await expect(rows.nth(1).locator('.el-input__inner').first()).toBeDisabled()
    const value=rows.nth(0).locator(fixedOptions?'.named-parameter-value .el-select':'.el-input-number input')
    if(fixedOptions){await value.click();await editor.getByRole('option',{name:selectedLabel,exact:true}).click()}
    else{await value.fill('2');await value.blur()}

    await editor.getByRole('button',{name:words.updateBtn,exact:true}).click()
    await expect(editor.getByText(words.submitFailed+': fixture default invalid',{exact:true})).toBeVisible()
    if(fixedOptions)await expect(value).toContainText(selectedLabel)
    else await expect(value).toHaveValue('2')
    expect(writes).toHaveLength(1);expect(writes[0].version).toBe(3)
    fail=false;await editor.getByRole('button',{name:words.updateBtn,exact:true}).click()
    await expect(editor.getByRole('heading',{name:'Query service list',exact:true})).toBeVisible()
    await page.goto(url);await page.addStyleTag({content:'iframe { width:100%; height:960px; border:0; }'})
    if(fixedOptions)await expect(value).toContainText(selectedLabel)
    else await expect(value).toHaveValue('2')
    await rows.nth(0).screenshot({path:`/tmp/p145-default-edit-${language}${fixedOptions?'-options':''}.png`})
    if(fixedOptions)expect(definition.named_parameters[0].options).toEqual(options)
    expect(definition.named_parameters[1]).toEqual({name:'subject',type:'string',required:true})
    expect(writes[1].version).toBe(3)
  })
}
