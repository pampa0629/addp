"""Owner API preparation only; never invokes the already-passed API T4 suite."""
import hashlib
import importlib
import json
import time
import urllib.request

API=importlib.import_module('scripts.utils.online-api')
DOMAIN=importlib.import_module('scripts.test.catalog-domain-overview-online')


def require(value, message):
    if not value:raise API.SuiteError(message)


def login(gateway, credentials, name):
    user=credentials['users'][name]
    request=urllib.request.Request(gateway+'/api/v1/system/login',method='POST',
        data=json.dumps({'username':user['username'],'password':user['password']}).encode(),
        headers={'Content-Type':'application/json'})
    opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request,timeout=10) as response:reply=json.load(response)
    require(reply.get('next_action')=='session_issued','fixture login must issue an ordinary session')
    client=API.GatewayClient(gateway,reply['session']['access_token'],10)
    facts,principal=DOMAIN.validate_identity(client,credentials['tenant_id'],set(user['permissions']))
    require(principal==str(user['principal_id']),'fixture principal changed')
    return client


def source_digest(payload):
    return hashlib.sha256(json.dumps(payload,sort_keys=True,separators=(',',':')).encode()).hexdigest()


def prepare(gateway, credentials):
    client=login(gateway,credentials,'preparer')
    units=client.request('GET','/api/v1/standard/units',(200,),response_type=list).payload
    require(len(units)>0,'published unit catalog unavailable')
    # Consume an existing formal unit, not a made-up unit ID or new platform definition.
    unit=next((row for row in units if row.get('code')=='m'),None)
    require(unit is not None,'formal metre unit unavailable')
    prefix=DOMAIN.fixture_prefix(credentials['run_id'])
    source_id=DOMAIN.prepare_reference(client,credentials['run_id'],unit_id=unit['id'])
    source_path=f'/api/v1/standard/elements/{source_id}'
    source=client.request('GET',source_path,(200,)).payload
    source_domain=source['owner_domain_id']
    document=client.request('POST','/api/v1/standard/documents',(201,),{'scope_type':'domain','owner_domain_id':source_domain,
        'code':prefix+'_doc','name':'Catalog 浏览器验收来源文档','doc_type':'internal','description':'Disposable browser fixture','tags':[]}).payload
    client.request('POST',source_path+'/documents/link',(200,),{'doc_id':document['id'],'version':source['version']})
    source=client.request('GET',source_path,(200,)).payload
    domain=client.request('POST','/api/v1/standard/domains',(201,),{'name':'Catalog 浏览器验收业务域 A','code':prefix+'_a'}).payload
    local=client.request('POST','/api/v1/standard/elements',(201,),{'scope_type':'domain','owner_domain_id':domain['id'],
        'code':prefix+'_local','name':'Catalog 无单位无文档对照','definition':'Browser acceptance control',
        'data_type':'string','value_domain_kind':'unrestricted','nullable':True,'tags':[]}).payload
    entity=client.request('POST','/api/v1/model/entities',(201,),{'domain_id':domain['id'],'code':prefix+'_entity',
        'name':'Catalog 跨域标准引用实体','description':'Browser acceptance draft reference'}).payload
    client.request('POST',f"/api/v1/model/entities/{entity['id']}/attributes",(201,),{'version':entity['version'],
        'element_id':source_id,'name':'跨域来源属性','column_name':'external_reference','data_type':'string','is_pk':False,'nullable':True,'sort_order':0})
    # Readiness only: enroll both exact domains and await their owner projections.
    deadline=time.monotonic()+120
    while True:
        totals=[]
        for domain_id in (domain['id'],source_domain):
            client.request('GET',f'/api/v1/catalog/entries?view=inventory&primary_domain_id={domain_id}',(200,))
            totals.append(client.request('GET',f'/api/v1/catalog/domains/{domain_id}/overview?view=inventory',(200,)).payload['total_entries'])
        if totals==[2,2]:break
        require(time.monotonic()<deadline,'catalog fixture synchronization timed out')
        time.sleep(1)
    references=[{'source_module':'standard','source_type':'element','source_identity':str(value)} for value in (local['id'],source_id)]
    resolved=client.request('POST','/api/v1/catalog/entries/resolve-sources',(200,),{'references':references}).payload
    require(all(row.get('found') for row in resolved['results']) and len(resolved['results'])==2,'fixture source entries unavailable')
    return {'domain_a':domain['id'],'domain_b':source_domain,'local_element':local['id'],'source_element':source_id,
        'unit_id':unit['id'],'document_id':document['id'],'entity_id':entity['id'],'relation_state':'draft',
        'entry_ids':[row['entry']['id'] for row in resolved['results']], 'source_digest':source_digest(source)}


def verify_source(gateway,credentials,samples):
    client=login(gateway,credentials,'extended')
    source=client.request('GET',f"/api/v1/standard/elements/{samples['source_element']}",(200,)).payload
    require(source_digest(source)==samples['source_digest'],'browser acceptance changed the source standard')
