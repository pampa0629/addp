import pytest

from addp_common.workflow_runtime import ExecutionRegistry, WorkflowRunner
from addp_common.workflow_runtime.admission import AdmissionLimits, ExecutionAdmission, RuntimeBusy


class DeferredThreads:
    def __init__(self):
        self.jobs = []

    def __call__(self, *, target, args, daemon):
        assert daemon and args == ()
        class Thread:
            def start(inner):
                self.jobs.append(target)
        return Thread()


def test_registry_bounded_queue_starts_only_running_jobs():
    admission = ExecutionAdmission(AdmissionLimits(2, 2), AdmissionLimits(2, 2))
    threads, registry = DeferredThreads(), ExecutionRegistry()
    seen = []
    runner = WorkflowRunner({'echo'}, lambda op, params: seen.append(params['id']) or params['id'])
    def submit(i):
        return registry.submit(runner, {'tasks': [{'id': 'a', 'operator': 'echo', 'params': {'id': i}, 'depends_on': []}]},
                               admission=admission, tenant_id=7, thread_factory=threads)
    snapshots = [submit(i) for i in range(4)]
    assert len(threads.jobs) == 2 and seen == []
    assert admission.snapshot()['waiting'] == 2
    with pytest.raises(RuntimeBusy):
        submit(4)
    assert len(registry._executions) == 4
    assert all(registry.get(item.execution_id).status == 'pending' for item in snapshots)
    threads.jobs[0]()
    assert len(threads.jobs) == 3 and seen == [0]
    threads.jobs[1]()
    threads.jobs[2]()
    threads.jobs[3]()
    assert seen == [0, 1, 2, 3]
    assert all(registry.get(item.execution_id).status == 'success' for item in snapshots)
    assert admission.snapshot()['running'] == admission.snapshot()['waiting'] == 0


def test_direct_and_workflow_share_capacity_and_tenant_quota():
    admission = ExecutionAdmission(AdmissionLimits(2, 2), AdmissionLimits(1, 2))
    threads, seen = DeferredThreads(), []
    with admission.direct(7):
        admission.submit(7, lambda: seen.append(7), thread_factory=threads)
        assert len(threads.jobs) == 0
        with pytest.raises(RuntimeBusy):
            with admission.direct(7):
                pass
        with admission.direct(8):
            with pytest.raises(RuntimeBusy):
                with admission.direct(9):
                    pass
    assert len(threads.jobs) == 1
    threads.jobs[0]()
    assert seen == [7]


def test_shrink_retains_accepted_work_and_pause_blocks_dispatch():
    admission = ExecutionAdmission(AdmissionLimits(2, 2), AdmissionLimits(2, 2))
    threads, seen = DeferredThreads(), []
    for i in range(4):
        admission.submit(7, lambda i=i: seen.append(i), thread_factory=threads)
    admission.configure(AdmissionLimits(1, 0), AdmissionLimits(1, 0), {})
    with pytest.raises(RuntimeBusy):
        admission.submit(7, lambda: None, thread_factory=threads)
    threads.jobs[0]()
    assert len(threads.jobs) == 2
    admission.pause()
    threads.jobs[1]()
    assert len(threads.jobs) == 2 and admission.snapshot()['waiting'] == 2
    admission.configure(AdmissionLimits(1, 0), AdmissionLimits(1, 0), {})
    threads.jobs[2]()
    threads.jobs[3]()
    assert seen == [0, 1, 2, 3]


def test_blocked_tenant_does_not_block_eligible_tenant():
    admission = ExecutionAdmission(AdmissionLimits(2, 3), AdmissionLimits(1, 3))
    threads, seen = DeferredThreads(), []
    for tenant in [1, 2, 1, 2]:
        admission.submit(tenant, lambda tenant=tenant: seen.append(tenant), thread_factory=threads)
    threads.jobs[1]()
    assert len(threads.jobs) == 3
    threads.jobs[2]()
    assert seen == [2, 2]
    threads.jobs[0]()
    threads.jobs[3]()
    assert seen == [2, 2, 1, 1]


def test_failed_execution_releases_capacity():
    registry, threads = ExecutionRegistry(), DeferredThreads()
    admission = ExecutionAdmission(AdmissionLimits(1, 1), AdmissionLimits(1, 1))
    runner = WorkflowRunner({'fail'}, lambda *_: 1 / 0)
    workflow = {'tasks': [{'id': 'a', 'operator': 'fail', 'params': {}, 'depends_on': []}]}
    first = registry.submit(runner, workflow, admission=admission, tenant_id=1, thread_factory=threads)
    second = registry.submit(runner, workflow, admission=admission, tenant_id=2, thread_factory=threads)
    threads.jobs[0]()
    threads.jobs[1]()
    assert registry.get(first.execution_id).status == registry.get(second.execution_id).status == 'failed'
    assert admission.snapshot()['running'] == 0


@pytest.mark.parametrize('running,waiting', [(True, 1), (0, 1), (1, True), (1, -1)])
def test_admission_rejects_invalid_limits(running, waiting):
    with pytest.raises(ValueError):
        AdmissionLimits(running, waiting)


def test_queued_thread_start_failure_is_terminal_and_dispatch_continues():
    admission = ExecutionAdmission(AdmissionLimits(1, 2), AdmissionLimits(1, 2))
    registry, threads = ExecutionRegistry(), DeferredThreads()
    runner = WorkflowRunner({'echo'}, lambda *_: 'ok')
    workflow = {'tasks': [{'id': 'a', 'operator': 'echo', 'params': {}, 'depends_on': []}]}
    def broken(**kwargs):
        class Thread:
            def start(self):
                raise RuntimeError('thread limit')
        return Thread()
    first = registry.submit(runner, workflow, admission=admission, tenant_id=1, thread_factory=threads)
    failed = registry.submit(runner, workflow, admission=admission, tenant_id=1, thread_factory=broken)
    last = registry.submit(runner, workflow, admission=admission, tenant_id=1, thread_factory=threads)
    threads.jobs[0]()
    assert registry.get(failed.execution_id).status == 'failed'
    assert registry.get(failed.execution_id).details == 'thread limit'
    threads.jobs[1]()
    assert registry.get(first.execution_id).status == registry.get(last.execution_id).status == 'success'
    assert admission.snapshot()['running'] == admission.snapshot()['waiting'] == 0
    with pytest.raises(RuntimeError, match='thread limit'):
        registry.submit(runner, workflow, admission=admission, tenant_id=1, thread_factory=broken)
    assert len(registry._executions) == 3


def test_registry_admission_does_not_hold_registry_lock_across_start():
    # A completion may hold admission's lock while updating the execution registry.
    # Starting a new execution must not hold the registry lock in the reverse order.
    import threading
    registry = ExecutionRegistry()
    runner = WorkflowRunner({'echo'}, lambda *_: 'ok')
    workflow = {'tasks': [{'id': 'a', 'operator': 'echo', 'params': {}, 'depends_on': []}]}
    class CheckingAdmission:
        def submit(self, tenant, job, **kwargs):
            done = threading.Event()
            def inspect():
                registry.get(next(iter(registry._executions)))
                done.set()
            worker = threading.Thread(target=inspect, daemon=True)
            worker.start()
            assert done.wait(2), 'registry lock held during admission'
            worker.join()
            job()
    accepted = registry.submit(runner, workflow, admission=CheckingAdmission(), tenant_id=1)
    assert registry.get(accepted.execution_id).status == 'success'
