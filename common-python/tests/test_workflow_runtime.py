import threading
import json

import pytest

from addp_common.workflow_access import publish_target_file

from addp_common.workflow_runtime import (
    ExecutionRegistry,
    WorkflowRunner,
    WorkflowValidationError,
    WorkflowExecutionError,
    validate_execution_authorization,
)


@pytest.mark.parametrize('projected', [False, True])
@pytest.mark.parametrize('nested', [False, True])
def test_create_conflict_keeps_typed_code_through_dag_and_registry(tmp_path, projected, nested):
    source, target = tmp_path / 'source', tmp_path / 'target'
    source.write_bytes(b'new')
    target.write_bytes(b'original')
    plan = {'target': {'kind': 'file', 'write_mode': 'create',
                       'access': {'method': 'mounted_path', 'path': str(target)}}}
    workflow = {'tasks': [{'id': 'publish', 'operator': 'publish', 'params': {}, 'depends_on': []}]}
    executor = lambda _operator, _params: publish_target_file(source, plan)
    if nested:
        inner = WorkflowRunner({'publish'}, executor)
        executor = lambda _operator, _params: inner.execute(workflow)
    runner = WorkflowRunner({'publish'}, executor, result_projector=json.dumps if projected else None)
    class InlineThread:
        def __init__(self, target, args, daemon): self.run = lambda: target(*args)
        def start(self): self.run()
    registry = ExecutionRegistry()
    accepted = registry.submit(runner, workflow, thread_factory=InlineThread)
    snapshot = registry.get(accepted.execution_id)
    assert snapshot.status == 'failed'
    assert snapshot.error_code == 'TARGET_ALREADY_EXISTS'
    assert target.read_bytes() == b'original'
    if projected:
        assert snapshot.all_results == {}
        assert snapshot.task_order == ['publish']


def test_error_text_and_untyped_code_attribute_cannot_fabricate_create_conflict():
    error = ValueError('target file already exists: private-path')
    error.error_code = 'TARGET_ALREADY_EXISTS'
    def execute(_operator, _params): raise error
    with pytest.raises(WorkflowExecutionError) as caught:
        WorkflowRunner({'publish'}, execute).execute({'tasks': [
            {'id': 'publish', 'operator': 'publish', 'params': {}, 'depends_on': []}]})
    assert caught.value.error_code == 'EXECUTION_FAILED'


@pytest.mark.parametrize('explicit_partial', [False, True])
def test_projected_failure_retains_safe_completed_results_atomically(explicit_partial):
    secret = object()
    def execute(operator, params):
        if operator == 'private': return secret
        if explicit_partial:
            raise WorkflowExecutionError('batch failed', partial_result={'completed_tiles': 2})
        raise ValueError('later task failed')
    def project(value):
        return json.dumps({'private': True} if value is secret else value)
    runner = WorkflowRunner({'private', 'fail'}, execute, result_projector=project)
    class InlineThread:
        def __init__(self, target, args, daemon): self.run = lambda: target(*args)
        def start(self): self.run()
    registry = ExecutionRegistry()
    accepted = registry.submit(runner, {'tasks': [
        {'id': 'a', 'operator': 'private', 'params': {}, 'depends_on': []},
        {'id': 'b', 'operator': 'fail', 'params': {}, 'depends_on': ['a']},
    ]}, thread_factory=InlineThread)
    snapshot = registry.get(accepted.execution_id)
    assert snapshot.status == 'failed' and snapshot.error_code == 'EXECUTION_FAILED'
    assert json.loads(snapshot.all_results['a']) == {'private': True}
    assert snapshot.task_order == ['a', 'b']
    assert snapshot.result == (json.dumps({'completed_tiles': 2}) if explicit_partial else None)
    json.dumps(snapshot.to_dict())


def test_runner_executes_dag_and_resolves_ports():
    runner = WorkflowRunner(
        {"add", "double"},
        lambda operator, params: params["a"] + params["b"] if operator == "add" else params["value"] * 2,
    )
    result = runner.execute({
        "tasks": [
            {"id": "a", "operator": "add", "params": {"a": 2, "b": 3}, "depends_on": []},
            {"id": "b", "operator": "double", "params": {"value": {"$ref": "a"}}, "depends_on": ["a"]},
        ]
    })
    assert result.task_order == ["a", "b"]
    assert result.all_results == {"a": 5, "b": 10}
    assert result.final_result == 10


def test_validation_rejects_undeclared_reference_dependency():
    runner = WorkflowRunner({"echo"}, lambda _operator, params: params)
    with pytest.raises(WorkflowValidationError, match="未在 depends_on 中声明"):
        runner.execute({
            "tasks": [
                {"id": "a", "operator": "echo", "params": {}, "depends_on": []},
                {"id": "b", "operator": "echo", "params": {"value": {"$ref": "a"}}, "depends_on": []},
            ]
        })


def test_execution_registry_runs_asynchronously():
    threads = []

    class DeferredThread:
        def __init__(self, *, target, args, daemon):
            self.target = target
            self.args = args
            self.daemon = daemon
            threads.append(self)

        def start(self):
            return None

    registry = ExecutionRegistry()
    runner = WorkflowRunner({"echo"}, lambda _operator, params: params["value"])
    submitted = registry.submit(
        runner,
        {"tasks": [{"id": "a", "operator": "echo", "params": {"value": 7}, "depends_on": []}]},
        thread_factory=DeferredThread,
    )
    assert submitted.status == "pending"
    threads[0].target(*threads[0].args)
    completed = registry.get(submitted.execution_id)
    assert completed is not None
    assert completed.status == "success"
    assert completed.result == 7
    assert completed.progress == 100


def test_execution_authorization_covers_dag_effects():
    workflow = {
        "tasks": [
            {"id": "load", "operator": "load", "params": {}, "depends_on": []},
            {"id": "save", "operator": "save", "params": {}, "depends_on": ["load"]},
        ]
    }
    effects = validate_execution_authorization(
        workflow,
        operator_effects={"load": ["read"], "save": ["write"]},
        runtime={"tenant_id": 7, "execution_authorization": {"id": 71, "effects": ["read", "write"]}},
    )
    assert effects == ["read", "write"]


def test_execution_authorization_rejects_missing_effect():
    workflow = {
        "tasks": [{"id": "save", "operator": "save", "params": {}, "depends_on": []}]
    }
    with pytest.raises(WorkflowValidationError, match="write"):
        validate_execution_authorization(
            workflow,
            operator_effects={"save": ["write"]},
            runtime={"tenant_id": 7, "execution_authorization": {"id": 71, "effects": ["read"]}},
        )


def test_execution_authorization_rejects_missing_tenant_context():
    workflow = {
        "tasks": [{"id": "load", "operator": "load", "params": {}, "depends_on": []}]
    }
    with pytest.raises(WorkflowValidationError, match="runtime.tenant_id"):
        validate_execution_authorization(
            workflow,
            operator_effects={"load": ["read"]},
            runtime={"execution_authorization": {"id": 71, "effects": ["read"]}},
        )


@pytest.mark.parametrize('failure', ['input', 'port', 'projection'])
def test_projected_runner_keeps_completed_results_when_later_node_preparation_fails(failure):
    import json
    from addp_common.workflow_runtime import WorkflowExecutionError
    params = {'value': {'$input':'missing'}} if failure == 'input' else {'value': {'$ref':'a','port':'missing'}} if failure == 'port' else {}
    def execute(operator, params):
        return {'saved':True} if operator == 'save' else object()
    runner = WorkflowRunner({'save','later'},execute,result_projector=json.dumps)
    with pytest.raises(WorkflowExecutionError) as caught:
        runner.execute({'tasks':[
            {'id':'a','operator':'save','params':{},'depends_on':[]},
            {'id':'b','operator':'later','params':params,'depends_on':['a']},
        ]})
    assert caught.value.all_results == {'a':json.dumps({'saved':True})}
    assert caught.value.task_order == ['a','b']


def test_typed_operator_diagnostic_preserves_definition_position_and_prior_results():
    from addp_common.workflow_runtime import OperatorInputError
    workflow={'tasks':[
        {'id':'private/password=hidden','operator':'batch_buffer','params':{},'depends_on':['earlier']},
        {'id':'earlier','operator':'load','params':{},'depends_on':[]},
        {'id':'save','operator':'save','params':{},'depends_on':['private/password=hidden']}]}
    calls=[]
    def execute(op,params):
        calls.append(op)
        if op=='batch_buffer':raise OperatorInputError('operator_field_conflict','private password=secret',field='distance')
        return {'previous':'kept'}
    registry=ExecutionRegistry();accepted=registry.submit(WorkflowRunner({'load','batch_buffer','save'},execute),workflow)
    import time,json
    for _ in range(100):
        snapshot=registry.get(accepted.execution_id)
        if snapshot.status=='failed':break
        time.sleep(.01)
    assert snapshot.error_code=='INVALID_PARAMS' and calls==['load','batch_buffer']
    assert snapshot.all_results=={'earlier':{'previous':'kept'}}
    assert snapshot.diagnostic=={'schema_version':'addp.execution-failure/v3','stage':'execution',
        'reason':'operator_field_conflict','operator':{'node_index':1,'operator_id':'batch_buffer','parameter':'input_gdf','field':'distance'}}
    assert 'private' not in json.dumps(snapshot.diagnostic) and 'secret' not in json.dumps(snapshot.diagnostic)
    snapshot.diagnostic['operator']['field']='private'
    assert registry.get(accepted.execution_id).diagnostic['operator']['field']=='distance'


def test_parameter_diagnostic_refuses_untyped_messages_and_malformed_context():
    from addp_common.workflow_runtime import OperatorInputError
    from addp_common.workflow_runtime.errors import validated_operator_diagnostic,OPERATOR_INPUT_RULES
    import copy
    for (op,reason,field),_ in OPERATOR_INPUT_RULES.items():
        value=OperatorInputError(reason,'internal text',field=field).diagnostic(op,2)
        assert validated_operator_diagnostic(value)==value
        for patch in [lambda x:x.update(message='private'),lambda x:x.update(stage='validation'),
            lambda x:x['operator'].update(node_index=True),lambda x:x['operator'].update(field='private'),
            lambda x:x['operator'].update(parameter=None),lambda x:x['operator'].update(node_id='private')]:
            altered=copy.deepcopy(value);patch(altered);assert validated_operator_diagnostic(altered) is None
    workflow={'tasks':[{'id':'one','operator':'batch_buffer','params':{},'depends_on':[]}]}
    def raw(op,p):raise ValueError('INVALID_PARAMS distance conflicts password=secret')
    with pytest.raises(WorkflowExecutionError) as failed:WorkflowRunner({'batch_buffer'},raw).execute(workflow)
    assert failed.value.error_code=='EXECUTION_FAILED' and failed.value.diagnostic is None


def test_ring_diagnostic_never_infers_a_conflict_field_or_trusts_private_fields():
    from addp_common.workflow_runtime import OperatorInputError
    from addp_common.workflow_runtime.errors import validated_operator_diagnostic
    import copy
    assert OperatorInputError('operator_field_conflict','private').diagnostic('extract_polygon_rings',1) is None
    for field in ['distance','private',None,[],{},True]:
        assert OperatorInputError('operator_field_conflict','private',field=field).diagnostic('extract_polygon_rings',1) is None
    original=OperatorInputError('operator_geometry_invalid','private').diagnostic('extract_polygon_rings',2)
    assert original['operator']=={'node_index':2,'operator_id':'extract_polygon_rings','parameter':'input_gdf'}
    for field in ['source_id','',None,[],True]:
        value=copy.deepcopy(original);value['operator']['field']=field
        assert validated_operator_diagnostic(value) is None


@pytest.mark.parametrize('reason,field', [('operator_field_conflict','ring_id'),('operator_geometry_invalid','')])
def test_ring_diagnostic_uses_definition_position_and_stops_before_save(reason,field):
    from addp_common.workflow_runtime import OperatorInputError
    tasks=[{'id':'private-ring','operator':'extract_polygon_rings','params':{},'depends_on':['load']},
           {'id':'load','operator':'load','params':{},'depends_on':[]},
           {'id':'save','operator':'save','params':{},'depends_on':['private-ring']}]
    calls=[]
    def execute(op,params):
        calls.append(op)
        if op=='extract_polygon_rings':raise OperatorInputError(reason,'private GEOS/password',field=field)
        return {'previous':'kept'}
    with pytest.raises(WorkflowExecutionError) as failure:
        WorkflowRunner({'load','extract_polygon_rings','save'},execute).execute({'tasks':tasks})
    assert calls==['load','extract_polygon_rings']
    assert failure.value.all_results=={'load':{'previous':'kept'}}
    diagnostic=failure.value.diagnostic
    assert diagnostic['operator']['node_index']==1 and diagnostic['reason']==reason
    assert diagnostic['operator'].get('field','')==field


@pytest.mark.parametrize('reason,parameter',[
    ('operator_distance_limit_invalid','max_distance'),('operator_input_invalid','input_gdf'),
    ('operator_reference_invalid','reference_gdf'),('operator_crs_invalid',''),
    ('operator_source_topology_invalid','input_gdf'),('operator_trim_conflict',''),
])
def test_trim_diagnostics_closed_tuple_drops_private_context_and_keeps_original_definition_position(reason,parameter):
    from addp_common.workflow_runtime.errors import OperatorInputError,validated_operator_diagnostic
    value=OperatorInputError(reason,'PRIVATE source coordinates').diagnostic('trim_line_endpoints',1)
    assert value=={'schema_version':'addp.execution-failure/v3','stage':'execution','reason':reason,
                  'operator':{'node_index':1,'operator_id':'trim_line_endpoints',**({'parameter':parameter} if parameter else {})}}
    assert validated_operator_diagnostic(value)==value
    for field in ['distance','private','']:
        changed={**value,'operator':{**value['operator'],'field':field}}
        assert validated_operator_diagnostic(changed) is None
    assert OperatorInputError(reason,'PRIVATE').diagnostic('foreign',1) is None


def test_difference_input_rules_are_exact_and_never_reference_identity_or_private_fields():
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES,OperatorInputError,validated_operator_diagnostic
    import copy
    expected={('difference','operator_input_invalid',''):'input_gdf',('difference','operator_mask_invalid',''):'gdf_b',('difference','operator_crs_invalid',''):''}
    assert {k:v for k,v in OPERATOR_INPUT_RULES.items() if k[0]=='difference'}==expected
    for (op,reason,field),param in expected.items():
        value=OperatorInputError(reason,'PRIVATE runtime geometry').diagnostic(op,2)
        assert validated_operator_diagnostic(value)==value
        for patch in ({'field':'source_id'},{'field':'PRIVATE'},{'parameter':'reference_gdf'},{'node_index':True}):
            bad=copy.deepcopy(value);bad['operator'].update(patch);assert validated_operator_diagnostic(bad) is None
    assert OperatorInputError('operator_reference_invalid','PRIVATE').diagnostic('difference',1) is None
    assert OperatorInputError('operator_mask_invalid','PRIVATE').diagnostic('intersection',1) is None


def test_clip_input_rules_are_exact_and_never_reference_identity_or_private_fields():
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES,OperatorInputError,validated_operator_diagnostic
    import copy
    expected={('clip','operator_input_invalid',''):'input_gdf',('clip','operator_mask_invalid',''):'mask_gdf',('clip','operator_crs_invalid',''):''}
    assert {k:v for k,v in OPERATOR_INPUT_RULES.items() if k[0]=='clip'}==expected
    for (op,reason,field),param in expected.items():
        value=OperatorInputError(reason,'PRIVATE runtime geometry').diagnostic(op,2)
        assert validated_operator_diagnostic(value)==value
        for patch in ({'field':'source_id'},{'field':'PRIVATE'},{'parameter':'reference_gdf'},{'node_index':True}):
            bad=copy.deepcopy(value);bad['operator'].update(patch);assert validated_operator_diagnostic(bad) is None
    assert OperatorInputError('operator_reference_invalid','PRIVATE').diagnostic('clip',1) is None
    assert OperatorInputError('operator_mask_invalid','PRIVATE').diagnostic('intersection',1) is None


def test_union_input_rules_are_six_closed_combinations_with_distinct_overlap_sides():
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES,OperatorInputError,validated_operator_diagnostic
    import copy
    expected={'operator_input_invalid': 'input_gdf', 'operator_reference_invalid': 'gdf_b', 'operator_crs_invalid': '', 'operator_overlay_fields_conflict': '', 'operator_input_overlap': 'input_gdf', 'operator_reference_overlap': 'gdf_b'}
    assert {k:v for k,v in OPERATOR_INPUT_RULES.items()if k[0]=='union'}=={('union',k,''):v for k,v in expected.items()}
    for reason,param in expected.items():
        value=OperatorInputError(reason,'PRIVATE source').diagnostic('union',2)
        assert value['operator']=={'node_index':2,'operator_id':'union',**({'parameter':param}if param else{})}
        assert validated_operator_diagnostic(value)==value
        for patch in ({'field':'source_id_a'},{'field':'PRIVATE'},{'parameter':'reference_gdf'},{'node_index':True}):
            bad=copy.deepcopy(value);bad['operator'].update(patch);assert validated_operator_diagnostic(bad)is None
    assert OperatorInputError('operator_mask_invalid','PRIVATE').diagnostic('union',1)is None
    assert OperatorInputError('operator_input_overlap','PRIVATE').diagnostic('intersection',1)is None


def test_dissolve_four_closed_diagnostic_rules():
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES,OperatorInputError,validated_operator_diagnostic
    import copy
    expected={'operator_input_invalid': 'input_gdf', 'operator_crs_invalid': '', 'operator_group_fields_invalid': 'group_fields', 'operator_dissolve_fields_conflict': ''}
    assert {k:v for k,v in OPERATOR_INPUT_RULES.items()if k[0]=='dissolve'}=={('dissolve',k,''):v for k,v in expected.items()}
    for reason,param in expected.items():
        good=OperatorInputError(reason,'PRIVATE').diagnostic('dissolve',1);assert validated_operator_diagnostic(good)==good
        for patch in [{'field':'source_id'},{'parameter':'by'},{'node_index':True}]:
            bad=copy.deepcopy(good);bad['operator'].update(patch);assert validated_operator_diagnostic(bad)is None


@pytest.mark.parametrize('operator_id', ['contains', 'intersects'])
def test_relation_filter_three_closed_input_rules_without_identifiers_or_private_context(operator_id):
    import copy
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES, OperatorInputError, validated_operator_diagnostic
    expected={'operator_input_invalid':'input_gdf','operator_reference_invalid':'gdf_b','operator_crs_invalid':''}
    assert {key:value for key,value in OPERATOR_INPUT_RULES.items() if key[0]==operator_id} == {(operator_id,key,''):value for key,value in expected.items()}
    for reason,parameter in expected.items():
        value=OperatorInputError(reason,'PRIVATE original data').diagnostic(operator_id,2)
        assert value['operator']=={'node_index':2,'operator_id':operator_id,**({'parameter':parameter}if parameter else{})}
        assert validated_operator_diagnostic(value)==value
        for patch in ({'field':'source_id'},{'field':'PRIVATE'},{'parameter':'reference_gdf'},{'node_index':True}):
            bad=copy.deepcopy(value);bad['operator'].update(patch)
            assert validated_operator_diagnostic(bad) is None
    assert OperatorInputError('operator_overlay_fields_conflict','PRIVATE').diagnostic(operator_id,2) is None


def test_voronoi_three_closed_admissions_require_explicit_mask_not_reference_fields():
    import copy
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES,OperatorInputError,validated_operator_diagnostic
    expected={'operator_input_invalid':'input_gdf','operator_mask_invalid':'mask_gdf','operator_crs_invalid':''}
    assert {key:value for key,value in OPERATOR_INPUT_RULES.items()if key[0]=='voronoi'}=={('voronoi',key,''):value for key,value in expected.items()}
    for reason,parameter in expected.items():
        value=OperatorInputError(reason,'PRIVATE source geometry').diagnostic('voronoi',2)
        assert value['operator']=={'node_index':2,'operator_id':'voronoi',**({'parameter':parameter}if parameter else{})}
        assert validated_operator_diagnostic(value)==value
        for patch in ({'field':'station'},{'field':'PRIVATE'},{'parameter':'gdf_b'},{'node_index':True}):
            bad=copy.deepcopy(value);bad['operator'].update(patch)
            assert validated_operator_diagnostic(bad)is None
    assert OperatorInputError('operator_reference_invalid','PRIVATE').diagnostic('voronoi',2)is None


@pytest.mark.parametrize('operator_id', ['buffer', 'batch_buffer'])
def test_buffer_input_rules_keep_scalar_and_array_domains_closed(operator_id):
    import copy
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES, OperatorInputError, validated_operator_diagnostic

    expected = {
        ('buffer', 'operator_input_invalid', ''): 'input_gdf',
        ('buffer', 'operator_crs_invalid', ''): '',
        ('buffer', 'operator_number_invalid', ''): 'distance',
        ('buffer', 'operator_positive_integer_required', ''): 'resolution',
    } if operator_id == 'buffer' else {
        ('batch_buffer', 'operator_input_invalid', ''): 'input_gdf',
        ('batch_buffer', 'operator_crs_invalid', ''): '',
        ('batch_buffer', 'operator_field_conflict', 'distance'): 'input_gdf',
        ('batch_buffer', 'operator_number_array_invalid', ''): 'distances',
        ('batch_buffer', 'operator_positive_integer_required', ''): 'resolution',
    }
    assert {key: value for key, value in OPERATOR_INPUT_RULES.items() if key[0] == operator_id} == expected
    for (_, reason, field), parameter in expected.items():
        value = OperatorInputError(reason, 'PRIVATE distance=-3', field=field).diagnostic(operator_id, 2)
        assert value['operator'] == {'node_index': 2, 'operator_id': operator_id,
                                     **({'parameter': parameter} if parameter else {}),
                                     **({'field': field} if field else {})}
        assert validated_operator_diagnostic(value) == value
        assert 'PRIVATE' not in json.dumps(value)
        for patch in [
            {'field': 'PRIVATE'}, {'field': ''},
            {'parameter': 'distance' if parameter == 'distances' else 'distances'},
            {'operator_id': 'unknown_buffer'}, {'node_index': True},
            {'node_index': 0}, {'node_index': 65536}, {'node_index': 1.5},
        ]:
            bad = copy.deepcopy(value)
            bad['operator'].update(patch)
            assert validated_operator_diagnostic(bad) is None
        if not field:
            bad = copy.deepcopy(value)
            bad['operator']['field'] = 'distance'
            assert validated_operator_diagnostic(bad) is None

    assert OperatorInputError('operator_positive_number_required', 'PRIVATE').diagnostic(operator_id, 1) is None
    assert OperatorInputError('operator_number_array_invalid', 'PRIVATE').diagnostic('buffer', 1) is None
    assert OperatorInputError('operator_number_invalid', 'PRIVATE').diagnostic('batch_buffer', 1) is None


@pytest.mark.parametrize('operator_id,reason,parameter', [
    ('buffer', 'operator_input_invalid', 'input_gdf'),
    ('buffer', 'operator_crs_invalid', ''),
    ('buffer', 'operator_number_invalid', 'distance'),
    ('buffer', 'operator_positive_integer_required', 'resolution'),
    ('batch_buffer', 'operator_crs_invalid', ''),
])
def test_buffer_diagnostics_bind_original_definition_position_before_save(operator_id, reason, parameter):
    from addp_common.workflow_runtime import OperatorInputError

    calls = []
    def execute(operator, params):
        calls.append(operator)
        if operator == operator_id:
            raise OperatorInputError(reason, 'PRIVATE distance=-3')
        return {'kept': True}

    definition = {'tasks': [
        {'id': 'PRIVATE_BUFFER', 'operator': operator_id, 'params': {}, 'depends_on': ['load']},
        {'id': 'load', 'operator': 'load', 'params': {}, 'depends_on': []},
        {'id': 'save', 'operator': 'save', 'params': {}, 'depends_on': ['PRIVATE_BUFFER']},
    ]}
    with pytest.raises(WorkflowExecutionError) as caught:
        WorkflowRunner({'load', operator_id, 'save'}, execute).execute(definition)
    assert calls == ['load', operator_id]
    assert caught.value.error_code == 'INVALID_PARAMS'
    assert caught.value.all_results == {'load': {'kept': True}}
    assert caught.value.diagnostic == {
        'schema_version': 'addp.execution-failure/v3', 'stage': 'execution', 'reason': reason,
        'operator': {'node_index': 1, 'operator_id': operator_id,
                     **({'parameter': parameter} if parameter else {})},
    }
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)


def test_simplify_input_rules_are_exact_and_reject_foreign_parameter_domains():
    import copy
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES, OperatorInputError, validated_operator_diagnostic

    expected = {
        ('simplify', 'operator_input_invalid', ''): 'input_gdf',
        ('simplify', 'operator_crs_invalid', ''): '',
        ('simplify', 'operator_nonnegative_number_required', ''): 'tolerance',
        ('simplify', 'operator_boolean_required', ''): 'preserve_topology',
    }
    assert {key: value for key, value in OPERATOR_INPUT_RULES.items() if key[0] == 'simplify'} == expected
    for (_, reason, _), parameter in expected.items():
        value = OperatorInputError(reason, 'PRIVATE').diagnostic('simplify', 2)
        assert value['operator'] == {'node_index': 2, 'operator_id': 'simplify', **({'parameter': parameter} if parameter else {})}
        assert validated_operator_diagnostic(value) == value
        for patch in ({'field': ''}, {'field': 'tolerance'}, {'parameter': 'distance'}, {'operator_id': 'unknown_simplify'}, {'node_index': True}, {'node_index': 0}):
            forged = copy.deepcopy(value); forged['operator'].update(patch)
            assert validated_operator_diagnostic(forged) is None
    for operator in ('buffer', 'batch_buffer', 'distance_to', 'unknown_simplify'):
        for reason in ('operator_nonnegative_number_required', 'operator_boolean_required'):
            assert OperatorInputError(reason, 'PRIVATE').diagnostic(operator, 1) is None
    for reason in ('operator_positive_number_required', 'operator_number_invalid', 'operator_number_array_invalid'):
        assert OperatorInputError(reason, 'PRIVATE').diagnostic('simplify', 1) is None


@pytest.mark.parametrize('reason,parameter', [
    ('operator_input_invalid', 'input_gdf'), ('operator_crs_invalid', ''),
    ('operator_nonnegative_number_required', 'tolerance'), ('operator_boolean_required', 'preserve_topology'),
])
def test_simplify_diagnostics_keep_original_frozen_task_position_and_stop_before_save(reason, parameter):
    from addp_common.workflow_runtime import OperatorInputError
    calls = []
    def execute(operator, params):
        calls.append(operator)
        if operator == 'simplify': raise OperatorInputError(reason, 'PRIVATE')
        return {'kept': True}
    definition = {'tasks': [
        {'id': 'PRIVATE', 'operator': 'simplify', 'params': {}, 'depends_on': ['load']},
        {'id': 'load', 'operator': 'load', 'params': {}, 'depends_on': []},
        {'id': 'saved', 'operator': 'save', 'params': {}, 'depends_on': ['PRIVATE']},
    ]}
    with pytest.raises(WorkflowExecutionError) as caught:
        WorkflowRunner({'load', 'simplify', 'save'}, execute).execute(definition)
    assert calls == ['load', 'simplify'] and caught.value.error_code == 'INVALID_PARAMS'
    assert caught.value.all_results == {'load': {'kept': True}}
    assert caught.value.diagnostic == {'schema_version': 'addp.execution-failure/v3', 'stage': 'execution', 'reason': reason,
        'operator': {'node_index': 1, 'operator_id': 'simplify', **({'parameter': parameter} if parameter else {})}}
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)


@pytest.mark.parametrize('operator', ['centroid', 'convex_hull', 'minimum_rotated_rectangle'])
def test_enclosing_construction_diagnostics_have_only_two_no_field_rules(operator):
    import copy
    from addp_common.workflow_runtime.errors import OPERATOR_INPUT_RULES, OperatorInputError, validated_operator_diagnostic
    expected = {(operator, 'operator_input_invalid', ''): 'input_gdf', (operator, 'operator_crs_invalid', ''): ''}
    assert {key: value for key, value in OPERATOR_INPUT_RULES.items() if key[0] == operator} == expected
    for (_, reason, _), parameter in expected.items():
        value = OperatorInputError(reason, 'PRIVATE').diagnostic(operator, 2)
        assert value['operator'] == {'node_index': 2, 'operator_id': operator, **({'parameter': parameter} if parameter else {})}
        assert validated_operator_diagnostic(value) == value
        for patch in ({'field': ''}, {'field': 'geometry'}, {'parameter': 'distance'}, {'parameter': 'reference_gdf'}, {'operator_id': 'foreign'}, {'node_index': 0}, {'node_index': True}):
            forged = copy.deepcopy(value); forged['operator'].update(patch)
            assert validated_operator_diagnostic(forged) is None
    for reason in ('operator_reference_invalid', 'operator_number_invalid', 'operator_nonnegative_number_required', 'operator_boolean_required'):
        assert OperatorInputError(reason, 'PRIVATE').diagnostic(operator, 1) is None


@pytest.mark.parametrize('operator', ['centroid', 'convex_hull', 'minimum_rotated_rectangle'])
@pytest.mark.parametrize('reason,parameter', [('operator_input_invalid', 'input_gdf'), ('operator_crs_invalid', '')])
def test_enclosing_construction_diagnostics_bind_original_frozen_task_position(operator, reason, parameter):
    from addp_common.workflow_runtime import OperatorInputError
    calls = []
    def execute(name, params):
        calls.append(name)
        if name == operator: raise OperatorInputError(reason, 'PRIVATE')
        return {'kept': True}
    definition = {'tasks': [
        {'id': 'PRIVATE', 'operator': operator, 'params': {}, 'depends_on': ['load']},
        {'id': 'load', 'operator': 'load', 'params': {}, 'depends_on': []},
        {'id': 'saved', 'operator': 'save', 'params': {}, 'depends_on': ['PRIVATE']},
    ]}
    with pytest.raises(WorkflowExecutionError) as caught:
        WorkflowRunner({'load', operator, 'save'}, execute).execute(definition)
    assert calls == ['load', operator] and caught.value.error_code == 'INVALID_PARAMS'
    assert caught.value.all_results == {'load': {'kept': True}}
    assert caught.value.diagnostic == {'schema_version': 'addp.execution-failure/v3', 'stage': 'execution', 'reason': reason,
        'operator': {'node_index': 1, 'operator_id': operator, **({'parameter': parameter} if parameter else {})}}
    assert 'PRIVATE' not in json.dumps(caught.value.diagnostic)
