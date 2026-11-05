"""Deterministic local Trace lifecycle contracts; never execute Docker/services."""
from pathlib import Path
import copy
import importlib.util
import json
import os
import signal
import tempfile
import threading
import unittest
import urllib.parse
from types import SimpleNamespace
from unittest import mock

spec = importlib.util.spec_from_file_location('monitor_traces_local_gate', Path(__file__).with_name('monitor-traces-local-gate.py'))
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class IncrementalBuildProgressTests(unittest.TestCase):
    def observe(self, raw, chunks=32768):
        observer=m.BuildProgress()
        with tempfile.TemporaryFile() as output:
            for offset in range(0,len(raw),chunks):
                output.seek(0,2);output.write(raw[offset:offset+chunks]);output.flush()
                observer.poll(output)
            result=observer.snapshot(output)
        return result

    def test_split_lines_and_large_body_preserve_only_the_unique_stage(self):
        for command,stage in [('apt-get update','apt'),('pip install -r /tmp/SECRET','python_dependency'),
                ('pip install --no-deps /tmp/common-python','common_package'),
                ('python -m pip check','verification'),('echo SECRET','other')]:
            raw=('#1 [1/2] RUN apt-get update\n#1 DONE 0.1s\n#2 [2/2] RUN '+command+'\n').encode()
            for size in (1,7,32768):
                with self.subTest(stage=stage,size=size):
                    result=self.observe(raw,size)
                    self.assertEqual(result['progress']['stage'],stage);self.assertEqual(result['progress']['status'],'unique')
                    self.assertNotIn('SECRET',repr(result));self.assertNotIn('/tmp/',repr(result))
            result=self.observe(raw+b'#2 1.0 SECRET body\n'*12000)
            self.assertGreater(result['scanned_bytes'],131072);self.assertEqual(result['progress']['stage'],stage)
            self.assertLessEqual(len(json.dumps(result).encode()),4096)

    def test_parallel_duplicate_finished_unknown_and_canceled_steps_are_explicit(self):
        for raw,status in [(b'#1 [1/2] RUN apt-get update\n#2 [2/2] RUN pip install SECRET\n','ambiguous'),
                (b'#1 [1/1] RUN apt-get update\n#1 [1/1] RUN echo SECRET\n','invalid'),
                (b'#1 [1/1] RUN apt-get update\n#10 DONE 0.1s\n','invalid'),
                (b'#1 [1/1] RUN apt-get update\n#100000 [2/2] RUN echo SECRET\n','invalid'),
                (b'#1 [1/1] RUN apt-get update\n#2 [2/2] RUN echo SECRET\r#3 [3/3] RUN apt-get update\n','invalid'),
                (b'#1 [1/1] RUN apt-get update\n#1 DONE 0.1s\n','unavailable'),
                (b'#1 [1/1] RUN apt-get update\n#1 CACHED\n','unavailable'),
                (b'#1 [1/1] RUN apt-get update\n#1 ERROR: SECRET\n','unavailable')]:
            with self.subTest(status=status):
                value=self.observe(raw);self.assertEqual(value['progress'],{'status':status,'stage':'unknown'})
        value=self.observe(b'#1 [1/1] RUN apt-get update\n#1 CANCELED\n')
        self.assertEqual(value['progress']['stage'],'apt');self.assertEqual(value['progress']['state'],'canceled')

    def test_line_step_byte_and_cpu_limits_never_report_stale_stage(self):
        for raw in [b'#1 [1/1] RUN apt-get update\n'+b'X'*4096+b'\n',
                    ''.join('#'+str(i)+' [1/1] RUN echo SECRET\n'for i in range(65)).encode(),
                    b'#1 [1/1] RUN apt-get update\n'+b'body\n'*(8*1024**2//5+1)]:
            value=self.observe(raw);self.assertEqual(value['status'],'over_budget');self.assertEqual(value['progress']['stage'],'unknown')
        calls=[0]
        def cpu():calls[0]+=1;return 0 if calls[0]==1 else 1.01
        with mock.patch.object(m.time,'thread_time',side_effect=cpu):
            value=self.observe(b'#1 [1/1] RUN apt-get update\n')
        self.assertEqual(value['status'],'over_budget');self.assertEqual(value['progress']['stage'],'unknown')

    def test_exact_byte_line_and_step_boundaries_and_summary_limit(self):
        raw=b'#1 [1/1] RUN apt-get update\n'
        size=8*1024**2-len(raw);raw+=(b'b'*4095+b'\n')*(size//4096)+b'b'*(size%4096-1)+b'\n'
        value=self.observe(raw)
        self.assertEqual(value['status'],'complete');self.assertEqual(value['scanned_bytes'],8*1024**2)
        value=self.observe(b'#1 [1/1] RUN apt-get update\n'+b'b'*4095+b'\n')
        self.assertEqual(value['progress']['stage'],'apt')
        raw=''.join('#'+str(i)+' [1/1] RUN echo SECRET\n#'+str(i)+' DONE 0.1s\n'for i in range(63)).encode()
        value=self.observe(raw+b'#63 [1/1] RUN apt-get update\n')
        self.assertEqual(value['steps_observed'],64);self.assertEqual(value['progress']['stage'],'apt')
        with mock.patch.object(m.json,'dumps',return_value='x'*4097):value=self.observe(b'#1 [1/1] RUN apt-get update\n')
        self.assertEqual(value['status'],'over_budget');self.assertEqual(value['progress']['stage'],'unknown')

    def test_unread_tail_truncation_shrink_and_read_failure_are_not_current_stage(self):
        for mode in ('unread','truncated','shrink','read_error'):
            with self.subTest(mode=mode),tempfile.TemporaryFile() as output:
                o=m.BuildProgress();output.write(b'#1 [1/1] RUN apt-get update\n');output.flush();o.poll(output)
                if mode=='unread':output.write(b'body\n'*20000);output.flush()
                elif mode=='truncated':output.write(b'partial SECRET');output.flush()
                elif mode=='shrink':output.truncate(0)
                if mode=='read_error':
                    with mock.patch.object(m.os,'pread',side_effect=OSError('SECRET')):value=o.snapshot(output)
                else:value=o.snapshot(output)
                self.assertEqual(value['status'],mode if mode in ('unread','truncated') else 'invalid' if mode=='shrink' else 'unavailable')
                self.assertEqual(value['progress']['stage'],'unknown');self.assertNotIn('SECRET',repr(value))

    def test_position_reads_are_bounded_and_do_not_change_writer_position(self):
        with tempfile.TemporaryFile() as output:
            output.write(b'body\n'*20000);output.flush();position=output.tell();o=m.BuildProgress()
            with mock.patch.object(m.os,'pread',wraps=m.os.pread) as read:
                o.poll(output);self.assertEqual(read.call_args.args[1],32768)
            self.assertEqual(output.tell(),position);self.assertEqual(o.scanned_bytes,32768)

    def test_exact_product_command_observes_in_original_loop_and_keeps_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.phase='product_image_gateway';clock=[0.0];events=[]
            process=mock.Mock(pid=654321,returncode=-15);process.poll.side_effect=lambda: -15 if events else None
            def start(*args,**kwargs):kwargs['stdout'].write(b'#1 [1/1] RUN pip install SECRET\n');kwargs['stdout'].flush();return process
            def wait(**kwargs):clock[0]=601;raise m.subprocess.TimeoutExpired('SECRET',kwargs['timeout'])
            process.wait.side_effect=lambda **kwargs: None if events else wait(**kwargs)
            command=['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services gateway --registry localhost:5001 --tag '+life.run_id]
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]),mock.patch.object(m.time,'monotonic_ns',side_effect=lambda:round(clock[0]*1e9)),mock.patch.object(m.subprocess,'Popen',side_effect=start) as spawn,mock.patch.object(m.os,'killpg',side_effect=lambda *a:events.append('cancel')):
                with self.assertRaisesRegex(m.GateError,'^command_deadline_exceeded$'):life.command(command,timeout=600)
            self.assertEqual(spawn.call_count,1);self.assertEqual(events,['cancel'])
            self.assertEqual(life.last_failure['termination'],'deadline');self.assertNotIn('output_sha256',life.last_failure)
            self.assertEqual(life.last_failure['public_build']['progress']['stage'],'python_dependency')
            self.assertNotIn('SECRET',repr(life.last_failure))

    def test_scope_and_observer_errors_preserve_the_original_exception(self):
        for sampling,command,error_text in [(True,['make','build-images'],'command_deadline_exceeded'),
                (False,['private-login'],'command_deadline_exceeded'),(False,None,'owned_container_oom'),
                (False,['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services gateway --registry localhost:5001 --tag OTHER'],'command_deadline_exceeded'),
                (False,None,'command_deadline_exceeded')]:
            with self.subTest(sampling=sampling,error=error_text),tempfile.TemporaryDirectory() as directory,tempfile.TemporaryFile() as output:
                life=bare_lifecycle(directory);life.phase='product_image_gateway'
                args=command or ['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services gateway --registry localhost:5001 --tag '+life.run_id]
                original=m.GateError(error_text)
                with mock.patch.object(m.os,'pread',side_effect=OSError('SECRET')):
                    with self.assertRaises(m.GateError) as caught:
                        with m.observe_product_build(life,args,output,sampling) as observer:
                            if observer:observer.poll(output)
                            raise original
                self.assertIs(caught.exception,original)
                if command is None and error_text=='command_deadline_exceeded':self.assertEqual(life.last_failure['public_build']['status'],'unavailable')
                else:self.assertFalse(hasattr(life,'last_failure'))

    def test_observation_time_exhausting_command_deadline_cancels_without_extra_wait(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.phase='product_image_gateway';clock=[0.0];events=[]
            process=mock.Mock(pid=654321,returncode=-15);process.poll.side_effect=lambda: -15 if events else None
            def consume(observer,output):clock[0]=601
            command=['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services gateway --registry localhost:5001 --tag '+life.run_id]
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]),mock.patch.object(m.time,'monotonic_ns',side_effect=lambda:round(clock[0]*1e9)),mock.patch.object(m.subprocess,'Popen',return_value=process),mock.patch.object(m.os,'killpg',side_effect=lambda *a:events.append('cancel')),mock.patch.object(m.BuildProgress,'poll',consume):
                with self.assertRaisesRegex(m.GateError,'^command_deadline_exceeded$'):life.command(command,timeout=600)
            self.assertEqual(events,['cancel']);process.wait.assert_called_once_with(timeout=1)

    def test_resource_cancel_freezes_the_same_build_before_signalling_without_reading(self):
        failure_code = 'owned_resource_measurement_failed'
        for scenario, status, stage in (
            ('unique', 'unique', 'python_dependency'),
            ('competing', 'ambiguous', 'unknown'),
            ('unread', 'unread', 'unknown'),
            ('truncated', 'truncated', 'unknown'),
            ('foreign_phase', 'unattributed', 'unknown'),
            ('stale_sample', 'unattributed', 'unknown'),
            ('deadline', 'deadline', 'unknown'),
            ('diagnostic_error', 'unavailable', 'unknown'),
        ):
            with self.subTest(scenario=scenario), tempfile.TemporaryDirectory() as directory:
                life = bare_lifecycle(directory)
                life.phase = 'product_image_agent_backend'
                life.product_services = ('agent-backend',)
                command = ['make','build-images','IMAGE_BUILD_ARGS=--verify --jobs 1 --services agent-backend'
                           ' --registry localhost:5001 --tag '+life.run_id]
                clock = [1.0]
                process = mock.Mock(pid=654321, returncode=-15)
                output = []
                events = []
                process.poll.side_effect = lambda: -15 if events else None
                def start(*args, **kwargs):
                    output.append(kwargs['stdout'])
                    output[0].write(b'#1 [1/2] RUN pip install PRIVATE\n')
                    if scenario == 'competing':
                        output[0].write(b'#2 [2/2] RUN apt-get update\n')
                    if scenario == 'truncated':
                        output[0].write(b'#2 [2/')
                    output[0].flush()
                    return process
                def wait(**kwargs):
                    if events:
                        return -15
                    if scenario == 'unread':
                        output[0].write(b'#1 DONE\n')
                        output[0].flush()
                    clock[0] = 602.0 if scenario == 'deadline' else 2.0
                    life.resource_failure = {'error_code': failure_code,
                        'phase': 'foreign' if scenario == 'foreign_phase' else life.phase,
                        'sample_started_monotonic': 0.2 if scenario == 'stale_sample' else 1.2,
                        'sample_finished_monotonic': 1.8}
                    life.failure = failure_code
                    raise m.subprocess.TimeoutExpired('synthetic', kwargs['timeout'])
                process.wait.side_effect = wait
                def kill(*args):
                    events.append(('cancel', copy.deepcopy(life.last_failure)))
                    output[0].write(b'#1 CANCELED\n')
                    output[0].flush()
                def snapshot(observer, out, *, final_read):
                    if scenario == 'diagnostic_error':
                        raise OSError('PRIVATE')
                    return original_snapshot(observer, out, final_read=final_read)
                original_snapshot = m.BuildProgress.snapshot
                with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                     mock.patch.object(m.time, 'monotonic_ns', side_effect=lambda: round(clock[0]*1e9)), \
                     mock.patch.object(m.subprocess, 'Popen', side_effect=start), \
                     mock.patch.object(m.os, 'killpg', side_effect=kill), \
                     mock.patch.object(m.os, 'pread', wraps=m.os.pread) as reads, \
                     mock.patch.object(m.BuildProgress, 'snapshot', snapshot):
                    with self.assertRaisesRegex(m.GateError, '^'+failure_code+'$'):
                        life.command(command, timeout=600)
                self.assertEqual(reads.call_count, 1)
                self.assertEqual(events, [('cancel', life.last_failure)])
                diagnostic = life.last_failure
                self.assertEqual(diagnostic['termination'], 'resource_guard')
                self.assertEqual(diagnostic['build_command']['run_id'], life.run_id)
                self.assertEqual(diagnostic['build_command']['command_id'], 1)
                self.assertEqual(diagnostic['build_command']['phase'], life.phase)
                self.assertEqual(diagnostic['public_build']['progress']['status'], status)
                self.assertEqual(diagnostic['public_build']['progress']['stage'], stage)
                self.assertNotIn('PRIVATE', repr(diagnostic))


class PublicAPTBuildEvidence(unittest.TestCase):
    def output(self,body,*,prefix='',step='7'):
        return (prefix+'\n#'+step+' [3/9] RUN apt-get update && apt-get install -y gcc postgresql-client\n'+
                '\n'.join('#'+step+' 1.0 '+line for line in body.splitlines())+'\n#'+step+
                ' ERROR: process "/bin/sh -c apt-get update && apt-get install -y gcc postgresql-client" did not complete successfully: exit code: 100\nDockerfile:5\n').encode()

    def test_scopes_only_the_unique_failed_apt_step(self):
        prefix='#2 [1/3] RUN apt-get update\n#2 4.0 Err:1 http://deb.debian.org/debian/dists/bookworm/InRelease\n#2 4.0 E: Connection timed out\nCache image not found; timed out PRIVATE-TOKEN'
        v=m.public_build_failure_diagnostic(self.output('E: Unable to locate package PRIVATE-PACKAGE',prefix=prefix))
        self.assertIn('apt',v)
        self.assertEqual(v['apt']['error_kinds'],['package_missing']);self.assertEqual(v['apt']['phase'],'unknown')
        self.assertEqual(v['apt']['sources'],[]);self.assertNotIn('PRIVATE',repr(v))
        self.assertEqual(v['process_exit_codes'],[100])

    def transport(self,body,**kwargs):
        apt=m.public_build_failure_diagnostic(self.output(body,**kwargs))['apt']
        self.assertIn('transport',apt)
        return apt['transport']

    def test_transport_connect_uses_error_endpoint_not_fetch_url(self):
        for phrase in ('Could not connect to',"Couldn't connect to",'Failed to connect to','Unable to connect to'):
            with self.subTest(phrase=phrase):
                v=self.transport('E: Failed to fetch http://deb.debian.org/debian/pool/main/p/pkg.deb '+
                    phrase+' http.docker.internal:3128 (192.0.2.17), connection timed out')
                self.assertEqual(v,{'operation':'connect','operation_status':'explicit',
                    'endpoint':'desktop_proxy','endpoint_status':'explicit'})

    def test_transport_body_read_requires_explicit_body_error(self):
        for phrase in ('Error reading response body from','Failed to read response body from',
                       'Timed out while receiving body from'):
            with self.subTest(phrase=phrase):
                v=self.transport('E: '+phrase+' deb.debian.org:80')
                self.assertEqual(v,{'operation':'body_read','operation_status':'explicit',
                    'endpoint':'official_debian','endpoint_status':'explicit'})

    def test_transport_generic_timeout_ip_and_source_do_not_guess(self):
        for body in ('E: Failed to fetch http://deb.debian.org/debian/pool/main/p/pkg.deb Connection timed out [IP: 192.0.2.17 80]',
                     'E: Error reading from server [IP: 192.0.2.17 80]',
                     'E: Connection timed out',
                     'Get:1 http://deb.debian.org/debian/pool/main/p/pkg.deb\nE: Unable to fetch some archives'):
            with self.subTest(body=body):
                self.assertEqual(self.transport(body),{'operation':'unknown','operation_status':'unavailable',
                    'endpoint':'unknown','endpoint_status':'unavailable'})

    def test_transport_endpoint_is_closed_and_exact(self):
        for target,endpoint in [('security.debian.org:443','official_debian'),
                ('https://deb.debian.org:443/','official_debian'),('http.docker.internal','desktop_proxy'),
                ('192.0.2.17:3128','unknown'),('[2001:db8::1]:3128','unknown'),
                ('deb.debian.org.PRIVATE:80','unknown'),('private.invalid:80','unknown'),
                ('http://user:SECRET@http.docker.internal:3128','unknown'),
                ('http.docker.internal:PRIVATE','unknown'),('http.docker.internal:','unknown'),
                ('http.docker.internal:0','unknown'),('http.docker.internal:99999','unknown'),
                ('http://http.docker.internal:3128/path?SECRET','unknown')]:
            with self.subTest(target=target):
                v=self.transport('E: Could not connect to '+target+', connection refused')
                self.assertEqual(v['operation'],'connect');self.assertEqual(v['endpoint'],endpoint)
                self.assertEqual(v['endpoint_status'],'unavailable' if endpoint=='unknown' else 'explicit')
                for secret in ('PRIVATE','SECRET','192.0.2','2001:db8','3128','http.docker.internal','deb.debian.org'):
                    self.assertNotIn(secret,repr(v))

    def test_transport_conflicts_are_independent_and_never_last_wins(self):
        v=self.transport('E: Could not connect to deb.debian.org:80\nE: Error reading response body from deb.debian.org:80')
        self.assertEqual(v,{'operation':'unknown','operation_status':'ambiguous',
            'endpoint':'official_debian','endpoint_status':'explicit'})
        for body in ('E: Could not connect to deb.debian.org:80\nE: Could not connect to http.docker.internal:3128',
                     'E: Could not connect to http.docker.internal:3128\nE: Could not connect to 192.0.2.17:3128'):
            v=self.transport(body)
            self.assertEqual(v,{'operation':'connect','operation_status':'explicit',
                'endpoint':'unknown','endpoint_status':'ambiguous'})

    def test_transport_ignores_other_steps_successes_and_command_text(self):
        prefix='#2 [1/3] RUN apt-get update\n#2 4.0 E: Could not connect to http.docker.internal:3128'
        v=self.transport('Get:1 http://deb.debian.org/debian/pool/main/p/Could-not-connect-to.deb\nE: Connection timed out',prefix=prefix)
        self.assertEqual(v['operation_status'],'unavailable');self.assertEqual(v['endpoint_status'],'unavailable')
        v=self.transport('ERROR: process "echo Could not connect to http.docker.internal:3128" failed')
        self.assertEqual(v['operation_status'],'unavailable');self.assertEqual(v['endpoint_status'],'unavailable')
        v=self.transport('Fetched package description: Error reading response body from deb.debian.org:80')
        self.assertEqual(v['operation_status'],'unavailable');self.assertEqual(v['endpoint_status'],'unavailable')

    def test_transport_operation_and_endpoint_can_be_missing_independently(self):
        v=self.transport('  Could not connect')
        self.assertEqual(v,{'operation':'connect','operation_status':'explicit',
            'endpoint':'unknown','endpoint_status':'unavailable'})
        v=self.transport('  Error reading response body')
        self.assertEqual(v,{'operation':'body_read','operation_status':'explicit',
            'endpoint':'unknown','endpoint_status':'unavailable'})

    def test_transport_unavailable_or_over_budget_cannot_keep_classification(self):
        raw=self.output('E: Could not connect to http.docker.internal:3128')
        for output in (b'',raw+self.output('E: Could not connect to deb.debian.org:80',step='8'),raw+b'x'*131073):
            v=m.public_build_failure_diagnostic(output)['apt']['transport']
            self.assertEqual(v,{'operation':'unknown','operation_status':'unavailable',
                'endpoint':'unknown','endpoint_status':'unavailable'})
        with mock.patch.object(m.json,'dumps',return_value='x'*4097):
            v=m.public_build_failure_diagnostic(raw)
        self.assertEqual(v['status'],'over_budget');self.assertEqual(v['apt']['transport']['operation'],'unknown')

    def test_missing_or_ambiguous_failure_number_does_not_guess(self):
        v=m.public_build_failure_diagnostic(b'ERROR: failed to solve: apt-get timed out exit code: 100')
        self.assertEqual(v['apt']['status'],'unavailable')
        v=m.public_build_failure_diagnostic(self.output('E: Connection timed out')+self.output('E: Unable to locate package PRIVATE',step='8'))
        self.assertEqual(v['apt']['status'],'ambiguous');self.assertEqual(v['apt']['error_kinds'],[])

    def test_apt_phases_and_fixed_error_kinds(self):
        cases=[('E: Failed to fetch http://deb.debian.org/debian/dists/trixie/InRelease Connection timed out','index_download',['timeout','download']),
               ('E: Failed to fetch https://deb.debian.org/debian/pool/main/p/private_1_arm64.deb 503 Service Unavailable [IP: 1.2.3.4 443]','deb_download',['download','http']),
               ('E: Sub-process /usr/bin/dpkg returned an error code (1)','dpkg',['dpkg']),
               ('E: Temporary failure resolving PRIVATE','unknown',['dns']),
               ('E: Could not connect to PRIVATE: Connection refused','unknown',['connection']),
               ('E: Could not handshake: certificate verification failed','unknown',['tls']),
               ('W: NO_PUBKEY PRIVATE','unknown',['signature']),
               ('E: Could not get lock /private/SECRET','unknown',['lock']),
               ('E: No space left on device','unknown',['no_space']),('E: PRIVATE-TOKEN','unknown',['other'])]
        for body,phase,kinds in cases:
            with self.subTest(phase=phase,kinds=kinds):
                apt=m.public_build_failure_diagnostic(self.output(body))['apt']
                self.assertEqual(apt['status'],'parsed');self.assertEqual(apt['phase'],phase);self.assertEqual(apt['error_kinds'],kinds)
                self.assertNotIn('PRIVATE',repr(apt));self.assertNotIn('SECRET',repr(apt));self.assertNotIn('1.2.3.4',repr(apt))

    def test_http_numbers_need_error_context_and_status_text(self):
        body='Get:1 http://deb.debian.org/debian/pool/main/p/pkg_407_502_504_arm64.deb 407502 bytes\nE: Unable to locate package pkg502\nW: Failed to fetch http://deb.debian.org/debian/dists/trixie/by-hash/SHA256/407502PRIVATE Unknown error'
        apt=m.public_build_failure_diagnostic(self.output(body))['apt']
        self.assertEqual(apt['http_statuses'],[]);self.assertNotIn('http',apt['error_kinds'])
        apt=m.public_build_failure_diagnostic(self.output('E: Failed to fetch http://deb.debian.org/debian/pool/main/p/a.deb 407 Proxy Authentication Required'))['apt']
        self.assertEqual(apt['http_statuses'],[407])

    def test_sources_are_closed_official_enums_without_urls(self):
        body='Err:1 https://deb.debian.org/debian/dists/trixie-updates/InRelease\nErr:2 http://security.debian.org/debian-security/dists/bookworm-security/InRelease\nErr:3 https://PRIVATE@deb.debian.org/debian/dists/trixie/InRelease\nErr:4 http://deb.debian.org/debian/dists/trixie/InRelease?token=SECRET\nErr:5 http://deb.debian.org:PRIVATE/debian/dists/trixie/InRelease\nErr:6 http://private.invalid/repo\nE: Failed to fetch metadata'
        apt=m.public_build_failure_diagnostic(self.output(body))['apt']
        self.assertEqual(apt['sources'],[{'scheme':'http','repository':'debian_security','suite':'bookworm-security'},{'scheme':'https','repository':'debian','suite':'trixie-updates'}])
        self.assertNotIn('PRIVATE',repr(apt));self.assertNotIn('SECRET',repr(apt));self.assertNotIn('://',repr(apt))

    def test_only_public_python_from_digest_is_retained_and_never_current_tag(self):
        prefix='#4 [1/9] FROM localhost:5001/python:3.12-slim@sha256:'+'a'*64
        v=m.public_build_failure_diagnostic(self.output('E: PRIVATE',prefix=prefix))
        self.assertEqual(v['base'],{'status':'resolved','kind':'python_3_12_slim','resolved_digest':'sha256:'+'a'*64})
        prefix+='\n#5 [stage 1/2] FROM localhost:5001/python:3.11-slim@sha256:'+'b'*64
        v=m.public_build_failure_diagnostic(self.output('E: PRIVATE',prefix=prefix))
        self.assertEqual(v['base']['status'],'ambiguous');self.assertIsNone(v['base']['resolved_digest'])
        v=m.public_build_failure_diagnostic(self.output('E: PRIVATE',prefix='#4 [1/9] FROM private.invalid/SECRET@sha256:'+'c'*64))
        self.assertEqual(v['base']['status'],'unavailable');self.assertNotIn('c'*64,repr(v))

    def test_input_and_collection_limits_do_not_truncate_to_complete(self):
        for raw in (b'x'*131073,b'x'*131073+self.output('E: Unable to locate package PRIVATE')):
            v=m.public_build_failure_diagnostic(raw);self.assertEqual(v['status'],'over_budget');self.assertEqual(v['limits_exceeded'],['input_bytes'])
            self.assertEqual(v['apt']['status'],'unavailable');self.assertEqual(v['process_exit_codes'],[])
        raw=self.output('E: PRIVATE');raw+=b'x'*(131072-len(raw))
        self.assertEqual(m.public_build_failure_diagnostic(raw)['status'],'complete')
        raw=self.output('E: PRIVATE')+b''.join(('Dockerfile:'+str(n)+'\n').encode()for n in range(1,10))
        v=m.public_build_failure_diagnostic(raw);self.assertEqual(v['status'],'over_budget');self.assertEqual(v['dockerfile_lines'],[])
        self.assertLessEqual(len(json.dumps(v).encode()),4096)

    def test_debian_from_digest_is_exact_deduplicated_and_never_inferred(self):
        base='#4 [1/2] FROM localhost:5001/debian-slim:bookworm@sha256:'+'d'*64
        v=m.public_build_failure_diagnostic(self.output('E: PRIVATE',prefix=base+'\n'+base))
        self.assertEqual(v['base'],{'status':'resolved','kind':'debian_bookworm_slim','resolved_digest':'sha256:'+'d'*64})
        for reference in ('debian-slim:bookworm','debian-slim:bookworm@sha256:'+'a'*63,
                'debian-slim:bookworm-updates@sha256:'+'a'*64,
                'debian-slim:bookworm@sha256:'+'a'*64+'PRIVATE',
                'debian-slim:bookworm@sha256:'+'a'*64+'?PRIVATE',
                'debian:bookworm-slim@sha256:'+'a'*64):
            with self.subTest(reference=reference):
                v=m.public_build_failure_diagnostic(self.output('E: PRIVATE',prefix='#4 [1/2] FROM localhost:5001/'+reference))
                self.assertEqual(v['base']['status'],'unavailable')
        for second in (base.replace('d'*64,'e'*64),'#5 [1/2] FROM localhost:5001/python:3.11-slim@sha256:'+'f'*64):
            v=m.public_build_failure_diagnostic(self.output('E: PRIVATE',prefix=base+'\n'+second))
            self.assertEqual(v['base'],{'status':'ambiguous','kind':None,'resolved_digest':None})

    def test_signature_subclasses_require_explicit_diagnostics_and_remove_details(self):
        cases=[('W: NO_PUBKEY PRIVATEKEY','missing_key'),
               ('W: GPG error: PRIVATE The following signatures were invalid: EXPKEYSIG PRIVATEKEY PRIVATEIDENTITY','expired_key'),
               ('W: BADSIG PRIVATEKEY PRIVATEIDENTITY','bad_signature'),
               ('W: GPG error: PRIVATE At least one invalid signature was encountered.','bad_signature'),
               ("E: The repository 'http://deb.debian.org/debian bookworm InRelease' is not signed.",'unsigned_repository'),
               ("E: Clearsigned file isn't valid, got 'PRIVATE-NOSPLIT'",'invalid_clearsigned'),
               ("W: The following signatures couldn't be verified: PRIVATE",'verification_failed'),
               ("W: GPG error: PRIVATE The following signatures couldn't be verified: PRIVATE",'verification_failed')]
        for body,kind in cases:
            with self.subTest(kind=kind,body=body):
                apt=m.public_build_failure_diagnostic(self.output(body))['apt']
                self.assertEqual(apt['signature_kinds'],[kind]);self.assertIn('signature',apt['error_kinds'])
                for secret in ('PRIVATE','http://','NOSPLIT'):self.assertNotIn(secret,repr(apt))
        apt=m.public_build_failure_diagnostic(self.output("W: The following signatures couldn't be verified because the public key is not available: NO_PUBKEY PRIVATE"))['apt']
        self.assertEqual(apt['signature_kinds'],['missing_key'])

    def test_owned_error_continuations_share_http_signature_and_transport_context(self):
        body=('Err:1 http://deb.debian.org/debian/dists/bookworm/InRelease\n'
              '  502 Bad Gateway [IP: 192.0.2.1 80]\n'
              '  Could not connect to http.docker.internal:3128\n'
              '  The following signatures were invalid: EXPKEYSIG PRIVATEKEY')
        apt=m.public_build_failure_diagnostic(self.output(body))['apt']
        self.assertEqual(apt['phase'],'index_download');self.assertEqual(apt['http_statuses'],[502])
        self.assertEqual(apt['signature_kinds'],['expired_key']);self.assertIn('signature',apt['error_kinds'])
        self.assertEqual(apt['transport'],{'operation':'connect','operation_status':'explicit','endpoint':'desktop_proxy','endpoint_status':'explicit'})
        for secret in ('PRIVATE','192.0.2','3128'):self.assertNotIn(secret,repr(apt))

    def test_error_continuations_stop_at_boundaries_and_never_adopt_other_output(self):
        root='Err:1 http://deb.debian.org/debian/dists/bookworm/InRelease'
        suffix='  502 Bad Gateway\n  NO_PUBKEY PRIVATEKEY'
        for boundary in ('','Get:1 http://deb.debian.org/debian/dists/bookworm/InRelease',
                '  Hit:1 PRIVATE','  Ign:1 PRIVATE','Reading package lists...',
                '  Reading package lists...','  Fetched 502 bytes','  PRIVATE progress',
                '[3/9] RUN echo PRIVATE','ERROR: process "echo PRIVATE"'):
            with self.subTest(boundary=boundary):
                apt=m.public_build_failure_diagnostic(self.output(root+'\n'+boundary+'\n'+suffix))['apt']
                self.assertEqual(apt['http_statuses'],[]);self.assertEqual(apt['signature_kinds'],[])
        for injected in ('#2 1.0 unrelated\n#7 1.0   502 Bad Gateway',
                         'unowned PRIVATE\n#7 1.0   502 Bad Gateway',
                         '#2 1.0   502 Bad Gateway'):
            raw=self.output(root).replace(b'#7 ERROR:',(injected+'\n#7 ERROR:').encode())
            apt=m.public_build_failure_diagnostic(raw)['apt'];self.assertEqual(apt['http_statuses'],[])

    def test_signature_and_http_markers_in_packages_urls_success_and_commands_do_not_count(self):
        for body in ('E: Unable to locate package NO_PUBKEY',
                'E: Failed to fetch http://deb.debian.org/debian/pool/main/p/NO_PUBKEY_PRIVATE.deb Unknown error',
                'Get:1 http://deb.debian.org/debian/dists/bookworm/InRelease\n  NO_PUBKEY PRIVATE',
                'ERROR: process "echo NO_PUBKEY PRIVATE"',
                'Fetched package description: NO_PUBKEY PRIVATE',
                'Err:1 http://deb.debian.org/debian/dists/bookworm/InRelease\n  PRIVATE_502_504 407502 bytes'):
            with self.subTest(body=body):
                apt=m.public_build_failure_diagnostic(self.output(body))['apt']
                self.assertEqual(apt['signature_kinds'],[]);self.assertEqual(apt['http_statuses'],[])
        raw=self.output('E: PRIVATE',prefix='#2 [1/2] RUN apt-get update\n#2 1.0 W: NO_PUBKEY PRIVATE')
        self.assertEqual(m.public_build_failure_diagnostic(raw)['apt']['signature_kinds'],[])

    def test_signature_collections_are_unique_and_empty_when_unavailable_or_over_budget(self):
        raw=self.output('W: NO_PUBKEY PRIVATE\nW: NO_PUBKEY PRIVATE\nW: BADSIG PRIVATE')
        apt=m.public_build_failure_diagnostic(raw)['apt'];self.assertEqual(apt['signature_kinds'],['missing_key','bad_signature'])
        for output in (b'',raw+self.output('W: EXPKEYSIG PRIVATE',step='8'),raw+b'x'*131073):
            self.assertEqual(m.public_build_failure_diagnostic(output)['apt']['signature_kinds'],[])
        with mock.patch.object(m.json,'dumps',return_value='x'*4097):
            v=m.public_build_failure_diagnostic(raw)
        self.assertEqual(v['status'],'over_budget');self.assertEqual(v['apt']['signature_kinds'],[])

    def test_proxy_facts_keep_validated_sources_but_no_values_or_secrets(self):
        daemon=m.build_proxy_facts({'HTTP_PROXY':'http://http.docker.internal:3128','HTTPS_PROXY':'http://http.docker.internal:3128'},'docker_info')
        client=m.build_proxy_facts({'HTTP_PROXY':'http://127.0.0.1:17890'},'host_environment')
        v=m.public_build_failure_diagnostic(self.output('E: PRIVATE'),{'buildkit':daemon,'run_build_args':daemon,'host_cli':client})
        self.assertEqual(v['proxy_provenance']['buildkit']['http'],'desktop_internal')
        self.assertEqual(v['proxy_provenance']['host_cli']['http'],'loopback')
        for raw in ('17890','3128','http.docker.internal','127.0.0.1','PRIVATE'):self.assertNotIn(raw,repr(v))
        with self.assertRaises(m.GateError):m.build_proxy_facts({'HTTP_PROXY':'http://user:PRIVATE@127.0.0.1:17890'},'host_environment')
        v=m.public_build_failure_diagnostic(b'',{'host_cli':{'source':'PRIVATE','http':'SECRET','https':'PRIVATE'}})
        self.assertEqual(v['proxy_provenance']['host_cli']['status'],'unavailable');self.assertNotIn('PRIVATE',repr(v))

    def test_serialized_limit_reports_over_budget_and_preserves_no_body(self):
        with mock.patch.object(m.json,'dumps',return_value='x'*4097):
            v=m.public_build_failure_diagnostic(self.output('E: PRIVATE'))
        self.assertEqual(v['status'],'over_budget');self.assertEqual(v['limits_exceeded'],['structured_bytes'])

    def test_failed_command_preserves_first_error_hash_and_proxy_provenance(self):
        raw=self.output('E: Failed to fetch http://deb.debian.org/debian/dists/trixie/InRelease Could not connect to http.docker.internal:3128, connection timed out')
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.phase='product_image_agent_backend'
            life.build_proxy_provenance={'run_build_args':m.build_proxy_facts({'HTTP_PROXY':'http://http.docker.internal:3128'},'docker_info')}
            process=mock.Mock(pid=654321,returncode=2);process.poll.return_value=2
            def start(*args,**kwargs):kwargs['stdout'].write(raw);return process
            with mock.patch.object(m.time,'monotonic',return_value=1),mock.patch.object(m.subprocess,'Popen',side_effect=start),mock.patch.object(m.Lifecycle,'docker') as docker:
                with self.assertRaisesRegex(m.GateError,'^command_failed_2$'):life.command(['make','build-images'])
            docker.assert_not_called()
            self.assertEqual(life.last_failure['exit_code'],2)
            self.assertEqual(life.last_failure['output_sha256'],__import__('hashlib').sha256(raw).hexdigest())
            self.assertEqual(life.last_failure['public_build']['apt']['phase'],'index_download')
            self.assertEqual(life.last_failure['public_build']['apt']['transport']['endpoint'],'desktop_proxy')
            self.assertEqual(life.last_failure['public_build']['proxy_provenance']['run_build_args']['http'],'desktop_internal')


def row(cpu=1, memory=512 * 1024**2, *, status='running', exit_code=0, oom=False, service='system-backend'):
    return {'Id': 'owned-id', 'Config': {'Labels': {'com.docker.compose.service': service}},
            'HostConfig': {'NanoCpus': int(cpu * 1e9), 'Memory': memory},
            'State': {'Running': status == 'running', 'Status': status, 'ExitCode': exit_code, 'OOMKilled': oom}}


def bare_lifecycle(directory):
    life = m.Lifecycle.__new__(m.Lifecycle)
    life.run_id = 'addp-trace-local-synthetic'
    life.uuid = '11111111-1111-4111-8111-111111111111'
    life.work = Path(directory)
    life.checkout = life.work / 'source'
    life.started = 0.0
    life.phase = 'synthetic'
    life.builder = life.run_id
    life.builder_ids = set()
    life.builder_volumes = set()
    life.builder_created = False
    life.builder_stopped = False
    life.tags = []
    life.env = {'PATH': '/synthetic'}
    life.stop = threading.Event()
    life.failure = None
    life.samples = []
    life.thread = None
    life.sample_lock = threading.Lock()
    life.sampler_volumes = ()
    life.sampler_volume_roles = ()
    life.sampler_started = False
    life.sample_thread_id = None
    life.sample_deadline = None
    life.command_evidence = m.CommandEvidence()
    life.minio_image = 'sha256:synthetic-pinned-minio'
    life.owned_bind_bytes = lambda **kwargs: 0
    return life


class GoCacheReclaimTests(unittest.TestCase):
    def fixture(self,directory):
        life=bare_lifecycle(Path(directory).resolve());life.started=m.time.monotonic()
        target=life.checkout/'.cache/go-build';target.mkdir(parents=True);(target/'object').write_text('owned-cache')
        life.sample=mock.Mock()
        child=mock.Mock(pid=12345);child.poll.return_value=0
        def delete(args,**kwargs):
            self.assertTrue(life.cache_reclaim_lock_held)
            self.assertFalse(life.sample_lock.acquire(blocking=False))
            self.assertEqual(args[-1],str(target));self.assertLessEqual(kwargs['timeout'],4)
            kwargs['process_observer'](child)
            m.shutil.rmtree(target)
            return ''
        life.command=mock.Mock(side_effect=delete)
        return life,target,child

    def test_full_precheck_precedes_exclusive_delete_and_receipt_is_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,child=self.fixture(directory);events=[]
            life.sample.side_effect=lambda **kwargs:(self.assertTrue(target.exists()),events.append('precheck'))
            delete=life.command.side_effect
            life.command.side_effect=lambda *a,**kw:(events.append('delete'),delete(*a,**kw))[-1]
            m.reclaim_go_build_cache(life)
            self.assertEqual(events,['precheck','delete']);self.assertFalse(target.exists())
            self.assertEqual(life.cache_reclaim_receipt['result'],'passed')
            self.assertTrue(life.cache_reclaim_receipt['deletion']['child_stopped'])
            self.assertFalse(life.cache_reclaim_lock_held)
            self.assertNotIn(str(life.work),json.dumps(life.cache_reclaim_receipt))
            self.assertIsInstance(life.sample.call_args.kwargs['deadline'],float)

    def test_native_deletion_child_is_reaped_before_unlock_without_docker(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,_=self.fixture(directory)
            life.command=m.Lifecycle.command.__get__(life)
            m.reclaim_go_build_cache(life)
            self.assertFalse(target.exists());self.assertFalse(life.cache_reclaim_lock_held)
            self.assertIsNotNone(life.cache_reclaim_process.poll())
            self.assertTrue(life.cache_reclaim_receipt['deletion']['child_stopped'])
            self.assertLess(life.cache_reclaim_receipt['deletion']['elapsed_seconds'],4)

    def test_failed_or_expired_precheck_never_deletes_to_hide_stop_line(self):
        for code in ('owned_volume_stop_line_reached','command_failed_1'):
            with self.subTest(code=code),tempfile.TemporaryDirectory() as directory:
                life,target,_=self.fixture(directory);life.sample.side_effect=m.GateError(code)
                with self.assertRaisesRegex(m.GateError,code):m.reclaim_go_build_cache(life)
                self.assertTrue(target.exists());life.command.assert_not_called()
                self.assertEqual(life.cache_reclaim_receipt['deletion']['result'],'not_started')
        with tempfile.TemporaryDirectory() as directory:
            life,target,_=self.fixture(directory);clock=[1.0];life.started=1
            life.sample.side_effect=lambda **kw:clock.__setitem__(0,5.01)
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]),self.assertRaisesRegex(m.GateError,'resource_sample_deadline'):
                m.reclaim_go_build_cache(life)
            life.command.assert_not_called();self.assertTrue(target.exists())

    def test_active_sample_prevents_deletion(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,_=self.fixture(directory);ready=threading.Event();errors=[]
            def precheck(**kwargs):
                ready.set()
                with life.sample_lock:self.assertTrue(target.exists())
            life.sample.side_effect=precheck
            def run():
                try:m.reclaim_go_build_cache(life)
                except BaseException as error:errors.append(error)
            life.sample_lock.acquire();worker=threading.Thread(target=run);worker.start()
            try:
                ready.wait(timeout=1)
                self.assertTrue(target.exists(),'cache removed while active sample owns lock')
                life.command.assert_not_called()
            finally:
                life.sample_lock.release();worker.join(timeout=1)
            self.assertFalse(worker.is_alive());self.assertEqual(errors,[]);self.assertFalse(target.exists())

    def test_sampler_cannot_read_during_delete_but_resumes_after_release(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,child=self.fixture(directory);life.sample=m.Lifecycle.sample.__get__(life)
            scanned=threading.Event();entered=threading.Event();errors=[];readers=[]
            life._sample=lambda:scanned.set() if life.phase=='go_cache_reclaim' else None
            def read():
                entered.set()
                try:life.sample(deadline=m.time.monotonic()+4)
                except BaseException as error:errors.append(error)
            def delete(args,**kwargs):
                kwargs['process_observer'](child)
                worker=threading.Thread(target=read);readers.append(worker);worker.start()
                self.assertTrue(entered.wait(timeout=1));self.assertFalse(scanned.is_set())
                m.shutil.rmtree(target)
            life.command.side_effect=delete
            m.reclaim_go_build_cache(life)
            for worker in readers:worker.join(timeout=1);self.assertFalse(worker.is_alive())
            self.assertTrue(scanned.is_set());self.assertEqual(errors,[])

    def test_delete_lock_wait_consumes_the_same_four_second_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,child=self.fixture(directory);clock=[1.0];life.started=1
            life.sample_lock=mock.Mock()
            def acquire(**kw):clock[0]+=3;return True
            life.sample_lock.acquire.side_effect=acquire
            def delete(args,**kwargs):
                self.assertAlmostEqual(kwargs['timeout'],1);kwargs['process_observer'](child)
                m.shutil.rmtree(target);clock[0]+=.5
            life.command.side_effect=delete
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]):m.reclaim_go_build_cache(life)
            self.assertEqual(life.cache_reclaim_receipt['deletion']['lock_wait_seconds'],3)
            life.sample_lock.release.assert_called_once()

    def test_delete_timeout_is_failure_and_reaped_child_precedes_unlock(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,child=self.fixture(directory);clock=[1.0];life.started=1
            def delete(args,**kwargs):
                kwargs['process_observer'](child);clock[0]=5.1
                raise m.GateError('command_deadline_exceeded')
            life.command.side_effect=delete
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]),self.assertRaises(m.GateError):m.reclaim_go_build_cache(life)
            self.assertEqual(life.command.call_count,1);self.assertTrue(target.exists())
            self.assertFalse(life.cache_reclaim_lock_held)
            self.assertEqual(life.cache_reclaim_receipt['result'],'failed')
            self.assertGreater(life.cache_reclaim_receipt['deletion']['elapsed_seconds'],4)

    def test_return_code_zero_at_deadline_is_still_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,child=self.fixture(directory);clock=[1.0];life.started=1
            def delete(args,**kwargs):
                kwargs['process_observer'](child);m.shutil.rmtree(target);clock[0]=5.0
                return ''
            life.command.side_effect=delete
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]),self.assertRaisesRegex(m.GateError,'go_cache_reclaim_deadline'):
                m.reclaim_go_build_cache(life)
            self.assertEqual(life.cache_reclaim_receipt['result'],'failed');self.assertFalse(life.cache_reclaim_lock_held)

    def test_live_child_retains_lock_and_cleanup_refuses_resource_removal(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,child=self.fixture(directory);child.poll.return_value=None
            def delete(args,**kwargs):
                kwargs['process_observer'](child)
                raise m.subprocess.TimeoutExpired('PRIVATE',4)
            life.command.side_effect=delete
            with self.assertRaises(m.subprocess.TimeoutExpired):m.reclaim_go_build_cache(life)
            self.assertTrue(life.cache_reclaim_lock_held);self.assertFalse(life.sample_lock.acquire(blocking=False))
            life.docker=mock.Mock(return_value='');child.wait.side_effect=m.subprocess.TimeoutExpired('PRIVATE',1)
            with mock.patch.object(m.os,'killpg'):
                self.assertEqual(life.cleanup(),['cache_reclaim_child_not_stopped'])
            life.docker.assert_not_called();self.assertTrue(target.exists());self.assertTrue(life.cache_reclaim_lock_held)
            child.poll.return_value=0
            self.assertEqual(life.cleanup(),[]);self.assertFalse(life.cache_reclaim_lock_held)

    def test_precheck_deadline_includes_lock_wait_even_for_trace_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=LocalTraceResourceGuardTests().sample_lifecycle(directory,1024);clock=[0.0]
            life.sample_lock=mock.Mock()
            def acquire(**kw):clock[0]=3.6;return True
            life.sample_lock.acquire.side_effect=acquire
            underlying=life.docker
            def delayed(*args,**kwargs):clock[0]+=.3;return underlying(*args,**kwargs)
            life.docker=delayed
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]),self.assertRaisesRegex(m.GateError,'resource_sample_deadline'):
                life.sample(deadline=4)
            self.assertEqual(life.sample_lock_wait,3.6);self.assertEqual(life.sample_began,0)
            self.assertEqual(life.samples,[]);life.sample_lock.release.assert_called_once()

    def test_precheck_never_retries_private_file_disappearance(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=LocalTraceResourceGuardTests().sample_lifecycle(directory,1024)
            life.last_failure={'du_missing_file':True};life.owned_bind_bytes=mock.Mock(side_effect=m.GateError('command_failed_1'))
            with mock.patch.object(m.time,'monotonic',return_value=1),self.assertRaises(m.GateError):life.sample(deadline=5)
            life.owned_bind_bytes.assert_called_once();self.assertEqual(life.samples,[])

    def test_unknown_child_status_fails_closed_without_unlock_or_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            life,target,child=self.fixture(directory);child.poll.side_effect=OSError('PRIVATE')
            def delete(args,**kwargs):
                kwargs['process_observer'](child)
                raise m.GateError('command_deadline_exceeded')
            life.command.side_effect=delete
            with self.assertRaises(m.GateError):m.reclaim_go_build_cache(life)
            life.docker=mock.Mock()
            self.assertEqual(life.cleanup(),['cache_reclaim_child_status_unknown'])
            life.docker.assert_not_called();self.assertTrue(target.exists());self.assertTrue(life.cache_reclaim_lock_held)
            child.poll.side_effect=None;child.poll.return_value=0;life.docker=mock.Mock(return_value='')
            self.assertEqual(life.cleanup(),[])

    def test_target_cannot_escape_or_follow_a_symlink(self):
        with tempfile.TemporaryDirectory() as directory,tempfile.TemporaryDirectory() as outside:
            life,target,_=self.fixture(directory);m.shutil.rmtree(target);target.symlink_to(Path(outside),target_is_directory=True)
            protected=Path(outside)/'untouched';protected.write_text('safe')
            with self.assertRaisesRegex(m.GateError,'target_invalid'):m.reclaim_go_build_cache(life)
            self.assertEqual(protected.read_text(),'safe');life.sample.assert_not_called();life.command.assert_not_called()


class VolumePreparationContractTests(unittest.TestCase):
    def test_protection_readiness_failure_exports_only_fixed_markers(self):
        markers=m.diagnostic_markers(b'execution trace Develop protection readiness: Tenant PRIVATE-TOKEN protection initialization incomplete')
        self.assertEqual(markers,['protection readiness','protection initialization'])

    def test_dependency_failure_names_the_exact_stage_and_only_fixed_public_markers(self):
        stages=[]
        def compose(*args,**kwargs):
            if args[0]=='exec':raise m.GateError('command_failed_2')
        with self.assertRaisesRegex(m.GateError,'command_failed_2'):
            m.start_dependencies(compose,'private-sql',stages.append)
        self.assertEqual(stages,['isolated_databases_start','isolated_platform_database_initialization'])
        markers=m.diagnostic_markers(b'psql: connection to server: PRIVATE-PASSWORD: server closed the connection unexpectedly')
        self.assertEqual(markers,['psql:','connection to server','server closed the connection unexpectedly'])
        self.assertNotIn('PRIVATE',str(markers))

    def test_first_sample_failure_reports_the_exact_operation_without_waiting_for_monitor(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.sample_operation='measure_private_bindings'
            life._sample=mock.Mock(side_effect=m.GateError('command_deadline_exceeded'))
            with self.assertRaisesRegex(m.GateError,'command_deadline_exceeded'):life.sample()
            self.assertEqual(life.resource_failure['operation'],'measure_private_bindings')
            self.assertEqual(life.resource_failure['error_code'],'command_deadline_exceeded')
            self.assertEqual(life.samples,[])

    def test_complete_owned_compose_volumes_are_created_before_sampling_and_startup(self):
        for wrong_owner in (False,True):
            with self.subTest(wrong_owner=wrong_owner),tempfile.TemporaryDirectory() as directory:
                life=bare_lifecycle(directory);life.deployment_config={'volumes':{'runtime-logs':{},'postgres-data':{}}};calls=[]
                def docker(*args,**kwargs):
                    calls.append(args);name=args[-1]
                    if args[1]=='create':return name+'\n'
                    logical=name.removeprefix(life.run_id+'_')
                    return json.dumps([{'Name':name,'Labels':{m.LABEL:'foreign' if wrong_owner else life.run_id,
                        'com.docker.compose.project':life.run_id,'com.docker.compose.volume':logical}}])
                life.docker=docker
                if wrong_owner:
                    with self.assertRaisesRegex(m.GateError,'owned_volume_preparation_ownership_mismatch'):m.prepare_sampling_volumes(life)
                else:
                    m.prepare_sampling_volumes(life)
                    self.assertEqual([args[-1] for args in calls if args[1]=='create'],
                        [life.run_id+'_postgres-data',life.run_id+'_runtime-logs'])
        source=__import__('inspect').getsource(m.build)
        order=['prepare_isolated_configuration(', 'prepare_sampling_volumes(', 'prepare_builder(', 'life.sample()', 'life.thread.start()', 'start_dependencies(']
        positions=[source.index(token) for token in order]
        self.assertEqual(positions,sorted(positions))
        builder=__import__('inspect').getsource(m.prepare_builder)
        self.assertLess(builder.index('life.builder_ids ='),builder.index('life.prepare_volume_sampler('))

    def test_sampler_admission_is_bounded_readonly_and_cannot_replace_a_running_helper(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.docker=mock.Mock(return_value='owned')
            life.prepare_volume_sampler(['z','a','z'],timeout=30)
            args=life.docker.call_args.args
            self.assertEqual(args[args.index('--pull')+1],'never')
            self.assertEqual(life.sampler_volumes,('a','z'))
            self.assertEqual(life.docker.call_args.kwargs,{'guarded':False,'timeout':30})
            self.assertIn(str(m.SAMPLER_CPU),args);self.assertIn('64m',args);self.assertIn('none',args)
            self.assertEqual([a for a in args if a.startswith('type=volume,')],
                ['type=volume,source=a,target=/space/0,readonly','type=volume,source=z,target=/space/1,readonly'])
            with self.assertRaisesRegex(m.GateError,'volume_sampler_already_started'):
                life.prepare_volume_sampler(['foreign'],timeout=30)
            self.assertEqual(life.docker.call_count,1)


class BuilderResumeContractTests(unittest.TestCase):
    def test_planned_stop_is_cleared_only_after_exact_healthy_builder_resumes(self):
        for running,oom in ((True,False),(False,False),(True,True)):
            with self.subTest(running=running,oom=oom),tempfile.TemporaryDirectory() as directory:
                life=bare_lifecycle(directory);life.builder_stopped=True;life.builder_ids={'owned-id'};life.sample=mock.Mock()
                life.docker=mock.Mock(side_effect=['bootstrapped',json.dumps([{'Id':'owned-id','State':{'Running':running,'OOMKilled':oom}}])])
                if running and not oom:
                    life.resume_builder();self.assertFalse(life.builder_stopped);life.sample.assert_called_once()
                else:
                    with self.assertRaisesRegex(m.GateError,'owned_builder_resume_not_healthy'):life.resume_builder()
                    self.assertTrue(life.builder_stopped);life.sample.assert_not_called()

    def test_two_cpu_compilation_with_stopped_builder_fits_aggregate_budget(self):
        dependencies=('postgres','business-postgres','redis','minio','loki','runtime-log-api','tempo','trace-query','alloy')
        rows=[row(cpu=m.LIMITS[name][0],memory=m.LIMITS[name][1]*1024**2,service=name) for name in dependencies]
        rows += [row(cpu=2,memory=3*1024**3,service=None),row(cpu=.25,memory=64*1024**2,service=None)]
        cpu,memory=m.validate_resources(rows)
        self.assertLessEqual(cpu,8);self.assertLessEqual(memory,12*1024**3)


class NginxRouteContractTests(unittest.TestCase):
    def probe(self,directory,strip_query=False):
        life=bare_lifecycle(directory);calls=[];life.sample=mock.Mock()
        def docker(*args,**kwargs):
            calls.append(args)
            if args[0] in ('network','run','kill'):return 'owned'
            if args[0]=='wait':return '0\n0\n'
            if args[0]=='exec':
                if args[2]=='sh':return '/route-ready' if 'n=0;' in args[-1] else 'HTTP/1.1 502 Bad Gateway\n'
                url=urllib.parse.urlsplit(args[-1]);path=url.path
                if path.startswith('/module-ui/'):path='/'+path.split('/',3)[3]
                return path+('?' + url.query if url.query and not strip_query else '')
            raise AssertionError(args)
        life.docker=docker
        return life,calls

    def test_real_probe_is_bounded_isolated_and_rejects_missing_modules(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.probe(directory);m.verify_nginx_routes(life,'sha256:product-nginx')
            self.assertEqual(life.nginx_route_assertions,{'uri_and_query_cases':11,'missing_module_502':True,'selected_module_remains_readable':True})
            self.assertEqual(life.sample.call_count,2)
            self.assertIn('--internal',calls[0])
            for args in (call for call in calls if call[0]=='run'):
                self.assertEqual(args[args.index('--cpus')+1],'.1');self.assertEqual(args[args.index('--memory')+1],'64m')
                self.assertNotIn('--publish',args);self.assertIn(m.LABEL+'.role=oneshot',args)
            aliases=calls[1];self.assertIn('gateway',aliases);self.assertNotIn('service-frontend',aliases)
            self.assertIn('listen 8000',(life.work/'route-echo.conf').read_text())

    def test_query_loss_cannot_be_counted_as_route_success(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.probe(directory,strip_query=True)
            with self.assertRaisesRegex(m.GateError,'nginx_proxy_uri_or_query_changed'):
                m.verify_nginx_routes(life,'sha256:product-nginx')
            self.assertFalse(hasattr(life,'nginx_route_assertions'))

    def test_local_scope_and_existing_ci_entry_cover_product_routes(self):
        self.assertEqual(m.FRONTENDS,('console','system-frontend','monitor-frontend','nginx'))
        self.assertEqual(len(m.BACKENDS),6)
        configuration=(m.ROOT/'nginx/nginx.conf').read_text()
        for name in ('system','manager','meta','transfer','orchestrator','develop','service'):
            self.assertNotRegex(configuration,r'(?m)^\s*upstream '+name+r'-frontend\s*\{')
            self.assertIn('rewrite ^/module-ui/'+name+'/(.*)$ /$1 break;',configuration)
            self.assertIn('proxy_pass http://$'+name+'_frontend_upstream;',configuration)
        make=(m.ROOT/'Makefile').read_text()
        self.assertIn('@$(MAKE) test-nginx-routes',make)
        self.assertIn('nginx/',(m.ROOT/'scripts/test/monitor-traces-gate.sh').read_text().splitlines()[3])


class MinioProductContractTests(unittest.TestCase):
    recipe = (m.ROOT/'scripts/infra/Dockerfile.minio').read_text()
    image_id = 'sha256:'+'a'*64

    def fixture(self,directory,*,metadata=None,versions=None):
        life=bare_lifecycle(directory)
        path=life.checkout/'scripts/infra/Dockerfile.minio'
        path.parent.mkdir(parents=True);path.write_text(self.recipe)
        expected=m.minio_product_contract(self.recipe)
        calls=[]
        def docker(*args,**kwargs):
            calls.append((args,kwargs))
            if args[:2]==('image','inspect'):
                self.assertEqual(args[2],'addp-minio:'+expected['minio']['release'])
                return json.dumps(metadata or {'id':self.image_id,'architecture':'arm64','os':'linux'})
            if args[0]=='run':
                name=args[args.index('--entrypoint')+1].rsplit('/',1)[-1]
                self.assertEqual(args[-2:],(self.image_id,'--version'))
                value=expected[name]
                return (versions or {}).get(name,name+' version '+value['release']+' (commit-id='+value['source_commit']+')\nPRIVATE-SENTINEL\n')
            raise AssertionError(args)
        life.docker=docker
        return life,calls

    def test_authoritative_recipe_pins_both_release_source_commits(self):
        expected=m.minio_product_contract(self.recipe)
        self.assertEqual(expected['minio']['source_commit'],'9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a')
        self.assertEqual(expected['mc']['source_commit'],'7394ce0dd2a80935aded936b09fa12cbb3cb8096')

    def test_ambiguous_or_unpinned_recipe_is_rejected(self):
        for value in (self.recipe+self.recipe,self.recipe.replace('github.com/minio/minio.git','github.com/other/minio.git'),
                      self.recipe.replace('9e49d5e7a648f00e26f2246f4dc28e6b07f8c84a','unpinned')):
            with self.subTest(value=value[:20]),self.assertRaises(m.GateError):m.minio_product_contract(value)

    def test_probes_bind_the_same_id_are_readonly_offline_and_keep_only_metadata(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.fixture(directory)
            m.verify_minio_product(life)
            self.assertEqual(life.minio_image,self.image_id)
            self.assertEqual(set(life.minio_provenance['components']),{'minio','mc'})
            self.assertNotIn('PRIVATE-SENTINEL',json.dumps(life.minio_provenance))
            runs=[args for args,_ in calls if args[0]=='run']
            self.assertEqual(len(runs),2)
            for args in runs:
                self.assertEqual(args[args.index('--pull')+1],'never')
                self.assertEqual(args[args.index('--network')+1],'none')
                self.assertIn('--read-only',args)
                self.assertIn('/data:rw,noexec,nosuid,size=1m',args)
                self.assertEqual(args[args.index('--cpus')+1],'.25')
                self.assertEqual(args[args.index('--memory')+1],'256m')

    def test_wrong_platform_or_mutable_identity_never_runs_a_probe(self):
        for metadata in ({'id':self.image_id,'architecture':'amd64','os':'linux'},
                         {'id':'mutable:tag','architecture':'arm64','os':'linux'},
                         {'id':self.image_id,'architecture':'arm64','os':'darwin'}):
            with tempfile.TemporaryDirectory() as directory:
                life,calls=self.fixture(directory,metadata=metadata)
                with self.assertRaisesRegex(m.GateError,'minio_product_image_identity_invalid'):m.verify_minio_product(life)
                self.assertEqual(len(calls),1)

    def test_wrong_release_or_embedded_source_is_rejected_without_fallback(self):
        for name,expected in m.minio_product_contract(self.recipe).items():
            versions=(name+' version RELEASE.2000-01-01T00-00-00Z (commit-id='+expected['source_commit']+')\n',
                      name+' version '+expected['release']+' (commit-id='+'b'*40+')\n')
            for value in versions:
                with tempfile.TemporaryDirectory() as directory:
                    life,calls=self.fixture(directory,versions={name:value})
                    with self.assertRaisesRegex(m.GateError,'minio_product_'+name+'_version_mismatch'):m.verify_minio_product(life)
                    self.assertEqual([args[0] for args,_ in calls],['image']+['run']*(1 if name=='minio' else 2))
                    self.assertFalse(hasattr(life,'minio_provenance'))

    def test_missing_product_image_refuses_without_pulling_or_building(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.fixture(directory)
            life.docker=mock.Mock(side_effect=m.GateError('command_failed_1'))
            with self.assertRaises(m.GateError):m.verify_minio_product(life)
            life.docker.assert_called_once()
            self.assertEqual(life.docker.call_args.args[:2],('image','inspect'))


class LocalTraceAdmissionTests(unittest.TestCase):
    def test_build_proxy_uses_only_credential_free_local_daemon_transport(self):
        self.assertEqual(m.daemon_build_proxies({}), {})
        proxy = 'http://http.docker.internal:3128'
        self.assertEqual(m.daemon_build_proxies({'HTTPProxy': proxy, 'HTTPSProxy': proxy}),
                         {'HTTP_PROXY': proxy, 'HTTPS_PROXY': proxy})
        self.assertEqual(m.daemon_build_proxies({'HTTPSProxy': 'http.docker.internal:3128'}),
                         {'HTTPS_PROXY': proxy})
        self.assertEqual(m.daemon_build_proxies({'HTTPSProxy': 'http://127.0.0.1:17890'},client=True),
                         {'HTTPS_PROXY':'http://127.0.0.1:17890'})
        with self.assertRaises(m.GateError):
            m.daemon_build_proxies({'HTTPSProxy':proxy},client=True)
        for value in ('http://user:secret@http.docker.internal:3128', 'http://foreign.invalid:3128',
                      'http://http.docker.internal:3128/?secret=value', 'file:///private/proxy'):
            with self.subTest(value=value), self.assertRaises(m.GateError):
                m.daemon_build_proxies({'HTTPSProxy': value})

    def test_only_native_local_profile_is_admitted(self):
        m.validate_profile({}, 'arm64', 'aarch64')
        for key in ('GITHUB_ACTIONS', 'ADDP_ONLINE_HOSTED', 'ADDP_ONLINE_OWNER_MANAGED', 'ADDP_ONLINE_HOST', 'ADDP_ONLINE_LOCAL_SGM'):
            for value in ('true', 'false', '1'):
                with self.subTest(key=key, value=value), self.assertRaises(m.GateError):
                    m.validate_profile({key: value}, 'arm64', 'arm64')
        for machine, daemon in [('x86_64', 'arm64'), ('arm64', 'amd64'), ('amd64', 'amd64')]:
            with self.subTest(machine=machine, daemon=daemon), self.assertRaises(m.GateError):
                m.validate_profile({}, machine, daemon)

    def test_effective_docker_context_cannot_hide_a_remote_daemon(self):
        reader = mock.Mock(return_value='ssh://remote-daemon')
        with self.assertRaises(m.GateError):
            m.effective_docker_endpoint({'DOCKER_HOST': 'unix:///local.sock', 'DOCKER_CONTEXT': 'remote'}, reader)
        reader.assert_called_once_with('remote')
        reader.reset_mock()
        self.assertEqual(m.effective_docker_endpoint({'DOCKER_HOST': 'unix:///private/docker.sock'}, reader), 'unix:///private/docker.sock')
        reader.assert_not_called()
        reader.return_value = 'unix:///desktop/docker.sock'
        self.assertEqual(m.effective_docker_endpoint({}, reader), 'unix:///desktop/docker.sock')
        reader.assert_called_once_with(None)
        for endpoint in ('tcp://127.0.0.1:2375', 'ssh://host', 'unix://relative', '', None):
            with self.subTest(endpoint=endpoint), self.assertRaises(m.GateError):
                m.effective_docker_endpoint({'DOCKER_HOST': endpoint}, lambda name: endpoint)

    def test_user_secrets_and_infra_inputs_are_not_inherited(self):
        environment = {'PATH': '/tools', 'HOME': '/private-home', 'TMPDIR': '/tmp', 'DOCKER_CONTEXT': 'desktop-linux',
                       'POSTGRES_PASSWORD': 'private-password', 'POSTGRES_HOST': 'shared-postgres',
                       'ADDP_ONLINE_TEST_USER_ACCESS_TOKEN': 'private-token', 'HTTP_PROXY': 'private-proxy'}
        with mock.patch.dict(os.environ, environment, clear=True):
            selected = m.safe_environment()
        self.assertEqual(set(selected), {'PATH', 'HOME', 'TMPDIR', 'DOCKER_CONTEXT'})
        self.assertNotIn('private-password', repr(selected))
        self.assertNotIn('private-token', repr(selected))

    def test_zero_or_unbounded_resources_oom_and_nonzero_exit_fail(self):
        cpu, memory = m.validate_resources([row(3, 5 * 1024**3), row(3, 5 * 1024**3)])
        self.assertEqual(cpu, 6)
        self.assertEqual(memory, 10 * 1024**3)
        cases = [row(cpu=0), row(memory=0), row(cpu=8.1), row(memory=12 * 1024**3 + 1), row(oom=True),
                 row(status='exited', exit_code=2), row(status='dead', exit_code=1)]
        for bad in cases:
            with self.subTest(bad=bad), self.assertRaises(m.GateError):
                m.validate_resources([bad])
        legacy = row(cpu=0)
        legacy['HostConfig'].update(CpuQuota=25000, CpuPeriod=100000)
        self.assertEqual(m.validate_resources([legacy])[0], .25)

    def test_aggregate_hard_limits_cannot_hide_behind_individual_limits(self):
        with self.assertRaises(m.GateError):
            m.validate_resources([row(4.1), row(4.1)])
        with self.assertRaises(m.GateError):
            m.validate_resources([row(memory=7 * 1024**3), row(memory=6 * 1024**3)])

    def test_successful_init_is_not_an_application_failure(self):
        m.validate_resources([row(status='exited', service='trace-store-init'),
                              row(status='exited', service='runtime-log-store-init')])

    def test_only_explicit_owned_oneoffs_and_stopped_builder_may_exit_zero(self):
        builder = row(status='exited', service='builder')
        m.validate_resources([builder], allowed_exits=('owned-id',))
        with self.assertRaises(m.GateError):
            m.validate_resources([builder], allowed_exits=('foreign-id',))
        helper = row(status='exited', service='identity')
        helper['Config']['Labels'][m.LABEL + '.role'] = 'oneshot'
        m.validate_resources([helper])
        helper['State']['ExitCode'] = 1
        with self.assertRaises(m.GateError):
            m.validate_resources([helper])

    def test_owned_oneoff_cannot_remove_exit_state_before_guard_inspection(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.command=mock.Mock(return_value='completed')
            with self.assertRaisesRegex(m.GateError,'oneshot_exit_state_must_be_retained'):
                life.docker('run','--rm','--label',m.LABEL+'.role=oneshot','owned-image')
            life.command.assert_not_called()
            self.assertEqual(life.docker('run','--label',m.LABEL+'.role=oneshot','owned-image'),'completed')

    def test_builder_stop_captures_exact_fixed_state_before_validating(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.builder_ids={'owned-id'}
            life.docker=mock.Mock(side_effect=[json.dumps([row()]),'',json.dumps([row(status='exited',exit_code=137)])])
            with self.assertRaisesRegex(m.GateError,'owned_container_failed'):life.stop_builder()
            self.assertTrue(life.builder_stopped)
            self.assertEqual(life.builder_stop_states,[{'id':'owned-id','status':'exited','exit_code':137,'oom':False}])

    def test_only_exact_planned_builder_may_use_verified_stop_exit_one(self):
        m.validate_resources([row(status='exited',exit_code=1)],allowed_exits=('owned-id',))
        for bad,allowed in [(row(status='exited',exit_code=1),('foreign-id',)),
                            (row(status='exited',exit_code=1,oom=True),('owned-id',)),
                            (row(status='dead',exit_code=1),('owned-id',)),
                            (row(status='exited',exit_code=137),('owned-id',))]:
            with self.subTest(bad=bad),self.assertRaises(m.GateError):m.validate_resources([bad],allowed)

    def test_long_running_product_exit_zero_is_still_a_stop_condition(self):
        for name in ('system-backend', 'gateway', 'tempo', 'alloy'):
            with self.subTest(service=name), self.assertRaises(m.GateError):
                m.validate_resources([row(status='exited', service=name)])


class LocalTraceIsolationTests(unittest.TestCase):
    def test_telemetry_configuration_probes_use_product_mounts_identity_and_no_network(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.docker=mock.Mock(return_value='verified')
            life.deployment_documents={name:{'image':'pinned-'+name,'user':'501:20','cap_drop':['ALL'],'command':['-config.file=/etc/loki/runtime.yml','-config.expand-env=true'],
                'environment':{'TEST_KEY':'private-placeholder'},'volumes':[{'type':'bind','source':directory,'target':'/configuration','read_only':True}]} for name in ('loki','trace-query')}
            m.verify_telemetry_configuration(life)
            self.assertEqual(life.docker.call_count,2)
            for call in life.docker.call_args_list:
                args=call.args
                self.assertNotIn('--rm',args);self.assertIn('none',args);self.assertIn('--read-only',args)
                self.assertIn('501:20',args);self.assertIn('ALL',args);self.assertIn(m.LABEL+'.role=oneshot',args)
            self.assertIn('-verify-config=true',life.docker.call_args_list[0].args)
            self.assertEqual(life.docker.call_args_list[1].args[-1],'-t')

    def test_product_trace_template_workspace_is_declared_without_selecting_capacity(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.command=mock.Mock(return_value='PATH=/tools\0')
            env=m.defaults(life)
            self.assertEqual(env['TRACE_T2_WORK'],str(life.work))
            self.assertNotIn('trace-load',m.TRACE)
            self.assertNotIn('trace-load',m.LIMITS)

    def test_deployment_configuration_failure_prevents_builder_and_compilation(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.command=mock.Mock(return_value='frozen-commit')
            life.docker=mock.Mock()
            with mock.patch.object(m,'committed_inputs',return_value={}),mock.patch.object(m,'defaults',return_value={}),\
                 mock.patch.object(m,'verify_minio_product'),mock.patch.object(m,'prepare_deployment_documents',side_effect=m.GateError('configuration_rejected')):
                with self.assertRaisesRegex(m.GateError,'configuration_rejected'):m.build(life)
            life.docker.assert_not_called()

    def test_setup_deployment_input_is_accepted_by_actual_product_renderer(self):
        renderer_spec=importlib.util.spec_from_file_location('trace_platform_renderer',m.ROOT/'scripts/prod/generate-trace-platform.py')
        renderer=importlib.util.module_from_spec(renderer_spec);renderer_spec.loader.exec_module(renderer)
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);env={'ADDP_TRACE_DEPLOYMENT_DIR':str(life.work/'deployment')}
            def command(args,**kwargs):
                if args[1]=='scripts/test/trace-deployment-fixture.py':
                    (life.work/'deployment').mkdir(mode=0o700)
                    for name in ('source-tls','query-tls'):
                        tls=life.work/name;tls.mkdir(mode=0o700)
                        for filename in ('ca.crt','client.crt','client.key'):
                            p=tls/filename;p.write_text('public-contract-test');p.chmod(0o600)
                elif args[1]=='scripts/prod/generate-trace-platform.py':
                    with mock.patch.dict(os.environ,kwargs['env'],clear=True):renderer.render()
                    config=json.loads((life.work/'deployment/platform-traces.json').read_text())
                    self.assertEqual(set(config['services']),set(m.BACKENDS))
                    self.assertEqual(len(config['services']['monitor-backend']['volumes']),2)
                elif args[:2]==['docker','compose']:
                    services={name:{'environment':{},'volumes':[]} for name in m.BACKENDS+m.FRONTENDS+m.TRACE+('postgres','redis')}
                    services['postgres'].update(command=[],volumes=[{'type':'volume','source':'postgres-data','target':'/data'}])
                    services['trace-query']['user']='0:0'
                    return json.dumps({'services':services})
                else:raise AssertionError(args)
            life.command=command
            documents=m.prepare_deployment_documents(life,env)
            self.assertEqual(documents['trace-query']['user'],str(os.getuid())+':'+str(os.getgid()))

    def document(self):
        return {'system-backend': {
            'image': 'old-product-tag', 'build': '.', 'restart': 'always', 'ports': ['8180:8180'],
            'depends_on': ['postgres'], 'profiles': ['production'], 'networks': ['personal-network'],
            'volumes': [{'type': 'volume', 'source': 'personal-runtime-logs', 'target': '/var/log/addp'},
                        {'type': 'volume', 'source': 'postgres-data', 'target': '/data'},
                        {'type': 'bind', 'source': '/private/synthetic/cert', 'target': '/cert', 'read_only': True}]}}

    def test_names_networks_ports_and_volumes_are_owned_and_loopback_only(self):
        source = self.document()
        original = copy.deepcopy(source)
        result = m.isolate(source, 'synthetic-owned', {'system-backend': 'sha256:owned-product'})
        self.assertEqual(source, original)
        service = result['services']['system-backend']
        self.assertEqual(service['container_name'], 'synthetic-owned-system-backend')
        self.assertEqual(service['image'], 'sha256:owned-product')
        self.assertEqual(service['restart'], 'no')
        self.assertEqual(service['labels'][m.LABEL], 'synthetic-owned')
        self.assertEqual(service['ports'], [{'target': 8180, 'host_ip': '127.0.0.1', 'published': '0'}])
        self.assertEqual(set(service['networks']), {'data', 'entry'})
        self.assertTrue(result['networks']['data']['internal'])
        for declaration in result['volumes'].values():
            self.assertEqual(declaration['labels'][m.LABEL], 'synthetic-owned')
            self.assertFalse(declaration.get('external'))
        self.assertEqual(service['volumes'][0]['source'], 'runtime-logs')
        self.assertTrue(all(mount['volume']['nocopy'] for mount in service['volumes'] if mount['type']=='volume'))
        for key in ('build', 'depends_on', 'profiles', 'extends'):
            self.assertNotIn(key, service)
        self.assertGreater(service['cpus'], 0)
        self.assertRegex(service['mem_limit'], r'^[1-9][0-9]*m$')

    def test_privileged_host_network_and_writable_bind_drift_fail_closed(self):
        for mutate in [lambda s: s.update(privileged=True), lambda s: s.update(network_mode='host'),
                       lambda s: s['volumes'].append({'type': 'bind', 'source': '/personal', 'target': '/writable', 'read_only': False})]:
            source = self.document()
            mutate(source['system-backend'])
            with self.assertRaises(m.GateError):
                m.isolate(source, 'owned', {})
        with self.assertRaises(m.GateError):
            m.isolate({'unknown-personal-service': {}}, 'owned', {})

    def test_application_services_have_no_external_network_route(self):
        source = {'develop-backend': {'image': 'product', 'volumes': []},
                  'alloy': {'image': 'alloy', 'volumes': []}, 'tempo': {'image': 'tempo', 'volumes': []}}
        isolated = m.isolate(source, 'owned', {})
        for service in isolated['services'].values():
            self.assertEqual(set(service['networks']), {'data'})
            self.assertNotIn('ports', service)


class LocalTraceResourceGuardTests(unittest.TestCase):
    def sample_lifecycle(self, directory, allocation):
        life = bare_lifecycle(directory)
        calls = []
        def docker(*args, **kwargs):
            calls.append((args, kwargs))
            if args[:2] == ('container', 'ls'):
                return 'owned-id'
            if args[:2] == ('volume', 'ls'):
                return 'owned-volume'
            if args[0] == 'inspect':
                return json.dumps([row()])
            if args[0] == 'run':
                return 'owned-sampler-id'
            if args[0] == 'exec':
                return m.VOLUME_SCAN_ENTERED+'\n'+str(allocation)+'\n'+m.VOLUME_SCAN_RETURNED+'\n'
            if args[0] == 'rm':
                return ''
            raise AssertionError(args)
        life.docker = docker
        return life, calls

    def test_last_read_cannot_turn_an_expired_scan_into_success(self):
        for finished in (4, 4.01):
            with self.subTest(finished=finished), tempfile.TemporaryDirectory() as directory:
                life, _ = self.sample_lifecycle(directory, 1024)
                clock = [0.0]
                def read(**kwargs):
                    clock[0] = finished
                    return 0
                life.owned_bind_bytes = read
                with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                    with self.assertRaisesRegex(m.GateError, 'resource_sample_deadline_exceeded'):
                        life.sample()
                self.assertEqual(life.samples, [])

    def test_success_preserves_step_costs_counts_and_shared_remaining_budget(self):
        with tempfile.TemporaryDirectory() as directory:
            life, calls = self.sample_lifecycle(directory, 1024)
            life.sampler_volumes = ('owned-volume',)
            underlying = life.docker
            clock = [0.0]
            def docker(*args, **kwargs):
                clock[0] += .5
                return underlying(*args, **kwargs)
            life.docker = docker
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                life.sample()
            sample = life.samples[-1]
            self.assertEqual(sample['container_count'], 1)
            self.assertEqual(sample['volume_count'], 1)
            self.assertEqual(sample['sample_duration_seconds'], 2)
            self.assertEqual([step['operation'] for step in sample['steps']], [
                'list_owned_containers', 'inspect_owned_containers', 'validate_container_states',
                'list_owned_volumes', 'measure_owned_volumes', 'measure_private_bindings', 'final_validation'])
            self.assertTrue(all(step['result'] == 'passed' for step in sample['steps']))
            self.assertEqual([kwargs['timeout'] for _, kwargs in calls], [4, 3.5, 3, 2.5])

    def test_failed_tick_keeps_completed_and_failed_steps_through_monitor(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 1024)
            life.sampler_volumes = ('owned-volume',)
            underlying = life.docker
            clock = [0.0]
            def docker(*args, **kwargs):
                if args[0] == 'exec':
                    clock[0] = 4.05
                    raise m.GateError('command_deadline_exceeded')
                clock[0] += .1
                return underlying(*args, **kwargs)
            life.docker = docker
            life.owned_bind_bytes = mock.Mock(side_effect=AssertionError('must stop'))
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                life.monitor()
            failure = life.resource_failure
            self.assertEqual(failure['error_code'], 'resource_sample_deadline_exceeded')
            self.assertEqual(failure['operation'], 'measure_owned_volumes')
            self.assertEqual(failure['container_count'], 1)
            self.assertEqual(failure['volume_count'], 1)
            self.assertEqual(failure['steps'][-1]['result'], 'failed')
            self.assertAlmostEqual(failure['steps'][-1]['duration_seconds'], 3.75)
            self.assertTrue(all(step['result'] == 'passed' for step in failure['steps'][:-1]))
            life.owned_bind_bytes.assert_not_called()
            self.assertEqual(life.samples, [])

    def test_lock_wait_is_separate_from_scan_duration(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 1024)
            clock = [0.0]
            def entered(**kwargs): clock[0] = 2; return True
            def read(**kwargs): clock[0] = 3; return 0
            life.sample_lock = mock.MagicMock()
            life.sample_lock.acquire.side_effect = entered
            life.owned_bind_bytes = read
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]):
                life.sample()
            self.assertEqual(life.samples[-1]['lock_wait_seconds'], 2)
            self.assertEqual(life.samples[-1]['sample_duration_seconds'], 1)

    def test_inspect_json_parsing_must_finish_within_original_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 1024)
            clock = [0.0]
            loads = m.json.loads
            def parse(raw): clock[0] = 4.01; return loads(raw)
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.json, 'loads', side_effect=parse):
                with self.assertRaisesRegex(m.GateError, 'resource_sample_deadline_exceeded'):
                    life.sample()
            self.assertEqual(life.samples, [])
            self.assertEqual(life.resource_failure['operation'], 'inspect_owned_containers')
            self.assertEqual(life.resource_failure['steps'][-1]['result'], 'failed')

    def test_actual_volume_stop_line_rejects_equal_threshold(self):
        with tempfile.TemporaryDirectory() as directory:
            for allocated in (m.VOLUME_STOP, m.VOLUME_STOP + 4096):
                life, _ = self.sample_lifecycle(directory, allocated)
                with mock.patch.object(m.time, 'monotonic', return_value=1), self.assertRaises(m.GateError):
                    life.sample()
                self.assertEqual(life.samples, [])
                self.assertEqual(life.resource_failure['measured_resources']['volume_allocated_bytes'], allocated)
            life, calls = self.sample_lifecycle(directory, m.VOLUME_STOP - 4096)
            with mock.patch.object(m.time, 'monotonic', return_value=1):
                life.sample()
            self.assertEqual(life.samples[-1]['volume_allocated_bytes'], m.VOLUME_STOP - 4096)
            usage = next(args for args, _ in calls if args[0] == 'run')
            self.assertIn('none', usage)
            self.assertIn(str(m.SAMPLER_CPU), usage)
            self.assertIn('64m', usage)
            self.assertTrue(all('readonly' in arg for arg in usage if arg.startswith('type=volume,')))

    def test_repeated_sampling_reuses_readonly_helper_and_remounts_changed_owned_set(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.sample_lifecycle(directory,1024)
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.sample()
                life.sample()
                life.owned=lambda kind,**kw:['owned-id'] if kind=='container' else ['owned-volume','new-owned-volume']
                life.sample()
            runs=[args for args,_ in calls if args[0]=='run']
            executions=[args for args,_ in calls if args[0]=='exec']
            removals=[args for args,_ in calls if args[0]=='rm']
            self.assertEqual(len(runs),2)
            self.assertEqual(len(executions),3)
            self.assertEqual(len(removals),1)
            self.assertTrue(all('readonly' in arg for args in runs for arg in args if arg.startswith('type=volume,')))
            self.assertTrue(all('/data:rw,noexec,nosuid,size=1m' in args for args in runs))

    def test_builder_volume_ownership_does_not_capture_application_volumes(self):
        with tempfile.TemporaryDirectory() as directory:
            life, calls = self.sample_lifecycle(directory, 1024)
            life.builder_ids = {'owned-builder'}
            underlying = life.docker
            def docker(*args, **kwargs):
                if args[0] == 'inspect':
                    return json.dumps([row(), dict(row(), Id='owned-builder', Mounts=[
                        {'Type': 'volume', 'Name': 'owned-buildkit-state'},
                        {'Type': 'bind', 'Source': '/not-a-builder-volume'},
                    ])])
                return underlying(*args, **kwargs)
            life.docker = docker
            with mock.patch.object(m.time, 'monotonic', return_value=1):
                life.sample()
            self.assertEqual(life.builder_volumes, {'owned-buildkit-state'})

    def test_invalid_volume_measurement_preserves_only_safe_diagnostics_and_fails(self):
        with tempfile.TemporaryDirectory() as directory:
            life, _ = self.sample_lifecycle(directory, 'du: PRIVATE-SENTINEL: missing\n1024')
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.monitor()
            self.assertEqual(life.failure,'owned_volume_measurement_invalid')
            self.assertEqual(life.resource_failure['exception_type'],'GateError')
            measured=life.resource_failure['measurement']
            self.assertEqual(measured['line_count'],2)
            self.assertTrue(measured['du_diagnostic'])
            self.assertEqual(len(measured['output_sha256']),64)
            self.assertNotIn('PRIVATE-SENTINEL',json.dumps(life.resource_failure))
            self.assertEqual(life.samples,[])

    def test_disappeared_volume_file_requires_a_complete_rescan(self):
        with tempfile.TemporaryDirectory() as directory:
            life,calls=self.sample_lifecycle(directory,1024)
            docker=life.docker;values=iter([m.VOLUME_SCAN_ENTERED+'\n'+value+'\n'+m.VOLUME_SCAN_RETURNED+'\n'
                for value in ('du: owned: No such file or directory\n512','1024')])
            life.docker=lambda *args,**kw: next(values) if args[0]=='exec' else docker(*args,**kw)
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.sample()
            self.assertEqual(life.samples[-1]['volume_allocated_bytes'],1024)

    def test_disappeared_private_file_requires_a_complete_rescan(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.sample_lifecycle(directory,1024)
            life.last_failure={'du_missing_file':True}
            life.owned_bind_bytes=mock.Mock(side_effect=[m.GateError('command_failed_1'),2048])
            with mock.patch.object(m.time,'monotonic',return_value=1):life.sample()
            self.assertEqual(life.samples[-1]['volume_allocated_bytes'],3072)

    def test_continuous_file_churn_exhausts_original_sample_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life,_=self.sample_lifecycle(directory,'du: owned: No such file or directory\n512')
            tick=[0]
            def now():tick[0]+=.1;return tick[0]
            with mock.patch.object(m.time,'monotonic',side_effect=now):
                with self.assertRaisesRegex(m.GateError,'resource_sample_deadline_exceeded'):life.sample()
            self.assertEqual(life.samples,[])

    def test_every_resource_sampling_command_has_a_short_timeout(self):
        with tempfile.TemporaryDirectory() as directory:
            life, calls = self.sample_lifecycle(directory, 1024)
            with mock.patch.object(m.time, 'monotonic', return_value=1):
                life.sample()
            self.assertTrue(calls)
            for args, kwargs in calls:
                self.assertGreater(kwargs.get('timeout', 0), 0, args)
                self.assertLessEqual(kwargs['timeout'], 5, args)

    def test_guard_sample_error_stops_future_guarded_commands(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            life.sample = mock.Mock(side_effect=m.GateError('owned_volume_stop_line_reached'))
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.monitor()
            self.assertEqual(life.failure, 'owned_volume_stop_line_reached')
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.subprocess, 'Popen') as start:
                with self.assertRaises(m.GateError):
                    life.command(['never-start-this'])
            start.assert_not_called()

    def test_sampler_reserve_fits_runtime_browser_and_build_aggregate_limits(self):
        running={k:v for k,v in m.LIMITS.items() if k not in ('runtime-log-store-init','trace-store-init')}
        self.assertLessEqual(sum(v[0] for v in running.values()) + .5 + m.SAMPLER_CPU, 8)
        self.assertLessEqual(sum(v[1] for v in running.values()) + 1024 + 64, 12*1024)
        dependencies={k:v for k,v in m.LIMITS.items() if k in m.TRACE+('postgres','business-postgres','redis') and k not in ('runtime-log-store-init','trace-store-init')}
        rows=[row(cpu,memory*1024**2) for cpu,memory in dependencies.values()]+[row(1,2*1024**3),row(1,2*1024**3),row(m.SAMPLER_CPU,64*1024**2)]
        m.validate_resources(rows)
        with self.assertRaisesRegex(m.GateError,'aggregate_hard_limit_exceeded'):m.validate_resources(rows+[row(1)])

    def test_sampler_timeout_reports_the_failed_operation_and_stops_requests(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            life.sample_operation='measure_owned_volumes'
            life.sample=mock.Mock(side_effect=m.GateError('command_deadline_exceeded'))
            with mock.patch.object(m.time,'monotonic',return_value=1):
                life.monitor()
                self.assertEqual(life.failure,'resource_sample_deadline_exceeded')
                self.assertEqual(life.resource_failure['operation'],'measure_owned_volumes')
                with self.assertRaises(m.GateError):life.request_timeout()

    def test_sampling_duration_is_deducted_from_the_five_second_interval(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            stop = mock.Mock()
            stop.is_set.side_effect = [False, True]
            life.stop = stop
            life.sample = mock.Mock()
            with mock.patch.object(m.time, 'monotonic', side_effect=[10, 12, 12]):
                life.monitor()
            stop.wait.assert_called_once()
            self.assertLessEqual(stop.wait.call_args.args[0], 3)


class ResourceDiagnosticContractTests(unittest.TestCase):
    @staticmethod
    def scan(phase, started, duration=.1):
        return {'phase':phase, 'sample_started_monotonic':started, 'sample_finished_monotonic':started+duration,
            'lock_wait_seconds':0, 'container_count':20, 'volume_count':8,
            'complete_scan_retries':{'volume':0,'private':0},
            'steps':[{'operation':'inspect_owned_containers','duration_seconds':duration,'processing_seconds':.01,
                'commands':[{'spawn_seconds':.02,'wait_seconds':.03,'output_seconds':.01,'cancel_cleanup_seconds':0}]}]}

    def test_phase_counts_and_step_distributions(self):
        rows=[]
        for phase,count in [('product_compile',12),('product_image_gateway',12),('diagnostic_idle',25),('diagnostic_steady',65)]:
            for _ in range(count): rows.append(self.scan(phase,len(rows)*4))
        rows.append(self.scan('diagnostic_startup_1',len(rows)*4))
        summary=m.summarize_resource_diagnostic(rows)
        m.validate_resource_diagnostic(summary,('compile','images','idle','steady'))
        self.assertEqual(summary['phases']['compile']['sample_count'],12)
        self.assertEqual(summary['phases']['startup']['sample_count'],1)
        costs=summary['phases']['compile']['operations']['inspect_owned_containers']
        self.assertEqual(costs['wait_seconds']['p99'],.03)
        self.assertEqual(summary['max_start_interval_seconds'],4)

    def test_missing_phase_and_insufficient_samples_are_rejected(self):
        summary=m.summarize_resource_diagnostic([self.scan('product_compile',0)])
        for phases in [('compile',),('images',),('idle',),('steady',)]:
            with self.subTest(phases=phases),self.assertRaisesRegex(m.GateError,'diagnostic_phase_samples_insufficient'):
                m.validate_resource_diagnostic(summary,phases)

    def test_four_second_and_five_second_boundaries(self):
        for duration,gap,allowed in [(3.999,5,True),(4,4,False),(.1,5.001,False)]:
            summary=m.summarize_resource_diagnostic([self.scan('product_compile',0,duration),self.scan('product_compile',gap)])
            with self.subTest(duration=duration,gap=gap):
                if allowed:m.validate_resource_diagnostic(summary,())
                else:
                    with self.assertRaises(m.GateError):m.validate_resource_diagnostic(summary,())

    def fixture(self,directory):
        life=bare_lifecycle(directory);clock=[0.0];life.command=mock.Mock(return_value='')
        life.compose=mock.Mock(side_effect=lambda *args,**kwargs:life.command(['docker','compose'],timeout=300))
        life.input_hashes={'owned':'hash'};life.module_commits={'system':'source'}
        def sample():
            life.samples.append(self.scan(life.phase,clock[0]));clock[0]+=.1
        def wait(seconds):
            end=clock[0]+seconds
            while clock[0]+4<end:
                clock[0]+=4;sample()
            clock[0]=end
        def build(life):
            life.command(['make','build'],timeout=2200)
            for phase in ('product_compile','product_image_gateway'):
                life.phase=phase
                for _ in range(12):clock[0]+=4;sample()
            return 'source',{},{}
        def setup(life,env,images):
            m.start_deployment(life.compose)
        life.sample=sample;life.wait=wait
        return life,clock,build,setup

    def test_one_round_uses_shared_build_deploy_and_six_checks_without_identity(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,build,setup=self.fixture(directory);command=life.command;compose=life.compose;report={}
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=build) as built, mock.patch.object(m,'setup',side_effect=setup), \
                 mock.patch.object(m,'identity',side_effect=AssertionError('identity forbidden')):
                m.run_resource_guard_diagnostic(life,report,0)
            built.assert_called_once()
            self.assertEqual(len(report['startup_checks']),6)
            self.assertEqual(compose.call_count,3)
            self.assertIs(life.command,command);self.assertIs(life.compose,compose)
            self.assertEqual(command.call_args_list[0].kwargs['timeout'],1800)
            self.assertTrue(all(0 < call.kwargs['timeout'] <= 180 for call in command.call_args_list[1:]))
            self.assertGreaterEqual(report['summary']['phases']['idle']['sample_count'],25)
            self.assertGreaterEqual(report['summary']['phases']['steady']['sample_count'],65)

    def test_first_failure_propagates_without_retry_and_restores_wrapper(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,_=self.fixture(directory);command=life.command
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=m.GateError('resource_sample_deadline_exceeded')) as build, \
                 mock.patch.object(m,'setup') as setup:
                with self.assertRaisesRegex(m.GateError,'resource_sample_deadline_exceeded'):
                    m.run_resource_guard_diagnostic(life,{},0)
            build.assert_called_once();setup.assert_not_called();self.assertIs(life.command,command)

    def test_build_startup_and_whole_window_deadlines_are_absolute(self):
        for stage in ('build','startup','whole'):
            with self.subTest(stage=stage),tempfile.TemporaryDirectory() as directory:
                life,clock,build,setup=self.fixture(directory);command=life.command;compose=life.compose
                def bounded_build(life):
                    result=build(life)
                    if stage=='build':clock[0]=1800
                    return result
                def bounded_setup(life,env,images):
                    setup(life,env,images)
                    if stage=='startup':clock[0]+=180
                    if stage=='whole':clock[0]=2399
                with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                     mock.patch.object(m,'build',side_effect=bounded_build),mock.patch.object(m,'setup',side_effect=bounded_setup):
                    with self.assertRaises(m.GateError):m.run_resource_guard_diagnostic(life,{},0)
                self.assertIs(life.command,command);self.assertIs(life.compose,compose)

    def test_changed_startup_groups_are_rejected_and_not_retried(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,build,_=self.fixture(directory);compose=life.compose
            def setup(life,env,images):life.compose('up','-d','--wait','--no-build','monitor-backend')
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=build),mock.patch.object(m,'setup',side_effect=setup):
                with self.assertRaisesRegex(m.GateError,'diagnostic_startup_group_mismatch'):
                    m.run_resource_guard_diagnostic(life,{},0)
            compose.assert_not_called();self.assertIs(life.compose,compose)

    def test_cadence_failure_stops_during_build_without_waiting_for_summary(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,_=self.fixture(directory);original_sample=life.sample
            def build(life):
                life.phase='product_compile';life.sample();clock[0]+=6;life.sample()
                raise AssertionError('must stop at cadence violation')
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m,'build',side_effect=build) as built,mock.patch.object(m,'setup') as setup:
                with self.assertRaisesRegex(m.GateError,'diagnostic_sample_cadence_exceeded'):
                    m.run_resource_guard_diagnostic(life,{},0)
            built.assert_called_once();setup.assert_not_called();self.assertIs(life.sample,original_sample)


class HostCLIComparisonTests(unittest.TestCase):
    def fixture(self, directory, *, command_seconds=.01, cpu_seconds=.02):
        life=bare_lifecycle(directory);clock=[0.0];cpu=[0.0]
        process=mock.Mock(pid=1234,returncode=0);process.poll.return_value=0
        def wait(**kwargs):clock[0]+=command_seconds;cpu[0]+=cpu_seconds
        process.wait.side_effect=wait
        def usage():
            return {'parent_user_seconds':0.0,'parent_system_seconds':0.0,
                'children_user_seconds':cpu[0],'children_system_seconds':0.0,
                'parent_rss_high_water_bytes':1024**2,'children_rss_high_water_bytes':2*1024**2}
        return life,clock,cpu,process,usage

    def invoke(self, life, clock, process, usage, report):
        def sleep(seconds):clock[0]+=seconds
        with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
             mock.patch.object(m.time,'monotonic_ns',side_effect=lambda:round(clock[0]*1e9)), \
             mock.patch.object(m.time,'sleep',side_effect=sleep), \
             mock.patch.object(m.subprocess,'Popen',return_value=process), \
             mock.patch.object(m,'host_cli_usage',side_effect=usage), \
             mock.patch.object(m,'require_no_child_processes'):
            m.run_host_cli_comparison(life,report,0,usage())

    def test_twenty_serial_groups_preserve_spacing_and_allow_multithread_cpu_above_wall(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);report={}
            self.invoke(life,clock,process,usage,report)
            self.assertEqual(report['measurement_result'],'passed')
            self.assertEqual(len(report['groups']),20)
            self.assertEqual(len(report['commands']),40)
            starts=[r['spawn_started_monotonic_ns'] for r in report['commands']]
            self.assertTrue(all(b-a>=10**9 for a,b in zip(starts,starts[1:])))
            self.assertTrue(all(r['child_user_cpu_seconds']>r['duration_seconds'] for r in report['commands']))
            self.assertTrue(all(g['duration_seconds']<4 for g in report['groups']))
            self.assertFalse(any('wait_estimate' in k for r in report['commands'] for k in r))

    def test_first_query_failure_stops_without_retry_and_keeps_cpu_record(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);process.returncode=17;report={}
            with self.assertRaisesRegex(m.GateError,'command_failed_17'):
                self.invoke(life,clock,process,usage,report)
            self.assertEqual(len(report['commands']),1)
            self.assertIn('child_user_cpu_seconds',report['commands'][0])
            self.assertEqual(report['measurement_result'],'failed')

    def test_group_records_and_waiting_count_towards_four_seconds(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory,command_seconds=2.1);report={}
            with self.assertRaisesRegex(m.GateError,'command_deadline_exceeded'):
                self.invoke(life,clock,process,usage,report)
            self.assertEqual(len(report['commands']),2)
            self.assertLess(len(report['groups']),20)

    def test_expired_request_budget_does_not_spawn_any_query(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);clock[0]=118;report={}
            with self.assertRaisesRegex(m.GateError,'host_cli_request_deadline_exceeded'):
                self.invoke(life,clock,process,usage,report)
            process.wait.assert_not_called()
            self.assertEqual(report['commands'],[])

    def test_cpu_and_rss_stop_lines_use_totals_and_high_water_not_rss_delta(self):
        origin={'parent_user_seconds':0.,'parent_system_seconds':0.,'children_user_seconds':0.,
                'children_system_seconds':0.,'parent_rss_high_water_bytes':200*1024**2,'children_rss_high_water_bytes':0}
        for changes,code in (({'children_user_seconds':30.},'host_cli_cpu_limit'),
                             ({'parent_rss_high_water_bytes':256*1024**2},'host_cli_rss_limit'),
                             ({'children_system_seconds':-1.},'host_cli_cpu_counter_invalid')):
            with self.subTest(changes=changes),self.assertRaisesRegex(m.GateError,code):
                m.check_host_cli_usage(origin,{**origin,**changes},{})

    def test_extra_or_unreaped_children_refuse_cpu_attribution(self):
        with mock.patch.object(m.os,'waitpid',side_effect=ChildProcessError):m.require_no_child_processes()
        for result in ((0,0),(1234,0)):
            with mock.patch.object(m.os,'waitpid',return_value=result),self.assertRaisesRegex(m.GateError,'host_cli_extra_child'):
                m.require_no_child_processes()

    def test_cpu_usage_normalizes_platform_rss_units(self):
        row=SimpleNamespace(ru_utime=.1,ru_stime=.2,ru_maxrss=123)
        with mock.patch.object(m.resource,'getrusage',return_value=row):
            with mock.patch.object(m.sys,'platform','darwin'):self.assertEqual(m.host_cli_usage()['parent_rss_high_water_bytes'],123)
            with mock.patch.object(m.sys,'platform','linux'):self.assertEqual(m.host_cli_usage()['parent_rss_high_water_bytes'],123*1024)

    def test_unexpected_objects_stop_without_exporting_names(self):
        with tempfile.TemporaryDirectory() as directory:
            life,clock,_,process,usage=self.fixture(directory);report={}
            def start(*args,**kwargs):kwargs['stdout'].write(b'PRIVATE-OBJECT\n');return process
            # invoke owns its mock; return a process whose wait writes the output.
            def wait(**kwargs):clock[0]+=.01
            def owned(kind,**kwargs):
                with mock.patch.object(m.subprocess,'Popen',side_effect=start):
                    return m.Lifecycle.owned(life,kind,**kwargs)
            life.owned=owned;process.wait.side_effect=wait
            with self.assertRaisesRegex(m.GateError,'host_cli_unexpected_objects'):
                self.invoke(life,clock,process,usage,report)
            self.assertNotIn('PRIVATE',json.dumps(report))

    def test_buffer_and_output_stop_lines_are_checked_without_truncating(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            with mock.patch.object(m,'HOST_CLI_OUTPUT_LIMIT',100),self.assertRaisesRegex(m.GateError,'host_cli_output_limit'):
                m.check_host_cli_buffers(life,{'large':'x'*200})
            with mock.patch.object(m,'HOST_CLI_BUFFER_LIMIT',100),self.assertRaisesRegex(m.GateError,'host_cli_buffer_limit'):
                m.check_host_cli_buffers(life,{})

    def test_context_resolution_respects_precedence_and_does_not_copy_auth(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);name='synthetic-context'
            (root/'config.json').write_text(json.dumps({'currentContext':name,'auths':{'PRIVATE':'PRIVATE-TOKEN'}}))
            metadata=root/'contexts/meta'/m.hashlib.sha256(name.encode()).hexdigest()/'meta.json'
            metadata.parent.mkdir(parents=True)
            metadata.write_text(json.dumps({'Name':name,'Endpoints':{'docker':{'Host':'unix:///synthetic'}}}))
            self.assertEqual(m.readonly_docker_endpoint({},root),'unix:///synthetic')
            self.assertEqual(m.readonly_docker_endpoint({'DOCKER_HOST':'unix:///explicit'},root),'unix:///explicit')
            self.assertEqual(m.readonly_docker_endpoint({'DOCKER_CONTEXT':name,'DOCKER_HOST':'unix:///ignored'},root),'unix:///synthetic')
            metadata.write_text(json.dumps({'Name':'wrong'}))
            with self.assertRaisesRegex(m.GateError,'host_cli_context_identity_mismatch'):m.readonly_docker_endpoint({},root)
            (root/'config.json').write_text('x'*65537)
            with self.assertRaisesRegex(m.GateError,'host_cli_context_file_invalid'):m.readonly_docker_endpoint({},root)

    def test_bounded_output_failure_preserves_diagnostic_and_never_passes_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);process=mock.Mock(pid=1234,returncode=0);process.poll.return_value=0
            def start(*args,**kwargs):kwargs['stdout'].write(b'PRIVATE-SENTINEL'*100);return process
            with mock.patch.object(m.subprocess,'Popen',side_effect=start),self.assertRaisesRegex(m.GateError,'command_output_limit_exceeded'):
                life.owned('container',timeout=4,output_limit=100)
            record=life.command_evidence.snapshot()[0]
            self.assertEqual(record['result'],'failed')
            self.assertTrue(record['record_complete'])
            self.assertNotIn('PRIVATE',json.dumps(record))


class CommandBoundaryEvidenceTests(unittest.TestCase):
    def test_formal_report_exports_cleanup_records_even_on_admission_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory)/'owned';work.mkdir()
            life=bare_lifecycle(work);target=Path(directory)/'report.json'
            def cleanup():
                record=life.command_evidence.begin(['docker','volume','ls','-q','--filter',
                    f'label={m.LABEL}={life.run_id}'],life.run_id,'cleanup')
                life.command_evidence.finish(record)
                return []
            life.cleanup=mock.Mock(side_effect=cleanup)
            with mock.patch.object(m,'Lifecycle',return_value=life), \
                 mock.patch.object(m.sys,'argv',['runner']), \
                 mock.patch.dict(m.os.environ,{'MONITOR_TRACE_LOCAL_REPORT':str(target)},clear=True), \
                 mock.patch.object(m,'effective_docker_endpoint',return_value='unix:///synthetic'), \
                 mock.patch.object(m,'private_docker_client',side_effect=m.GateError('synthetic_admission_failure')), \
                 mock.patch('builtins.print'):
                self.assertEqual(m.main(),1)
            report=json.loads(target.read_text())
            self.assertEqual(report['owned_list_commands'][0]['phase'],'cleanup')
            self.assertTrue(report['owned_list_commands'][0]['record_complete'])
            self.assertEqual(report['result'],'failed')

    def test_standalone_nginx_cleanup_does_not_require_a_formal_report(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.cleanup=mock.Mock(return_value=[])
            life.nginx_route_assertions={'synthetic':True}
            with mock.patch.object(m,'Lifecycle',return_value=life), \
                 mock.patch.object(m,'effective_docker_endpoint',return_value='unix:///synthetic'), \
                 mock.patch.object(m,'private_docker_client'),mock.patch.object(m,'verify_nginx_routes'), \
                 mock.patch('builtins.print'):
                m.nginx_route_contract()
            life.cleanup.assert_called_once()

    def test_owned_lists_are_registered_outside_sampling_and_contain_no_arguments(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            process=mock.Mock(pid=1234,returncode=0);process.poll.return_value=0
            with mock.patch.object(m.subprocess,'Popen',return_value=process):
                life.owned('container',timeout=4)
                life.phase='cleanup';life.owned('volume',timeout=4)
            records=life.command_evidence.snapshot()
            self.assertEqual([r['phase'] for r in records],['synthetic','cleanup'])
            self.assertEqual(len({r['command_id'] for r in records}),2)
            self.assertEqual([r['operation'] for r in records],['list_owned_containers','list_owned_volumes'])
            for r in records:
                self.assertEqual(r['pid'],1234)
                self.assertRegex(r['request_signature_sha256'],r'^[a-f0-9]{64}$')
                points=[r[k] for k in ('entered_monotonic_ns','spawn_started_monotonic_ns',
                    'spawn_finished_monotonic_ns','exit_observed_monotonic_ns','output_finished_monotonic_ns',
                    'decision_monotonic_ns','finished_monotonic_ns')]
                self.assertTrue(all(isinstance(v,int) for v in points))
                self.assertEqual(points,sorted(points))
                self.assertEqual(r['duration_seconds'],round((points[-1]-points[0])/1e9,6))
                self.assertNotIn('args',r);self.assertNotIn('env',r)

    def test_spawn_failure_retains_boundary_and_releases_signature(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            with mock.patch.object(m.subprocess,'Popen',side_effect=OSError('PRIVATE-TOKEN')):
                with self.assertRaises(OSError):life.owned('container',timeout=4)
                with self.assertRaises(OSError):life.owned('container',timeout=4)
            rows=life.command_evidence.snapshot()
            self.assertEqual(len(rows),2)
            self.assertTrue(all(r['result']=='failed' and r['pid'] is None for r in rows))
            self.assertTrue(all(r['spawn_finished_monotonic_ns'] is not None and r['exit_observed_monotonic_ns'] is None for r in rows))
            self.assertNotIn('PRIVATE',json.dumps(rows))

    def test_ledger_rejects_overlap_and_reserves_bounded_cleanup_space(self):
        ledger=m.CommandEvidence(limit=1)
        args=['docker','container','ls','-a','-q','--filter',f'label={m.LABEL}=owned']
        first=ledger.begin(args,'owned','preparation')
        with self.assertRaisesRegex(m.GateError,'owned_list_command_overlap'):
            ledger.begin(args,'owned','sampling')
        ledger.finish(first)
        with self.assertRaisesRegex(m.GateError,'command_evidence_limit'):
            ledger.begin(args,'owned','sampling')
        cleanup=ledger.begin(args,'owned','cleanup');ledger.finish(cleanup)
        self.assertEqual(len(ledger.snapshot()),2)
        for _ in range(7):
            cleanup=ledger.begin(args,'owned','cleanup');ledger.finish(cleanup)
        with self.assertRaisesRegex(m.GateError,'command_evidence_limit'):
            ledger.begin(args,'owned','cleanup')

    def test_unfinished_worker_does_not_hide_failure_report(self):
        ledger=m.CommandEvidence()
        args=['docker','volume','ls','-q','--filter',f'label={m.LABEL}=owned']
        pending=ledger.begin(args,'owned','preparation')
        self.assertFalse(ledger.snapshot()[0]['record_complete'])
        ledger.finish(pending)
        self.assertTrue(ledger.snapshot()[0]['record_complete'])

    def test_signature_separates_runs_and_does_not_accept_broader_queries(self):
        def args(run):return ['docker','volume','ls','-q','--filter',f'label={m.LABEL}={run}']
        a=m.owned_list_identity(args('one'),'one')
        b=m.owned_list_identity(args('two'),'two')
        self.assertNotEqual(a['request_signature_sha256'],b['request_signature_sha256'])
        self.assertIsNone(m.owned_list_identity(args('other'),'owned'))
        self.assertIsNone(m.owned_list_identity(args('owned')+['--filter','name=PRIVATE'],'owned'))

    def test_raw_secrets_and_unowned_queries_never_enter_ledger(self):
        ledger=m.CommandEvidence()
        record=ledger.begin(['docker','inspect','PRIVATE-ID'],'owned','preparation')
        ledger.finish(record)
        record=ledger.begin(['docker','container','ls','-a','-q','--filter','label=PRIVATE-TOKEN'],
                            'owned','preparation')
        ledger.finish(record)
        self.assertEqual(ledger.snapshot(),[])

    def test_clock_interval_uses_bounds_not_symmetric_midpoint(self):
        self.assertEqual(m.apiproxy_clock_interval(100,200,1000,1050),(-900,-850))
        with self.assertRaisesRegex(m.GateError,'clock_event_order_invalid'):
            m.apiproxy_clock_interval(200,100,1000,1050)
        with self.assertRaisesRegex(m.GateError,'clock_interval_empty'):
            m.apiproxy_clock_interval(100,120,1000,1050)

    def test_clock_envelope_rejects_excess_uncertainty_and_wall_jump(self):
        self.assertEqual(m.apiproxy_clock_envelope((10,20),(15,25)),(10,25))
        with self.assertRaisesRegex(m.GateError,'clock_uncertainty_exceeded'):
            m.apiproxy_clock_envelope((0,100),(0,50000001))
        with self.assertRaisesRegex(m.GateError,'clock_wall_jump'):
            m.validate_clock_anchors([(100,1000,101),(200,1000000000,201)])
        with self.assertRaisesRegex(m.GateError,'clock_intervals_inconsistent'):
            m.apiproxy_clock_envelope((0,10),(11,20))

    def test_pairing_refuses_overlap_or_missing_without_request_ids(self):
        def event(direction,t):return {'direction':direction,'timestamp_ns':t,'request_signature_sha256':'a'*64}
        self.assertEqual(len(m.pair_apiproxy_events([event('>>',1),event('<<',2)])),1)
        for events in ([event('>>',1)],[event('<<',1)],
                       [event('>>',1),event('>>',2),event('<<',3),event('<<',4)],
                       [event('>>',1),event('<<',2),event('<<',2)]):
            with self.subTest(events=events),self.assertRaises(m.GateError):m.pair_apiproxy_events(events)

    def test_request_attribution_rejects_ambiguity_missing_and_reuse(self):
        command={'command_id':1,'request_signature_sha256':'a'*64,'entered_monotonic_ns':100,
                 'decision_monotonic_ns':200,'result':'passed'}
        pair={'request_signature_sha256':'a'*64,'started_ns':1000,'finished_ns':1010}
        self.assertEqual(m.attribute_apiproxy_requests([command],[pair],(-900,-850)),{1:0})
        for commands,pairs in (([command],[]),([command],[pair,pair]),([command,{**command,'command_id':2}],[pair]),
                               ([command],[{**pair,'started_ns':5000,'finished_ns':5010}])):
            with self.subTest(commands=commands,pairs=pairs),self.assertRaises(m.GateError):
                m.attribute_apiproxy_requests(commands,pairs,(-900,-850))

    def test_clock_proof_requires_version_bound_event_time_evidence(self):
        with self.assertRaisesRegex(m.GateError,'apiproxy_clock_semantics_unconfirmed'):
            m.require_apiproxy_clock_semantics(None,'4.49.0')
        with self.assertRaisesRegex(m.GateError,'apiproxy_clock_semantics_unconfirmed'):
            m.require_apiproxy_clock_semantics({'version':'4.49.0','source':'log_path_only'},'4.49.0')


class VolumeCheckpointTests(unittest.TestCase):
    def capture_attribution(self, body, roles, *, deadline=None, complete=True):
        with tempfile.TemporaryFile() as output:
            raw = (m.VOLUME_SCAN_ENTERED+'\n'+body+'\n1024\n').encode()
            if complete:
                raw += (m.VOLUME_SCAN_RETURNED+'\n').encode()
            output.write(raw)
            output.flush()
            timing = {}
            run_id = 'addp-http-local-synthetic'
            args = ['docker','exec',run_id+'-space','/bin/sh','-ec',m.VOLUME_SCAN_COMMAND]
            with m.capture_volume_checkpoints(args,run_id,output,timing,True,volume_roles=roles,deadline=deadline):
                pass
            summary = timing['volume_measurement_checkpoints']
            self.assertLessEqual(len(json.dumps(summary, separators=(',', ':')).encode()), 512)
            self.assertNotIn('PRIVATE', repr(summary))
            return summary['path_disappearance']

    def test_attribution_uses_exact_sorted_sampler_slots_and_deduplicates_objects(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            builder = 'buildx_buildkit_'+life.run_id+'0_state'
            postgres = life.run_id+'_postgres_data'
            life.builder_volumes.add(builder)
            life.docker = mock.Mock()
            life.prepare_volume_sampler([builder, postgres], timeout=30)
            args = life.docker.call_args.args
            self.assertIn(f'type=volume,source={postgres},target=/space/0,readonly', args)
            self.assertIn(f'type=volume,source={builder},target=/space/1,readonly', args)
            body = ("du: /space/0: No such file or directory\n"
                    "du: can't stat '/space/1/PRIVATE': No such file or directory\n"
                    "du: /space/1/PRIVATE: No such file or directory")
            result = self.capture_attribution(body, life.sampler_volume_roles)
        self.assertEqual(result['status'], 'attributed')
        self.assertEqual(result['slots'], [[0, 'postgres_data', 1, 0], [1, 'buildkit_state', 0, 1]])
        self.assertEqual(result['object_set_sha256'], m.hashlib.sha256(
            b'addp-http-local-synthetic\0/space/0\0/space/1/PRIVATE').hexdigest())

    def test_volume_attribution_rejects_untrusted_or_over_budget_output_as_a_group(self):
        line = 'du: /space/0/PRIVATE: No such file or directory'
        for body, reason in (
            (line+'\ndu: /space/0/other: Permission denied', 'unrecognized_diagnostic'),
            (line.replace('/space/0/', '/space/1/'), 'unowned_slot'),
            (line.replace('/space/0/', '/space/0/../'), 'invalid_path'),
            (line.replace('/space/0/', '/space/00/'), 'invalid_path'),
            (line.replace('/space/0/', '/space/0/a//'), 'invalid_path'),
            (line.replace('/space/0/', '/space/0/\\'), 'invalid_path'),
            ('\n'.join([line]*17), 'diagnostic_limit'),
            (line.replace('PRIVATE', 'P'*4096), 'over_limit'),
        ):
            with self.subTest(reason=reason):
                self.assertEqual(self.capture_attribution(body, ('buildkit_state',)),
                                 {'status': 'unknown', 'reason': reason})
        self.assertEqual(self.capture_attribution('\n'.join([line]*16), ('buildkit_state',))['slots'],
                         [[0, 'buildkit_state', 0, 1]])
        self.assertEqual(self.capture_attribution(line, ('buildkit_state',), complete=False),
                         {'status': 'unknown', 'reason': 'partial'})
        many = '\n'.join(line.replace('/space/0/', f'/space/{i}/') for i in range(16))
        self.assertEqual(self.capture_attribution(many, ('prometheus_data',)*16),
                         {'status': 'unknown', 'reason': 'summary_limit'})
        self.assertEqual(self.capture_attribution(line, ('buildkit_state',), deadline=0),
                         {'status': 'unknown', 'reason': 'deadline'})
        with mock.patch.object(m, 'volume_path_disappearance', side_effect=OSError('PRIVATE')):
            self.assertEqual(self.capture_attribution(line, ('buildkit_state',)),
                             {'status': 'unknown', 'reason': 'unavailable'})

    def test_attributed_disappearance_keeps_the_strict_sample_failure(self):
        raw = (m.VOLUME_SCAN_ENTERED+'\ndu: /space/0/PRIVATE: No such file or directory\n1024\n'
               +m.VOLUME_SCAN_RETURNED+'\n').encode()
        with tempfile.TemporaryDirectory() as directory:
            life, _ = LocalTraceResourceGuardTests().sample_lifecycle(directory, 1024)
            life.started = m.time.monotonic()
            life.strict_resources = True
            life.sampler_volumes = ('owned-volume',)
            life.sampler_volume_roles = ('buildkit_state',)
            original = life.docker
            def docker(*args, **kwargs):
                if args[0] == 'exec':
                    return m.Lifecycle.docker(life,*args,**kwargs)
                if args[0] == 'inspect':
                    return json.dumps([dict(row(), SizeRw=0)])
                return original(*args,**kwargs)
            life.docker = docker
            life.owned_bind_bytes = mock.Mock(side_effect=AssertionError('do not continue'))
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            def start(*args, **kwargs):
                kwargs['stdout'].write(raw)
                kwargs['stdout'].flush()
                return process
            with mock.patch.object(m.subprocess, 'Popen', side_effect=start) as spawn:
                life.monitor()
            spawn.assert_called_once()
            self.assertEqual(life.failure, 'owned_resource_measurement_failed')
            self.assertEqual(life.samples, [])
            life.owned_bind_bytes.assert_not_called()
            evidence = life.resource_failure['steps'][-1]['commands'][0]['volume_measurement_checkpoints']
            self.assertEqual(evidence['path_disappearance']['slots'], [[0,'buildkit_state',0,1]])
            self.assertNotIn('PRIVATE', repr(life.resource_failure))

    def test_shell_frames_the_same_pipeline_and_return_is_not_du_success(self):
        for failed in (False,True):
            du="du() { printf '1 /space/0\\n2 /space/1\\n'; }; "
            if failed:du="du() { printf 'du: PRIVATE: denied\\n' >&2; return 1; }; "
            result=m.subprocess.run(['/bin/sh','-ec',du+m.VOLUME_SCAN_COMMAND],
                stdout=m.subprocess.PIPE,stderr=m.subprocess.STDOUT,timeout=2,check=False)
            self.assertEqual(result.returncode,0)
            lines=result.stdout.splitlines()
            self.assertEqual(lines[0],b'ADDP_VOLUME_SCAN_V1_ENTERED')
            self.assertEqual(lines[-1],b'ADDP_VOLUME_SCAN_V1_RETURNED')
            if not failed:self.assertEqual(lines[1:-1],[b'3072'])
            else:self.assertIn(b'du: PRIVATE: denied',lines)
            summary=m.volume_measurement_checkpoints(result.stdout)
            self.assertEqual(summary['pipeline_returned'],'observed')
            self.assertNotIn('PRIVATE',json.dumps(summary))

    def test_complete_and_partial_checkpoints_are_observations_only(self):
        parse = m.volume_measurement_checkpoints
        complete = parse(b'ADDP_VOLUME_SCAN_V1_ENTERED\n1024\nADDP_VOLUME_SCAN_V1_RETURNED\n')
        self.assertEqual(complete['fragment_status'], 'complete')
        self.assertEqual(complete['measurement_entered'], 'observed')
        self.assertEqual(complete['pipeline_returned'], 'observed')
        for raw, entered in ((b'', 'not_observed'), (b'ADDP_VOLUME_SCAN_V1_ENTERED\n', 'observed')):
            with self.subTest(raw=raw):
                summary = parse(raw)
                self.assertEqual(summary['fragment_status'], 'partial')
                self.assertEqual(summary['measurement_entered'], entered)
                self.assertEqual(summary['pipeline_returned'], 'not_observed')
                self.assertNotIn('executed', summary)

    def test_invalid_truncated_unavailable_and_over_limit_never_infer_execution(self):
        entered=b'ADDP_VOLUME_SCAN_V1_ENTERED\n';returned=b'ADDP_VOLUME_SCAN_V1_RETURNED\n'
        for raw in (returned, returned+entered, entered+entered, entered+returned+returned,
                    b'ADDP_VOLUME_SCAN_V1_BOGUS\n'):
            with self.subTest(raw=raw):
                summary=m.volume_measurement_checkpoints(raw)
                self.assertEqual(summary['fragment_status'],'invalid')
                self.assertEqual(summary['measurement_entered'],'unavailable')
        summary=m.volume_measurement_checkpoints(entered+returned[:-1])
        self.assertEqual(summary['pipeline_returned'],'not_observed')
        self.assertEqual(m.volume_measurement_checkpoints(None)['fragment_status'],'unavailable')
        for raw in (b'PRIVATE'*700, entered+b'PRIVATE'*700):
            summary=m.volume_measurement_checkpoints(raw)
            self.assertEqual(summary['fragment_status'],'over_limit')
            self.assertEqual(summary['measurement_entered'],'unavailable')
            self.assertLessEqual(summary['examined_bytes'],4096)
            self.assertLessEqual(len(json.dumps(summary).encode()),512)
            self.assertNotIn('PRIVATE',json.dumps(summary))

    def test_capture_reads_at_most_four_kib_and_only_exact_sampling_command(self):
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryFile() as output:
            life=bare_lifecycle(directory);output.write(b'PRIVATE'*700);output.flush()
            wrapped=mock.Mock(wraps=output);wrapped.fileno.return_value=output.fileno()
            args=['docker','exec',life.run_id+'-space','/bin/sh','-ec',m.VOLUME_SCAN_COMMAND]
            timing={}
            with m.capture_volume_checkpoints(args,life.run_id,wrapped,timing,True):pass
            wrapped.read.assert_called_once_with(4096)
            self.assertEqual(timing['volume_measurement_checkpoints']['fragment_status'],'over_limit')
            for command,sampling in ((args,False),(args[:-1]+['unrelated'],True),
                    (['docker','exec','foreign-space',*args[3:]],True)):
                wrapped.reset_mock();timing={}
                with m.capture_volume_checkpoints(command,life.run_id,wrapped,timing,sampling):pass
                wrapped.read.assert_not_called()
                self.assertEqual(timing,{})

    def test_capture_read_failure_does_not_replace_original_exception(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);output=mock.Mock();output.fileno.side_effect=OSError('PRIVATE')
            timing={};args=['docker','exec',life.run_id+'-space','/bin/sh','-ec',m.VOLUME_SCAN_COMMAND]
            with self.assertRaisesRegex(m.GateError,'original_failure'):
                with m.capture_volume_checkpoints(args,life.run_id,output,timing,True):raise m.GateError('original_failure')
            self.assertEqual(timing['volume_measurement_checkpoints']['fragment_status'],'unavailable')
            self.assertNotIn('PRIVATE',json.dumps(timing))

    def test_timeout_retains_only_this_commands_fragment_and_original_deadline(self):
        entered=b'ADDP_VOLUME_SCAN_V1_ENTERED\n';returned=b'ADDP_VOLUME_SCAN_V1_RETURNED\n'
        for raw,observed in ((b'','not_observed'),(entered,'not_observed'),
                             (entered+b'PRIVATE\n1024\n'+returned,'observed')):
            with self.subTest(raw=raw),tempfile.TemporaryDirectory() as directory:
                life=bare_lifecycle(directory);clock=[0.0]
                life.sample_thread_id=threading.get_ident();life.sample_phase='product_compile'
                life.sample_deadline=4;life.sample_step={'commands':[]}
                process=mock.Mock(pid=654321,returncode=0);process.poll.return_value=None
                def start(*args,**kwargs):kwargs['stdout'].write(raw);kwargs['stdout'].flush();clock[0]=.01;return process
                def wait(**kwargs):
                    if kwargs['timeout']<=.05:
                        clock[0]=4;raise m.subprocess.TimeoutExpired('PRIVATE',kwargs['timeout'])
                    clock[0]+=.2;process.poll.return_value=0
                process.wait.side_effect=wait
                with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                     mock.patch.object(m.time,'monotonic_ns',side_effect=lambda:round(clock[0]*1e9)), \
                     mock.patch.object(m.subprocess,'Popen',side_effect=start) as spawn, \
                     mock.patch.object(m.os,'killpg') as kill:
                    with self.assertRaisesRegex(m.GateError,'command_deadline_exceeded'):
                        life.command(['docker','exec',life.run_id+'-space','/bin/sh','-ec',m.VOLUME_SCAN_COMMAND],timeout=4)
                record=life.sample_step['commands'][0]
                self.assertEqual(record['volume_measurement_checkpoints']['pipeline_returned'],observed)
                self.assertEqual(record['result'],'failed')
                self.assertEqual(record['cancel_started_monotonic_ns'],4_000_000_000)
                self.assertEqual(record['cancel_cleanup_seconds'],.2)
                self.assertEqual(record['finished_monotonic_ns'],4_200_000_000)
                self.assertEqual(life.samples,[])
                self.assertNotIn('PRIVATE',json.dumps(record))
                spawn.assert_called_once();kill.assert_called_once_with(process.pid,signal.SIGTERM)

    def test_successful_output_diagnostics_cannot_pass_after_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);clock=[0.0]
            life.sample_thread_id=threading.get_ident();life.sample_phase='product_compile'
            life.sample_deadline=4;life.sample_step={'commands':[]}
            process=mock.Mock(pid=654321,returncode=0);process.poll.return_value=0
            def start(*args,**kwargs):
                kwargs['stdout'].write(b'ADDP_VOLUME_SCAN_V1_ENTERED\n1024\nADDP_VOLUME_SCAN_V1_RETURNED\n')
                kwargs['stdout'].flush();return process
            parse=m.volume_measurement_checkpoints
            def slow(raw,**kwargs):
                value=parse(raw,**kwargs)
                if raw is not None:clock[0]=4.01
                return value
            with mock.patch.object(m.time,'monotonic',side_effect=lambda:clock[0]), \
                 mock.patch.object(m.time,'monotonic_ns',side_effect=lambda:round(clock[0]*1e9)), \
                 mock.patch.object(m.subprocess,'Popen',side_effect=start), \
                 mock.patch.object(m,'volume_measurement_checkpoints',side_effect=slow):
                with self.assertRaisesRegex(m.GateError,'command_deadline_exceeded'):
                    life.command(['docker','exec',life.run_id+'-space','/bin/sh','-ec',m.VOLUME_SCAN_COMMAND],timeout=4)
            record=life.sample_step['commands'][0]
            self.assertEqual(record['result'],'failed')
            self.assertEqual(record['checkpoint_processing_seconds'],4.01)
            self.assertEqual(record['volume_measurement_checkpoints']['pipeline_returned'],'observed')

    def test_returned_marker_cannot_replace_nonzero_command_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            life.sample_thread_id=threading.get_ident();life.sample_phase='product_compile'
            life.sample_deadline=10**20;life.sample_step={'commands':[]}
            process=mock.Mock(pid=654321,returncode=7);process.poll.return_value=7
            def start(*args,**kwargs):
                kwargs['stdout'].write(b'ADDP_VOLUME_SCAN_V1_ENTERED\nPRIVATE\nADDP_VOLUME_SCAN_V1_RETURNED\n')
                kwargs['stdout'].flush();return process
            with mock.patch.object(m.subprocess,'Popen',side_effect=start):
                with self.assertRaisesRegex(m.GateError,'command_failed_7'):
                    life.command(['docker','exec',life.run_id+'-space','/bin/sh','-ec',m.VOLUME_SCAN_COMMAND],guarded=False)
            record=life.sample_step['commands'][0]
            self.assertEqual(record['volume_measurement_checkpoints']['pipeline_returned'],'observed')
            self.assertEqual(record['result'],'failed')
            self.assertEqual(life.last_failure['exit_code'],7)
            self.assertNotIn('PRIVATE',json.dumps(record))
            self.assertEqual(life.samples,[])

    def test_old_numeric_route_and_bad_framed_measurement_are_rejected(self):
        entered=m.VOLUME_SCAN_ENTERED;returned=m.VOLUME_SCAN_RETURNED
        for raw in ('1024',entered+'\n1024',entered+'\n1024\n'+returned,
                    entered+'\n1024\n'+returned+'\n'+returned+'\n',
                    entered+'\ndu: PRIVATE: denied\n1024\n'+returned+'\n'):
            with self.subTest(raw=raw),tempfile.TemporaryDirectory() as directory:
                life,_=LocalTraceResourceGuardTests().sample_lifecycle(directory,1024)
                original=life.docker
                life.docker=lambda *args,**kw:raw if args[0]=='exec' else original(*args,**kw)
                life.owned_bind_bytes=mock.Mock(side_effect=AssertionError('do not continue'))
                with mock.patch.object(m.time,'monotonic',return_value=1):life.monitor()
                self.assertEqual(life.failure,'owned_volume_measurement_invalid')
                self.assertEqual(life.samples,[])
                life.owned_bind_bytes.assert_not_called()
                self.assertNotIn('PRIVATE',json.dumps(life.resource_failure))


class LocalTraceCommandBudgetTests(unittest.TestCase):
    def setUp(self):
        clock = mock.patch.object(m.time, 'monotonic_ns', side_effect=lambda: round(m.time.monotonic()*1e9))
        clock.start()
        self.addCleanup(clock.stop)

    def wait_poll_case(self, actions, *, start_ns=0, error=None, sampling=True, spawn_error=False, command_count=1):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            life.sample_thread_id = threading.get_ident() if sampling else -1
            life.sample_phase = 'synthetic'
            life.sample_deadline = 4
            life.sample_step = {'commands': []}
            clock = [start_ns]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = None
            pending = iter(actions)
            calls = []
            def wait(*, timeout):
                calls.append(timeout)
                if timeout == 1:  # Original cancellation wait, never a poll.
                    clock[0] += 200_000_000
                    process.poll.return_value = 0
                    return 0
                outcome, duration_ns = next(pending)
                clock[0] += duration_ns
                if outcome == 'timeout':
                    raise m.subprocess.TimeoutExpired('PRIVATE-TOKEN', timeout)
                if outcome == 'error':
                    raise RuntimeError('PRIVATE-TOKEN')
                process.poll.return_value = 0
                return 0
            process.wait.side_effect = wait
            command = ['docker', 'container', 'ls', '-a', '-q', '--filter', f'label={m.LABEL}={life.run_id}']
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]/1e9), \
                 mock.patch.object(m.time, 'monotonic_ns', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', side_effect=OSError('PRIVATE-TOKEN') if spawn_error else None, return_value=process), \
                 mock.patch.object(m.os, 'killpg') as kill:
                if error is None:
                    for _ in range(command_count):
                        self.assertEqual(life.command(command, guarded=False, timeout=4), '')
                else:
                    with self.assertRaisesRegex(error[0], error[1]):
                        life.command(command, guarded=False, timeout=4)
            record = life.command_evidence.snapshot()[-1]
            return record, calls, kill.call_args_list

    def test_wait_poll_summarizes_repeated_timeouts_and_early_return(self):
        record, calls, killed = self.wait_poll_case([
            ('timeout', 70_000_000), ('timeout', 55_000_000),
            ('timeout', 40_000_000), ('success', 80_000_000)])
        summary = record['wait_poll']
        self.assertEqual(summary, {'calls': 4, 'timeouts': 3,
            'timeout_overrun_total_ns': 25_000_000, 'timeout_overrun_max_ns': 20_000_000})
        self.assertEqual(calls, [.05]*4)
        self.assertEqual(killed, [])
        self.assertLessEqual(len(json.dumps(summary).encode()), 512)
        self.assertTrue(all(type(v) is int for v in summary.values()))
        self.assertNotIn('PRIVATE', json.dumps(summary))
        # Successful observation after a long wait is not a TimeoutExpired overrun.
        self.assertEqual(record['result'], 'passed')

    def test_wait_poll_immediate_exit_and_commands_have_independent_zero_counts(self):
        slow, _, _ = self.wait_poll_case([('timeout', 60_000_000), ('success', 0)])
        fast, _, _ = self.wait_poll_case([('timeout', 60_000_000), ('success', 0), ('success', 0)], command_count=2)
        self.assertEqual(slow['wait_poll']['timeout_overrun_total_ns'], 10_000_000)
        self.assertEqual(fast['wait_poll'], {'calls': 1, 'timeouts': 0,
            'timeout_overrun_total_ns': 0, 'timeout_overrun_max_ns': 0})

    def test_wait_poll_uses_short_remaining_budget_and_excludes_cancellation(self):
        record, calls, killed = self.wait_poll_case([('timeout', 23_000_000)],
            start_ns=3_980_000_000, error=(m.GateError, '^command_deadline_exceeded$'))
        self.assertAlmostEqual(calls[0], .02)
        self.assertEqual(calls[1:], [1])
        self.assertEqual(record['wait_poll'], {'calls': 1, 'timeouts': 1,
            'timeout_overrun_total_ns': 3_000_000, 'timeout_overrun_max_ns': 3_000_000})
        self.assertEqual(record['result'], 'failed')
        self.assertEqual(len(killed), 1)
        self.assertGreater(record['cancel_cleanup_seconds'], 0)

    def test_wait_poll_exception_retains_first_error_and_counts_only_original_poll(self):
        record, calls, killed = self.wait_poll_case([('error', 5_000_000)],
            error=(RuntimeError, '^PRIVATE-TOKEN$'))
        self.assertEqual(record['wait_poll'], {'calls': 1, 'timeouts': 0,
            'timeout_overrun_total_ns': 0, 'timeout_overrun_max_ns': 0})
        self.assertEqual(calls, [.05, 1])
        self.assertEqual(record['result'], 'failed')
        self.assertEqual(len(killed), 1)
        self.assertNotIn('PRIVATE', json.dumps(record))

    def test_wait_poll_spawn_failure_is_zero_and_non_sampling_has_no_summary(self):
        record, calls, killed = self.wait_poll_case([], spawn_error=True,
            error=(OSError, '^PRIVATE-TOKEN$'))
        self.assertEqual(record['wait_poll'], {'calls': 0, 'timeouts': 0,
            'timeout_overrun_total_ns': 0, 'timeout_overrun_max_ns': 0})
        self.assertEqual(calls, [])
        self.assertEqual(killed, [])
        record, _, _ = self.wait_poll_case([('success', 0)], sampling=False)
        self.assertNotIn('wait_poll', record)

    def test_other_threads_do_not_inherit_observer_deadline_or_diagnostics(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            life.sample_thread_id = -1
            life.sample_deadline = 0
            life.sample_step = {'commands': []}
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            with mock.patch.object(m.time, 'monotonic', return_value=1), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process):
                self.assertEqual(life.command(['synthetic-product-command']), '')
            self.assertEqual(life.sample_step['commands'], [])

    def test_process_creation_counts_towards_command_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            def start(*args, **kwargs): clock[0] = 4.01; return process
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                with self.assertRaisesRegex(m.GateError, 'command_deadline_exceeded'):
                    life.command(['synthetic-owned-command'], timeout=4)

    def test_output_processing_cannot_pass_after_deadline(self):
        with tempfile.TemporaryDirectory() as directory, tempfile.TemporaryFile() as output:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            wrapped = mock.Mock(wraps=output)
            def read(): clock[0] = 4.01; return output.read()
            wrapped.read.side_effect = read
            context = mock.MagicMock()
            context.__enter__.return_value = wrapped
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), \
                 mock.patch.object(m.tempfile, 'TemporaryFile', return_value=context):
                with self.assertRaisesRegex(m.GateError, 'command_deadline_exceeded'):
                    life.command(['synthetic-owned-command'], timeout=4)

    def test_wait_returns_on_child_exit_without_fixed_sleep(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = None
            def wait(**kwargs):
                self.assertGreater(kwargs['timeout'], 0)
                self.assertLessEqual(kwargs['timeout'], .05)
                clock[0] += .004
                process.poll.return_value = 0
            process.wait.side_effect = wait
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), \
                 mock.patch.object(m.time, 'sleep', side_effect=AssertionError('fixed sleep forbidden')):
                self.assertEqual(life.command(['synthetic-owned-command']), '')
            process.wait.assert_called_once()
            self.assertEqual(clock[0], .004)

    def test_guard_failure_interrupts_wait_and_reaps_child(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = None
            calls = []
            def wait(**kwargs):
                calls.append(kwargs['timeout'])
                if len(calls) == 1:
                    life.failure = 'owned_volume_stop_line_reached'
                    raise m.subprocess.TimeoutExpired('private', kwargs['timeout'])
                process.poll.return_value = 0
            process.wait.side_effect = wait
            with mock.patch.object(m.time, 'monotonic', return_value=1), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), \
                 mock.patch.object(m.time, 'sleep', side_effect=AssertionError('fixed sleep forbidden')), \
                 mock.patch.object(m.os, 'killpg') as kill:
                with self.assertRaisesRegex(m.GateError, 'owned_volume_stop_line_reached'):
                    life.command(['synthetic-owned-command'],process_observer=lambda child:setattr(life,'observed_child',child))
            self.assertIs(life.observed_child,process)
            self.assertEqual(process.poll.return_value,0)
            self.assertLessEqual(calls[0], .05)
            kill.assert_called_once_with(process.pid, signal.SIGTERM)
            self.assertEqual(calls[1], 1)

    def test_sample_command_timings_never_expose_command_or_environment(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            life.env = {'PRIVATE-ENV': 'PRIVATE-TOKEN'}
            life.owned = lambda kind, **kw: life.command(['PRIVATE-COMMAND'], guarded=False, **kw).split()
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = 0
            def start(*args, **kwargs): clock[0] += .01; return process
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                life.sample()
            commands = [command for step in life.samples[-1]['steps'] for command in step['commands']]
            self.assertEqual(len(commands), 2)
            self.assertTrue(all(command['spawn_seconds'] == .01 for command in commands))
            self.assertNotIn('PRIVATE', json.dumps(life.samples))

    def test_timeout_keeps_spawn_wait_and_cleanup_costs_without_success_sample(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            clock = [0.0]
            process = mock.Mock(pid=654321, returncode=0)
            process.poll.return_value = None
            def start(*args, **kwargs): clock[0] = .01; return process
            def wait(**kwargs):
                if kwargs['timeout'] <= .05:
                    clock[0] = 4
                    raise m.subprocess.TimeoutExpired('PRIVATE-COMMAND', kwargs['timeout'])
                clock[0] += .2
                process.poll.return_value = 0
            process.wait.side_effect = wait
            with mock.patch.object(m.time, 'monotonic', side_effect=lambda: clock[0]), \
                 mock.patch.object(m.subprocess, 'Popen', side_effect=start), \
                 mock.patch.object(m.os, 'killpg') as kill:
                life.monitor()
            failure = life.resource_failure
            self.assertEqual(failure['error_code'], 'resource_sample_deadline_exceeded')
            command = failure['steps'][-1]['commands'][-1]
            self.assertEqual(command['result'], 'failed')
            self.assertEqual(command['spawn_seconds'], .01)
            self.assertEqual(command['wait_seconds'], 3.99)
            self.assertEqual(command['wait_poll'], {'calls': 1, 'timeouts': 1,
                'timeout_overrun_total_ns': 3_940_000_000, 'timeout_overrun_max_ns': 3_940_000_000})
            self.assertEqual(command['cancel_cleanup_seconds'], .2)
            self.assertEqual(command['entered_monotonic_ns'],0)
            self.assertEqual(command['cancel_started_monotonic_ns'],4_000_000_000)
            self.assertEqual(command['cancel_finished_monotonic_ns'],4_200_000_000)
            self.assertEqual(command['exit_observed_monotonic_ns'],4_200_000_000)
            self.assertEqual(command['decision_monotonic_ns'],4_200_000_000)
            self.assertEqual(command['finished_monotonic_ns'],4_200_000_000)
            self.assertEqual(failure['sample_duration_seconds'], 4.2)
            self.assertEqual(failure['steps'][-1]['processing_seconds'], 0)
            self.assertNotIn('PRIVATE', json.dumps(failure))
            self.assertEqual(life.samples, [])
            kill.assert_called_once_with(process.pid, signal.SIGTERM)


class LocalTraceCleanupTests(unittest.TestCase):
    def setUp(self):
        clock = mock.patch.object(m.time, 'monotonic_ns', side_effect=lambda: round(m.time.monotonic()*1e9))
        clock.start()
        self.addCleanup(clock.stop)

    def test_sampler_mount_is_released_before_builder_state_volume_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            work=Path(directory)/'owned';work.mkdir()
            life=bare_lifecycle(work)
            alive=True
            def docker(*args,**kwargs):
                nonlocal alive
                if args[0]=='ps':
                    return 'owned-sampler' if alive and 'name=^'+life.run_id+'-space$' in args else ''
                if args[:2]==('rm','-fv'):
                    self.assertEqual(args[2:],('owned-sampler',));alive=False;return ''
                if args[:2]==('buildx','rm'):
                    self.assertFalse(alive,'sampler still pins builder state volume');return ''
                if args[0] in ('container','network','volume') and args[1]=='ls':return ''
                raise AssertionError(args)
            life.docker=docker
            self.assertEqual(life.cleanup(),[])
            self.assertFalse(alive)

    def test_removal_is_filtered_and_zero_is_verified(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            resources = {'container': ['owned-container'], 'network': ['owned-network'], 'volume': ['owned-volume']}
            calls = []
            def docker(*args, **kwargs):
                calls.append((args, kwargs))
                if len(args) > 1 and args[1] == 'ls' and args[0] in resources:
                    self.assertIn('label=' + m.LABEL + '=' + life.run_id, args)
                    return '\n'.join(resources[args[0]])
                if args[0] in resources and args[1] == 'rm':
                    self.assertTrue(set(args[2:]) - {'-fv'} <= set(resources[args[0]]))
                    resources[args[0]] = []
                    return ''
                if args[:2] == ('buildx', 'rm') or args[0] == 'ps':
                    return ''
                raise AssertionError(args)
            life.docker = docker
            self.assertEqual(life.cleanup(), [])
            self.assertFalse(work.exists())
            self.assertTrue(all(not values for values in resources.values()))
            self.assertTrue(all(not kwargs.get('guarded', True) for _, kwargs in calls))

    def test_cleanup_failure_does_not_prevent_remaining_owned_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            life.builder_ids = {'owned-builder'}
            life.builder_created = True
            resources = {'container': ['owned-container'], 'network': ['owned-network'], 'volume': ['owned-volume']}
            removed = []
            def docker(*args, **kwargs):
                if args[0] == 'ps':
                    return ''
                if args[:2] == ('buildx', 'rm'):
                    raise m.GateError('synthetic_builder_refusal')
                if args[0] in resources and args[1] == 'ls':
                    return '\n'.join(resources[args[0]])
                if args[0] in resources and args[1] == 'rm':
                    if args[0] == 'container':
                        raise m.GateError('synthetic_owned_removal_refusal')
                    resources[args[0]] = []
                    removed.append(args[0])
                    return ''
                raise AssertionError(args)
            life.docker = docker
            errors = life.cleanup()
            self.assertIn('builder', errors)
            self.assertIn('container', errors)
            self.assertEqual(removed, ['network', 'volume'])
            self.assertFalse(work.exists())

    def test_failed_command_exposes_only_exit_hash_and_fixed_diagnostic_markers(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            process = mock.Mock(pid=654321, returncode=17)
            process.poll.return_value = 17
            def start(*args, **kwargs):
                kwargs['stdout'].write(b'permission denied password=PRIVATE-SENTINEL token=PRIVATE-TOKEN')
                return process
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                with self.assertRaisesRegex(m.GateError, '^command_failed_17$'):
                    life.command(['synthetic-private-command'])
            self.assertEqual(set(life.last_failure), {'exit_code', 'output_sha256','diagnostic_markers'})
            self.assertEqual(life.last_failure['exit_code'], 17)
            self.assertEqual(life.last_failure['diagnostic_markers'],['permission denied'])
            self.assertRegex(life.last_failure['output_sha256'], r'^[a-f0-9]{64}$')
            self.assertNotIn('PRIVATE', repr(life.last_failure))

    def test_build_failure_classification_excludes_arbitrary_output(self):
        cases = [
            (b'failed to compute cache key: failed to calculate checksum: PRIVATE-PATH not found', ['not found', 'failed to compute cache key', 'failed to calculate checksum']),
            (b'failed to solve: content digest sha256:PRIVATE-TOKEN: not found', ['failed to solve', 'not found', 'content digest']),
            (b'failed to solve: failed to fetch anonymous token: unexpected status from HEAD request: PRIVATE-TOKEN 404 Not Found', ['failed to solve', 'not found', 'failed to fetch anonymous token', 'unexpected status from head request']),
        ]
        for raw, expected in cases:
            with self.subTest(raw=raw):
                markers = m.diagnostic_markers(raw)
                self.assertEqual(markers, expected)
                self.assertNotIn('PRIVATE', repr(markers))

    def test_public_build_diagnostic_distinguishes_missing_input_from_content(self):
        missing = m.public_build_failure_diagnostic(b'#7 ERROR: failed to calculate checksum: "/dist/release-linux-arm64/gateway": not found PRIVATE-TOKEN\n > [2/3] COPY dist/release-linux-arm64/gateway ./server:\nDockerfile.prebuilt:14')
        self.assertEqual(missing['reasons'], ['context_input_missing'])
        self.assertEqual(missing['steps'], ['copy'])
        self.assertEqual(missing['inputs'], ['dist/release-linux-arm64/gateway'])
        self.assertEqual(missing['dockerfile_lines'], [14])
        content = m.public_build_failure_diagnostic(('ERROR: failed to solve: content digest sha256:' + 'a' * 64 + ': not found PRIVATE-TOKEN\n > exporting to image:').encode())
        self.assertEqual(content['reasons'], ['content_digest_missing'])
        self.assertEqual(content['steps'], ['export'])
        self.assertNotIn('PRIVATE', repr(missing) + repr(content))
        self.assertNotIn('a' * 64, repr(content))

    def test_public_build_diagnostic_retains_bounded_codes_without_bodies(self):
        value = m.public_build_failure_diagnostic(b'ERROR: unexpected status from HEAD request: https://PRIVATE-TOKEN/repo: 404 Not Found\n > [internal] load metadata for localhost:5001/debian-slim:bookworm:\nDockerfile.prebuilt:1\nSQL=PRIVATE-SQL password=PRIVATE-PASSWORD')
        self.assertEqual(value['reasons'], ['registry_http_status'])
        self.assertEqual(value['steps'], ['base_metadata'])
        self.assertEqual(value['http_statuses'], [404])
        self.assertNotIn('PRIVATE', repr(value))
        self.assertNotIn('https://', repr(value))
        unknown = m.public_build_failure_diagnostic(b'ERROR: PRIVATE-TOKEN /private/password SQL=SECRET')
        self.assertEqual(unknown['status'],'complete')
        self.assertEqual(unknown['reasons'],['unclassified'])
        self.assertTrue(all(unknown[k]==[] for k in ('steps','inputs','dockerfile_lines','http_statuses','process_exit_codes')))
        self.assertEqual(unknown['apt']['status'],'unavailable');self.assertEqual(unknown['base']['status'],'unavailable')
        self.assertNotIn('PRIVATE',repr(unknown));self.assertNotIn('SECRET',repr(unknown))

    def test_failed_product_image_command_adds_only_structured_public_diagnostic(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory); life.phase = 'product_image_gateway'
            process = mock.Mock(pid=654321, returncode=17); process.poll.return_value = 17
            def start(*args, **kwargs):
                kwargs['stdout'].write(b'ERROR: content digest sha256:' + b'a' * 64 + b': not found PRIVATE-TOKEN')
                return process
            with mock.patch.object(m.time, 'monotonic', return_value=1), mock.patch.object(m.subprocess, 'Popen', side_effect=start):
                with self.assertRaisesRegex(m.GateError, '^command_failed_17$'):
                    life.command(['make', 'build-images'])
            self.assertEqual(life.last_failure['public_build']['reasons'], ['content_digest_missing'])
            self.assertNotIn('PRIVATE', repr(life.last_failure))

    def test_public_build_failure_diagnostics_remain_fixed_markers(self):
        markers = m.diagnostic_markers(b'ERROR: failed to solve: proxyconnect tcp PRIVATE-TOKEN: connection refused')
        self.assertEqual(markers, ['failed to solve', 'connection refused', 'proxyconnect'])
        self.assertNotIn('PRIVATE', repr(markers))

    def test_invalid_report_admission_removes_private_work_before_any_docker_call(self):
        with tempfile.TemporaryDirectory() as directory:
            root = (Path(directory) / 'repository').resolve(); root.mkdir()
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            life.docker = mock.Mock(side_effect=AssertionError('Docker must not run'))
            with mock.patch.object(m, 'ROOT', root), mock.patch.object(m, 'Lifecycle', return_value=life), \
                 mock.patch.dict(m.os.environ, {'MONITOR_TRACE_LOCAL_REPORT': str(root / 'forbidden.json')}, clear=True), \
                 mock.patch.object(m.sys, 'argv', ['monitor-traces-local-gate.py']):
                with self.assertRaises(m.GateError):
                    m.main()
            life.docker.assert_not_called()
            self.assertFalse(work.exists())
            self.assertFalse((root / 'forbidden.json').exists())

    def test_unstopped_guard_is_a_cleanup_failure(self):
        with tempfile.TemporaryDirectory() as directory:
            work = Path(directory) / 'private'; work.mkdir()
            life = bare_lifecycle(work)
            life.thread = mock.Mock()
            life.thread.is_alive.return_value = True
            life.docker = mock.Mock(return_value='')
            self.assertIn('resource_guard_not_stopped', life.cleanup())
            life.thread.join.assert_called_once()

    def test_command_interruption_reaps_its_process_group(self):
        with tempfile.TemporaryDirectory() as directory:
            life = bare_lifecycle(directory)
            process = mock.Mock(pid=654321)
            process.poll.return_value = None
            process.wait.side_effect = [KeyboardInterrupt, m.subprocess.TimeoutExpired('owned', 1), 0]
            with mock.patch.object(m.time, 'monotonic', return_value=1), \
                 mock.patch.object(m.subprocess, 'Popen', return_value=process), mock.patch.object(m.os, 'killpg') as kill:
                with self.assertRaises(KeyboardInterrupt):
                    life.command(['synthetic-owned-command'])
            self.assertEqual(kill.call_args_list, [mock.call(process.pid, signal.SIGTERM), mock.call(process.pid, signal.SIGKILL)])
            self.assertEqual(process.wait.call_count, 3)


if __name__ == '__main__':
    unittest.main()


class LocalTraceCommittedSourceTests(unittest.TestCase):
    def test_private_input_drift_fails_before_clone_or_build(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'owned.py').write_text('committed source')
            life=mock.Mock();life.command.return_value='committed source'
            with mock.patch.object(m,'ROOT',root),mock.patch.object(m,'OWN_INPUTS',('owned.py',)):
                result=m.committed_inputs(life,'fixed-commit')
                self.assertRegex(result['owned.py'],r'^[0-9a-f]{64}$')
                (root/'owned.py').write_text('uncommitted source')
                with self.assertRaisesRegex(m.GateError,'commit_all_local_inputs'):m.committed_inputs(life,'fixed-commit')
            self.assertTrue(all(call.args[0][0:2]==['git','show'] for call in life.command.call_args_list))
    def test_shared_wip_is_not_built_and_missing_committed_integration_is_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'Makefile').write_text('other session WIP')
            life=mock.Mock();life.command.return_value='test-monitor-traces-local: committed-entry'
            with mock.patch.object(m,'ROOT',root),mock.patch.object(m,'OWN_INPUTS',('Makefile',)):
                self.assertIn('Makefile',m.committed_inputs(life,'fixed-commit'))
                life.command.return_value='old unrelated entry'
                with self.assertRaisesRegex(m.GateError,'committed_local_integration_missing'):m.committed_inputs(life,'fixed-commit')


class LocalTracePrivateFilesTests(unittest.TestCase):
    def test_private_json_is_exclusive_and_owner_only(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            path=life.private_json('input.json',{'safe':'value'})
            self.assertEqual(path.stat().st_mode&0o777,0o600)
            self.assertEqual(path.stat().st_nlink,1)
            self.assertEqual(json.loads(path.read_text()),{'safe':'value'})
            with self.assertRaises(FileExistsError):life.private_json('input.json',{})
    def test_public_docker_client_never_loads_personal_credential_helpers(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory);life.env={'DOCKER_CONTEXT':'personal','HOME':'/personal'}
            path=m.private_docker_client(life,'unix:///local/socket')
            self.assertEqual(path.stat().st_mode&0o777,0o700)
            config=json.loads((path/'config.json').read_text())
            self.assertEqual(config['auths'],{})
            self.assertNotIn('credsStore',config);self.assertNotIn('credHelpers',config)
            self.assertEqual((path/'config.json').stat().st_mode&0o777,0o600)
            self.assertEqual(life.env['DOCKER_HOST'],'unix:///local/socket')
            self.assertEqual(life.env['DOCKER_CONFIG'],str(path))
            self.assertNotIn('DOCKER_CONTEXT',life.env)


class LocalTraceStartupAndWritableBudgetTests(unittest.TestCase):
    def test_init_commands_finish_successfully_before_long_running_consumers(self):
        calls=[]
        def compose(*args,**kwargs):
            if args[0]=='exec':
                self.assertEqual(kwargs,{'data':'standard-initialization-sql'})
                self.assertIn('ON_ERROR_STOP=1',args[-1])
                self.assertIn('$POSTGRES_DB',args[-1])
            if args[0]=='up':
                self.assertIn('--wait',args)
                self.assertFalse({'runtime-log-store-init','trace-store-init'}.intersection(args))
            calls.append(args)
        m.start_dependencies(compose,'standard-initialization-sql')
        m.start_deployment(compose)
        self.assertEqual(calls[2:4],[('run','--no-deps','runtime-log-store-init'),('run','--no-deps','trace-store-init')])
        self.assertTrue(all(name in calls[0] for name in ('postgres','business-postgres','redis','minio')))
        self.assertEqual(calls[1][:3],('exec','-T','postgres'))
        self.assertIn('alloy',calls[4])
        self.assertIn('system-backend',calls[5])
        self.assertFalse(any(set(call).intersection(m.TRACE+('postgres','business-postgres','redis')) for call in calls[5:]))
    def test_database_initialization_failure_stops_all_product_startup(self):
        calls=[]
        def compose(*args,**kwargs):
            calls.append(args)
            if args[0]=='exec':raise m.GateError('initialization_rejected')
        with self.assertRaisesRegex(m.GateError,'initialization_rejected'):
            m.start_dependencies(compose,'standard-initialization-sql')
        self.assertEqual(len(calls),2)

    def test_private_writable_directories_are_part_of_the_same_stop_line(self):
        with tempfile.TemporaryDirectory() as directory:
            life=bare_lifecycle(directory)
            life.owned=lambda kind,**kw: []
            life.owned_bind_bytes=lambda **kw: m.VOLUME_STOP
            with self.assertRaisesRegex(m.GateError,'owned_volume_stop_line_reached'):life.sample()
            life.command=mock.Mock(return_value='2048 /private/work\n')
            self.assertEqual(m.Lifecycle.owned_bind_bytes(life,timeout=1),2*1024**2)
            self.assertEqual(life.command.call_args.args[0],['du','-sk',str(life.work)])
