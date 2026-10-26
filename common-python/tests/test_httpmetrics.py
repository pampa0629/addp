"""Full HTTP facts are independent of trace sampling and collector availability."""
import asyncio
from datetime import datetime, timedelta, timezone
import ipaddress
import json
import os
from pathlib import Path
import ssl
import tempfile
import time
import unittest
from unittest.mock import patch
import urllib.request

from addp_common.httpmetrics import HTTPMetrics, MetricsUnavailable
from addp_common.httpmetrics import source
from addp_common.httpmetrics.server import HTTPObservationMiddleware
from addp_common.client.module_registry import ModuleRegistration
from prometheus_client.parser import text_string_to_metric_families


def values(metrics, name):
    return [sample for family in text_string_to_metric_families(metrics.render().decode()) for sample in family.samples if sample.name == name]


class HTTPFacts(unittest.IsolatedAsyncioTestCase):
    async def test_completion_stream_activity_and_no_trace_dependency(self):
        metrics = HTTPMetrics()
        async def app(scope, receive, send):
            scope['route'] = type('Route',(),{'path':'/items/{item}'})()
            self.assertEqual(values(metrics,'http_server_active_requests')[0].value, 1)
            await send({'type':'http.response.start','status':200})
            await send({'type':'http.response.body','body':b'private','more_body':True})
            self.assertEqual(values(metrics,'addp_http_server_requests_total')[0].value,0)
            await send({'type':'http.response.body','body':b'done'})
            self.assertEqual(values(metrics,'addp_http_server_requests_total')[0].value,1)
            self.assertEqual(values(metrics,'http_server_active_requests')[0].value,0)
            raise RuntimeError('background after completed response')
        sent=[]
        async def send(message):sent.append(message)
        with patch('addp_common.httpmetrics.server.process_metrics',return_value=metrics), patch('addp_common.httptrace.runtime.enabled',return_value=False):
            with self.assertRaises(RuntimeError):
                await HTTPObservationMiddleware(app,'agent')({'type':'http','method':'GET','path':'/items/SECRET','scheme':'http','headers':[]},None,send)
        self.assertEqual(values(metrics,'addp_http_server_requests_total')[0].value,1)
        self.assertEqual(values(metrics,'addp_http_server_failures_total')[0].value,0)
        self.assertNotIn('SECRET',metrics.render().decode())
        self.assertNotIn('private',metrics.render().decode())
        self.assertEqual(sent[-1]['body'],b'done')

    async def test_failures_ready_auth_unmatched_and_unknown_method_once(self):
        for status,error in [(503,None),(401,None),(404,None),(200,TimeoutError()),(200,asyncio.CancelledError()),(200,OSError())]:
            with self.subTest(status=status,error=type(error).__name__):
                metrics=HTTPMetrics()
                async def app(scope,receive,send):
                    await send({'type':'http.response.start','status':status})
                    if error:raise error
                    await send({'type':'http.response.body'})
                async def send(message):pass
                with patch('addp_common.httpmetrics.server.process_metrics',return_value=metrics),patch('addp_common.httptrace.runtime.enabled',return_value=False):
                    try:await HTTPObservationMiddleware(app,'copilot')({'type':'http','method':'ARBITRARY','path':'/sensitive','headers':[]},None,send)
                    except BaseException as caught:self.assertIs(caught,error)
                self.assertEqual(values(metrics,'addp_http_server_requests_total')[0].value,1)
                self.assertEqual(values(metrics,'addp_http_server_failures_total')[0].value,int(status>=500 or error is not None))
                self.assertEqual(values(metrics,'addp_http_server_client_errors_total')[0].value,int(400<=status<500))
                self.assertEqual(values(metrics,'http_server_active_requests')[0].value,0)
                sample=values(metrics,'http_server_request_duration_seconds_count')[0]
                self.assertEqual(sample.labels['http_request_method'],'_OTHER')
                self.assertEqual(sample.labels['http_route'],'')

    async def test_send_failure_and_disconnect_share_single_finish(self):
        metrics=HTTPMetrics()
        async def app(scope,receive,send):
            await receive()
            await send({'type':'http.response.body'})
        async def receive():return {'type':'http.disconnect'}
        async def send(message):raise OSError('SECRET')
        with patch('addp_common.httpmetrics.server.process_metrics',return_value=metrics),patch('addp_common.httptrace.runtime.enabled',return_value=False):
            with self.assertRaises(OSError):await HTTPObservationMiddleware(app,'agent')({'type':'http','method':'GET','path':'/','headers':[]},receive,send)
        self.assertEqual(values(metrics,'addp_http_server_requests_total')[0].value,1)
        self.assertEqual(values(metrics,'addp_http_server_failures_total')[0].value,1)
        self.assertEqual(values(metrics,'http_server_active_requests')[0].value,0)


class CollectorLimits(unittest.TestCase):
    def test_private_registry_no_unrequested_process_or_gc_collectors(self):
        metrics=HTTPMetrics();metrics.activate()
        body=metrics.render().decode()
        for name in ('process_cpu','process_resident','python_gc','python_info'):
            self.assertNotIn(name,body)
        self.assertEqual(values(metrics,'addp_http_server_requests_total')[0].value,0)
        self.assertEqual(values(metrics,'addp_http_server_instrumentation_start_time_seconds')[0].value,metrics.started)

    def test_cardinality_overflow_latches_failure_without_growing_or_losing_total(self):
        m=HTTPMetrics()
        for i in range(514):
            m.enter('GET','http');m.finish('GET','http',f'/route{i}',200,'',.25)
        self.assertEqual(len(m.rows),512);self.assertEqual(m.total[0],514)
        with self.assertRaises(MetricsUnavailable):m.render()
        m.enter('GET','http');m.finish('GET','http','/route0',200,'',.25)
        with self.assertRaises(MetricsUnavailable):m.render()
        self.assertEqual(m.active[('GET','http')],0)

    def test_output_and_application_budget_fail_closed(self):
        for constant in ('MAX_OUTPUT','MAX_APPLICATION'):
            m=HTTPMetrics();m.enter('GET','http');m.finish('GET','http','/route',200,'',.1)
            with patch('addp_common.httpmetrics.'+constant,1),self.assertRaises(MetricsUnavailable):m.render()
            self.assertTrue(m.failed)

    def test_histogram_boundaries_and_cross_thread_facts(self):
        m=HTTPMetrics()
        def record():
            for _ in range(50):m.enter('POST','https');m.finish('POST','https','/route',500,'500',1.)
        import threading
        threads=[threading.Thread(target=record) for _ in range(4)]
        for t in threads:t.start()
        for t in threads:t.join()
        self.assertEqual(values(m,'addp_http_server_requests_total')[0].value,200)
        buckets=values(m,'http_server_request_duration_seconds_bucket')
        self.assertEqual(len(buckets),15)
        self.assertEqual(next(s.value for s in buckets if s.labels['le']=='1.0'),200)

    def test_strict_deployment_and_registration(self):
        d={'module_name':'agent','role':'backend','listen':'127.0.0.1:19401','endpoint':'https://127.0.0.1:19401/metrics','tls_dir':'/external/source'}
        self.assertEqual(source.deployment(json.dumps([d]),'agent','backend'),d)
        self.assertIsNone(source.deployment('','agent','backend'))
        for entry in (dict(d,extra='SECRET'),dict(d,cgroup_v2=True),dict(d,listen='0.0.0.0:19401'),dict(d,endpoint=d['endpoint']+'?x=1')):
            with self.assertRaises(ValueError):source.deployment(json.dumps([entry]),'agent','backend')
        for raw in (json.dumps([d,d]),json.dumps([d])+' []','['+' '* (256<<10)+']'):
            with self.assertRaises(ValueError):source.deployment(raw,'agent','backend')
        r=ModuleRegistration(module_name='agent',module_url='http://localhost:1',route_prefix='/agent')
        self.assertNotIn('process_metrics',r.model_dump(exclude_none=True))
        with patch.dict(os.environ,{source.ENV:json.dumps([d])}),patch.object(source,'source_tls',side_effect=ValueError('SECRET')):
            self.assertIsNone(source.start_optional(r))


def pki(root):
    from cryptography import x509
    from cryptography.x509.oid import NameOID,ExtendedKeyUsageOID
    from cryptography.hazmat.primitives import hashes,serialization
    from cryptography.hazmat.primitives.asymmetric import ec
    now=datetime.now(timezone.utc)
    key=ec.generate_private_key(ec.SECP256R1());name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'owned CA')])
    ca=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-timedelta(minutes=1)).not_valid_after(now+timedelta(hours=1)).add_extension(x509.BasicConstraints(ca=True,path_length=None),critical=True).sign(key,hashes.SHA256())
    for role in ('server','client'):
        leafkey=ec.generate_private_key(ec.SECP256R1())
        leaf=x509.CertificateBuilder().subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,role)])).issuer_name(name).public_key(leafkey.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-timedelta(minutes=1)).not_valid_after(now+timedelta(hours=1)).add_extension(x509.BasicConstraints(ca=False,path_length=None),critical=True).add_extension(x509.SubjectAlternativeName([x509.IPAddress(ipaddress.ip_address('127.0.0.1'))]),critical=False).add_extension(x509.ExtendedKeyUsage([ExtendedKeyUsageOID.SERVER_AUTH if role=='server' else ExtendedKeyUsageOID.CLIENT_AUTH]),critical=False).sign(key,hashes.SHA256())
        (root/(role+'.crt')).write_bytes(leaf.public_bytes(serialization.Encoding.PEM))
        (root/(role+'.key')).write_bytes(leafkey.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()))
        (root/(role+'.key')).chmod(0o600)
    (root/'ca.crt').write_bytes(ca.public_bytes(serialization.Encoding.PEM))


class SourceTLSRejection(unittest.TestCase):
    def test_tls_permissions_san_symlinks_and_extra_files(self):
        import shutil
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);pki(root);private=root/'source';private.mkdir(mode=0o700)
            for name in ('ca.crt','server.crt','server.key'):shutil.copy2(root/name,private/name)
            source.source_tls(str(private),'https://127.0.0.1:1/metrics')
            with self.assertRaises(ValueError):source.source_tls(str(private),'https://127.0.0.2:1/metrics')
            (private/'server.key').chmod(0o644)
            with self.assertRaises(ValueError):source.source_tls(str(private),'https://127.0.0.1:1/metrics')
            (private/'server.key').chmod(0o600)
            (private/'extra').touch()
            with self.assertRaises(ValueError):source.source_tls(str(private),'https://127.0.0.1:1/metrics')
            (private/'extra').unlink();(private/'server.key').unlink();(private/'server.key').symlink_to(root/'server.key')
            with self.assertRaises(OSError):source.source_tls(str(private),'https://127.0.0.1:1/metrics')


class SourceContract(unittest.TestCase):
    def test_real_mtls_fixed_endpoint_identity_and_graceful_close(self):
        import shutil
        import socket
        with tempfile.TemporaryDirectory() as folder:
            root=Path(folder);pki(root)
            server=root/'source';server.mkdir(mode=0o700)
            for name in ('ca.crt','server.crt','server.key'):shutil.copy(root/name,server/name)
            sock=socket.socket();sock.bind(('127.0.0.1',0));port=sock.getsockname()[1];sock.close()
            reg=ModuleRegistration(module_name='agent',module_url='http://localhost:1',route_prefix='/agent')
            entry={'module_name':'agent','role':'backend','listen':f'127.0.0.1:{port}','endpoint':f'https://127.0.0.1:{port}/metrics','tls_dir':str(server)}
            m=HTTPMetrics();m.activate()
            with patch.dict(os.environ,{source.ENV:json.dumps([entry])}),patch.object(source,'process',return_value=m):
                running=source.start_optional(reg)
            self.assertIsNotNone(running)
            try:
                ctx=ssl.create_default_context(cafile=str(root/'ca.crt'));ctx.load_cert_chain(root/'client.crt',root/'client.key')
                with urllib.request.urlopen(entry['endpoint'],context=ctx,timeout=4) as response:
                    body=response.read().decode()
                self.assertIn('addp_process_identity_info',body);self.assertNotIn('process_cpu',body)
                self.assertEqual(running.declaration.schema_version,'addp.process-metrics/v1')
                from urllib.error import HTTPError
                for path,status in (('/metrics?x=1',404),('/other',404)):
                    with self.assertRaises(HTTPError) as caught:urllib.request.urlopen(f'https://127.0.0.1:{port}'+path,context=ctx,timeout=4)
                    self.assertEqual(caught.exception.code,status)
                bad=ssl.create_default_context(cafile=str(root/'ca.crt'))
                with self.assertRaises(Exception):urllib.request.urlopen(entry['endpoint'],context=bad,timeout=4)
                m.failed=True
                with self.assertRaises(HTTPError) as caught:urllib.request.urlopen(entry['endpoint'],context=ctx,timeout=4)
                self.assertEqual(caught.exception.code,503)
            finally:running.close()
            self.assertFalse(running.thread.is_alive())
