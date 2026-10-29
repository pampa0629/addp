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
        if op=='batch_buffer':raise OperatorInputError('operator_field_conflict','private password=secret')
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
    for (op,reason),_ in OPERATOR_INPUT_RULES.items():
        value=OperatorInputError(reason,'internal text').diagnostic(op,2)
        assert validated_operator_diagnostic(value)==value
        for patch in [lambda x:x.update(message='private'),lambda x:x.update(stage='validation'),
            lambda x:x['operator'].update(node_index=True),lambda x:x['operator'].update(field='private'),
            lambda x:x['operator'].update(parameter=None),lambda x:x['operator'].update(node_id='private')]:
            altered=copy.deepcopy(value);patch(altered);assert validated_operator_diagnostic(altered) is None
    workflow={'tasks':[{'id':'one','operator':'batch_buffer','params':{},'depends_on':[]}]}
    def raw(op,p):raise ValueError('INVALID_PARAMS distance conflicts password=secret')
    with pytest.raises(WorkflowExecutionError) as failed:WorkflowRunner({'batch_buffer'},raw).execute(workflow)
    assert failed.value.error_code=='EXECUTION_FAILED' and failed.value.diagnostic is None
