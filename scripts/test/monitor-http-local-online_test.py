"""Business oracles reject fabricated zero/freshness and bounded request overshoot."""
from pathlib import Path
import importlib.util
import unittest
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

if __name__=='__main__':unittest.main()
