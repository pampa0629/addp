"""Business oracles reject fabricated zero/freshness and bounded request overshoot."""
from pathlib import Path
import importlib.util
import unittest
from datetime import datetime,timezone,timedelta
import copy
import json
import tempfile
from types import SimpleNamespace
from unittest import mock
spec=importlib.util.spec_from_file_location('http_local_online',Path(__file__).with_name('monitor-http-local-online.py'))
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)

class HTTPProductOracles(unittest.TestCase):
    def test_request_limits_count_attempts_before_transport(self):
        budget=m.RequestBudget()
        for _ in range(200):budget.before('/api/v1/system/auth/context')
        with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'control_request_limit'):budget.before('/api/v1/system/auth/context')
        budget=m.RequestBudget()
        with mock.patch.object(m.time,'monotonic',side_effect=range(400)):
            for _ in range(300):budget.before('/api/v1/agent/sessions')
            with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'controlled_request_limit'):budget.before('/api/v1/agent/sessions')

    def test_rate_limit_does_not_allow_retry_burst(self):
        budget=m.RequestBudget()
        with mock.patch.object(m.time,'monotonic',side_effect=[10,10.9]):
            budget.before('/api/v1/copilot/settings/inference-bindings/navigation_guide')
            with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'rate_limit'):budget.before('/api/v1/agent/sessions')

    def test_actual_source_increments_are_exact_for_matched_2xx_4xx_routes(self):
        after={'agent':{'/api/v1/agent/sessions|200':15,'/api/v1/agent/sessions/{session_id}|404':15,
            '/api/v1/agent/sessions/{session_id}|422':15},'copilot':{
            '/api/v1/copilot/settings/inference-bindings/{scenario_code}|200':15,
            '/api/v1/copilot/settings/inference-bindings/{scenario_code}|404':15}}
        m.verify_increments({},after)
        after['agent']['/api/v1/agent/sessions/{session_id}|404']=14
        with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'increment_mismatch'):m.verify_increments({},after)

    def test_inactive_or_empty_history_is_never_accepted_as_retained_data(self):
        with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'history_no_valid_points'):
            m.require_history({'data':[{'collection':{'state':'not_active'},'observations':[]}]})
        m.require_history({'data':[{'observations':[{'series':[{'points':[{'data_state':'valid','value':1}]}]}]}]})

    def test_stopped_source_requires_exact_identity_inactive_empty_and_new_agent_sample(self):
        stop=datetime.now(timezone.utc)-timedelta(seconds=70)
        instances=[{'id':1,'module_name':'agent','instance_id':'agent-owned','role':'backend'},
                   {'id':2,'module_name':'copilot','instance_id':'copilot-owned','role':'backend'}]
        payload={'data':[{'subject':instances[0].copy(),'collection':{'state':'collecting','sampled_at':datetime.now(timezone.utc).isoformat()},
            'observations':[{'series':[{'metric_key':'http.requests.per_second','points':[{'data_state':'valid','value':.1}]}]}]},
            {'subject':instances[1].copy(),'collection':{'state':'not_active'},'observations':[]}]}
        m.verify_stopped_sources(payload,instances,stop)
        for mutate in (lambda v:v['data'][1]['collection'].update(state='collecting'),
                       lambda v:v['data'][1]['collection'].update(state='failed'),
                       lambda v:v['data'][1].update(observations=[{'series':[]}]),
                       lambda v:v['data'][0]['collection'].update(sampled_at=(stop-timedelta(seconds=1)).isoformat()),
                       lambda v:v['data'][0]['subject'].update(id=999),
                       lambda v:v['data'][0]['observations'][0]['series'][0]['points'][0].update(data_state='stale')):
            value=copy.deepcopy(payload);mutate(value)
            with self.assertRaises(m.auth.LocalTraceAssertionFailure):m.verify_stopped_sources(value,instances,stop)

    def test_live_oracle_requires_all_seven_metrics_correct_subjects_and_zero_server_failure(self):
        instance={'id':1,'module_name':'agent','instance_id':'owned','role':'backend'}
        keys={'http.requests.per_second':1,'http.client_errors.percent':50,'http.failures.percent':0,
            'http.duration.mean_seconds':.1,'http.duration.p95_seconds':.2,'http.duration.p99_seconds':.3,'http.active_requests':0}
        def payload(state='collecting',changes=None):
            values={**keys,**(changes or {})}
            return {'window_seconds':60,'data':[{'subject':instance.copy(),'collection':{'state':state},'observations':[{'series':[
                {'metric_key':key,'points':[{'data_state':'valid','value':value}]} for key,value in values.items()]}]}]}
        m.validate_live(payload(),[instance])
        for item in (payload('failed'),payload(changes={'http.failures.percent':1}),payload(changes={'http.requests.per_second':0}),payload(changes={'http.client_errors.percent':0})):
            with self.assertRaises(m.auth.LocalTraceAssertionFailure):m.validate_live(item,[instance])
        with self.assertRaises(m.auth.LocalTraceAssertionFailure):m.validate_live(payload(),[{**instance,'id':2}])

class HTTPBrowserPhases(unittest.TestCase):
    def fixture(self,directory):
        life=SimpleNamespace(work=Path(directory),checkout=Path(directory),run_id='owned',label='owned.label',
            network='owned-network',browser_image='owned-image',request_budget=m.RequestBudget())
        life.private_json=lambda name,value:(life.work/name).write_text(json.dumps(value))
        life.request_budget.control=20
        def docker(*args,**kwargs):
            phase='stopped' if args[args.index('--name')+1].endswith('-stopped') else 'live'
            receipt={'schema':'addp.http-quality-browser-receipt/v1','run_id':'owned','phase':phase,'result':'passed',
                'api_requests':35 if phase=='live' else 25,'assertions':{key:True for key in (
                    'real_password_mfa','summary_ranking_trend_api_values','url_reload_restored','chinese_and_english',
                    'agent_sample_after_stop','stopped_page_empty')}}
            (life.work/('http-browser-'+phase+'-report.json')).write_text(json.dumps(receipt))
        life.docker=mock.Mock(side_effect=docker)
        return life

    def test_two_phases_share_request_budget_and_keep_distinct_receipts_and_names(self):
        with tempfile.TemporaryDirectory() as directory:
            life=self.fixture(directory);report={}
            m.browser(life,{},report,[])
            stop=datetime.now(timezone.utc)
            m.browser(life,{},report,[],stopped_at=stop)
            self.assertEqual(life.request_budget.browser,60);self.assertEqual(life.request_budget.control,80)
            self.assertEqual(set(report['browser']),{'live','stopped'})
            self.assertEqual(json.loads((life.work/'http-browser-stopped.json').read_text())['request_limit'],45)
            self.assertEqual(len({call.args[call.args.index('--name')+1] for call in life.docker.call_args_list}),2)
            self.assertTrue(all(call.kwargs['timeout']==300 for call in life.docker.call_args_list))
            self.assertEqual(json.loads((life.work/'http-browser-stopped.json').read_text())['stopped_at'],stop.isoformat())

    def test_phase_order_duplicate_or_exhausted_budget_never_starts_transport(self):
        with tempfile.TemporaryDirectory() as directory:
            life=self.fixture(directory)
            with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'phase_order'):
                m.browser(life,{}, {},[],stopped_at=datetime.now(timezone.utc))
            for count,control in ((80,100),(0,121)):
                life.request_budget.browser=count;life.request_budget.control=control
                with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'reserve_insufficient'):m.browser(life,{}, {},[])
            life.docker.assert_not_called()
            life.request_budget.browser=0;life.request_budget.control=20
            report={};m.browser(life,{},report,[])
            with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'phase_order'):m.browser(life,{},report,[])
            self.assertEqual(life.docker.call_count,1)

    def test_missing_wrong_or_incomplete_receipt_cannot_pass(self):
        for change in ({'run_id':'foreign'},{'phase':'stopped'},{'result':'failed'},{'assertions':{}},
                {'api_requests':81},{'api_requests':-1},{'api_requests':True}):
            with self.subTest(change=change),tempfile.TemporaryDirectory() as directory:
                life=self.fixture(directory);original=life.docker.side_effect
                def docker(*args,**kwargs):
                    original(*args,**kwargs)
                    path=life.work/'http-browser-live-report.json'
                    path.write_text(json.dumps({**json.loads(path.read_text()),**change}))
                life.docker.side_effect=docker
                with self.assertRaises(m.auth.LocalTraceAssertionFailure):m.browser(life,{}, {},[])
                self.assertEqual(life.docker.call_count,1)
        with tempfile.TemporaryDirectory() as directory:
            life=self.fixture(directory);life.docker.side_effect=None
            with self.assertRaisesRegex(m.auth.LocalTraceAssertionFailure,'receipt_failed'):m.browser(life,{}, {},[])

    def test_failed_transport_counts_attempts_and_preserves_first_failure(self):
        for malformed in (False,True):
            with self.subTest(malformed=malformed),tempfile.TemporaryDirectory() as directory:
                life=self.fixture(directory);original=life.docker.side_effect
                def docker(*args,**kwargs):
                    original(*args,**kwargs)
                    if malformed:(life.work/'http-browser-live-report.json').write_text('not-json')
                    raise RuntimeError('first_transport_failure')
                life.docker.side_effect=docker;report={}
                with self.assertRaisesRegex(RuntimeError,'first_transport_failure'):m.browser(life,{},report,[])
                self.assertEqual(life.request_budget.browser,0 if malformed else 35)
                self.assertEqual(life.docker.call_count,1)
                if malformed:self.assertEqual(report['browser_receipt_error'],'browser_receipt_invalid')

if __name__=='__main__':unittest.main()
