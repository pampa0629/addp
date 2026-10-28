import copy
import importlib
import json
import os
import subprocess
import shutil
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

FIXTURE = importlib.import_module("scripts.test.platform-node-metrics-fixture")


class MetricsFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="addp-metrics-fixture-test-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.secret = self.root / "addp-online-secret-test"
        self.secret.mkdir(mode=0o700)
        self.env = dict(GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1", ADDP_ONLINE_HOST="1",
                        ONLINE_SUITE="platform-node-metrics", POSTGRES_DB="addp_online",
                        ADDP_ONLINE_SECRET_DIR=str(self.secret), ADDP_ONLINE_ARTIFACT_DIR=str(self.root / "artifacts"))

    def native_exporter(self):
        return {"Id": "a"*64, "State": {"Running": True},
                "Config": {"User": "65534:65534", "Env": ["SECRET=not-for-artifacts"], "Labels": {
                    "com.docker.compose.project": "addp-node-metrics", "com.docker.compose.service": "node-exporter",
                    "io.addp.node-metrics.owner": str(FIXTURE.ROOT)},
                    "Cmd": ["--path.rootfs=/host", "--path.procfs=/host/proc", "--path.sysfs=/host/sys",
                            "--collector.disable-defaults", "--collector.filesystem"]},
                "HostConfig": {"NetworkMode": "host", "PidMode": "host", "ReadonlyRootfs": True,
                               "Privileged": False, "CapDrop": ["ALL"]},
                "Mounts": [{"Type": "bind", "Source": "/", "Destination": "/host", "RW": False, "Propagation": "rslave"}]}

    def prove_native(self, row=None, info=None, ids=None):
        info = info if info is not None else {"OSType": "linux", "KernelVersion": "test-kernel", "OperatingSystem": "Ubuntu"}
        with patch.object(FIXTURE, "boundary"), patch.object(FIXTURE.platform, "system", return_value="Linux"), \
             patch.object(FIXTURE.platform, "release", return_value="test-kernel"), \
             patch.object(FIXTURE, "command", side_effect=[json.dumps(info), ids if ids is not None else "a"*64,
                                                         json.dumps([row if row is not None else self.native_exporter()])]) as command:
            proof = FIXTURE.native_node_source_proof()
            self.assertTrue(all(call.kwargs == {"timeout": 5} for call in command.call_args_list))
            self.assertEqual(command.call_count, 3)
            return proof

    def test_native_source_proof_is_closed_and_does_not_export_docker_secrets(self):
        proof = self.prove_native()
        self.assertEqual(set(proof), {"schema_version", "native_linux", "source_id_sha256", "host_network_pid",
                                     "readonly_host_root", "host_collector_paths", "filesystem_collector", "nonprivileged_exporter"})
        self.assertEqual(proof["schema_version"], "addp.native-node-source/v1")
        self.assertRegex(proof["source_id_sha256"], r"^[0-9a-f]{64}$")
        self.assertTrue(all(value is True for key, value in proof.items() if key not in ("schema_version", "source_id_sha256")))
        self.assertNotIn("not-for-artifacts", json.dumps(proof))
        self.assertNotIn(str(FIXTURE.ROOT), json.dumps(proof))

    def test_native_source_proof_rejects_non_host_sources_and_changed_ownership(self):
        for info in ({"OSType": "windows"}, {"OSType": "linux", "KernelVersion": "other"},
                     {"OSType": "linux", "KernelVersion": "test-kernel", "OperatingSystem": "Docker Desktop"}):
            with self.subTest(info=info), self.assertRaises(ValueError): self.prove_native(info=info)
        for ids in ("", "a"*64+"\n"+"b"*64, "short-id"):
            with self.subTest(ids=ids), self.assertRaises(ValueError): self.prove_native(ids=ids)
        changes = [lambda r: r.update(Id="b"*64), lambda r: r["State"].update(Running=False),
                   lambda r: r["Config"]["Labels"].update({"io.addp.node-metrics.owner": "foreign"}),
                   lambda r: r["Config"]["Labels"].update({"com.docker.compose.service": "other"}),
                   lambda r: r["Config"].update(User="root")]
        changes += [lambda r, key=key, value=value: r["HostConfig"].update({key: value}) for key, value in
                    (("NetworkMode", "bridge"), ("PidMode", ""), ("ReadonlyRootfs", False), ("Privileged", True), ("CapDrop", []))]
        changes += [lambda r, key=key, value=value: r["Mounts"][0].update({key: value}) for key, value in
                    (("Type", "volume"), ("Source", "/container"), ("RW", True), ("Propagation", "rprivate"))]
        changes += [lambda r: r.update(Mounts=[]), lambda r: r["Mounts"].append(copy.deepcopy(r["Mounts"][0])),
                    lambda r: r["Mounts"].append({"Source": "/proc", "Destination": "/host/proc"}),
                    lambda r: r["Config"]["Cmd"].append("--path.rootfs=/"),
                    lambda r: r["Config"]["Cmd"].extend(["--path.rootfs", "/"]),
                    lambda r: r["Config"]["Cmd"].remove("--path.procfs=/host/proc"),
                    lambda r: r["Config"]["Cmd"].remove("--path.sysfs=/host/sys"),
                    lambda r: r["Config"]["Cmd"].remove("--collector.filesystem"),
                    lambda r: r["Config"]["Cmd"].append("--no-collector.filesystem"),
                    lambda r: r["Config"]["Cmd"].append("--collector.filesystem=false"),
                    lambda r: r["Config"]["Cmd"].append("--collector.disable-defaults=false"),
                    lambda r: r["Config"]["Cmd"].append("--collector.textfile.directory=/fake")]
        for index, change in enumerate(changes):
            row = self.native_exporter(); change(row)
            with self.subTest(change=index), self.assertRaises(ValueError): self.prove_native(row=row)
        with patch.object(FIXTURE, "boundary", side_effect=ValueError("personal")), patch.object(FIXTURE, "command") as command:
            with self.assertRaises(ValueError): FIXTURE.native_node_source_proof()
            command.assert_not_called()

    def check_worker_sources(self, case):
        original = FIXTURE.command
        def command(args):
            if args[:4] == ["docker", "network", "inspect", "bridge"]:
                return '[{"IPAM":{"Config":[{"Gateway":"172.17.0.1"}]}}]'
            return original(args)
        with patch.dict(os.environ, dict(self.env, ADDP_ONLINE_NODE_METRICS_CASE=case)), patch.object(FIXTURE, "command", side_effect=command), patch.object(FIXTURE.socket, "socket"):
            FIXTURE.prepare(self.secret)
        import shlex
        values = {key: shlex.split(value)[0] for key, value in (line.removeprefix('export ').split('=',1) for line in (self.secret/'metrics.env').read_text().splitlines())}
        profile = FIXTURE.WORKER_PROFILES[case]
        for key, value in profile['env'].items():
            self.assertEqual(values[key], value)
        plan = json.loads(values['ADDP_DEV_PROCESS_METRICS_PLAN'])
        self.assertEqual(set(plan), {'system','monitor',*profile['ports']})
        self.assertEqual(values['MONITOR_METRICS_ALLOWED_PORTS'], ','.join(map(str, [19100,19101,19102,*profile['ports'].values()])))
        for unit in profile['ports']:
            if unit == profile['module']: continue
            self.assertEqual(plan[unit]['module_name'], profile['module'])
            self.assertEqual(plan[unit]['role'], 'worker')
            folder = Path(plan[unit]['tls_dir'])
            self.assertEqual(folder, self.secret/('process-'+unit))
            self.assertEqual({p.name for p in folder.iterdir()}, {'ca.crt','server.crt','server.key'})
            subprocess.run(['openssl','verify','-verify_ip','172.17.0.1','-purpose','sslserver','-CAfile',str(folder/'ca.crt'),str(folder/'server.crt')], check=True, capture_output=True)
        for key in ('listen','endpoint','tls_dir'):
            self.assertEqual(len({item[key] for item in plan.values()}), len(plan))

    def test_meta_worker_sources_have_distinct_tls_ports_and_one_deployment_per_unit(self):
        self.check_worker_sources('meta-workers')

    def test_quality_worker_sources(self):
        self.check_worker_sources('quality-workers')

    def test_transfer_worker_sources_have_distinct_tls_ports_and_one_deployment_per_unit(self):
        self.check_worker_sources('transfer-workers')

    def test_boundary_refuses_personal_hosts_databases_and_public_secrets(self):
        with patch.dict(os.environ, self.env, clear=True), patch.object(FIXTURE, "ROOT", self.root / "repository"), patch.object(FIXTURE.platform, "system", return_value="Linux"), patch.object(FIXTURE.platform, "machine", return_value="x86_64"):
            self.assertEqual(FIXTURE.boundary(), self.secret)
            for key, value in (("GITHUB_ACTIONS", "false"), ("RUNNER_OS", "macOS"), ("POSTGRES_DB", "addp_test"), ("ONLINE_SUITE", "other"), ("ADDP_ONLINE_HOSTED", "0")):
                with self.subTest(key=key), patch.dict(os.environ, {key: value}), self.assertRaises(ValueError):
                    FIXTURE.boundary()
            self.secret.chmod(0o755)
            with self.assertRaises(ValueError):
                FIXTURE.boundary()

    def test_prepare_uses_production_center_and_distinct_real_tls_identities(self):
        calls = []
        original = FIXTURE.command
        def command(args):
            calls.append(args)
            if args[:4] == ["docker", "network", "inspect", "bridge"]:
                return '[{"IPAM":{"Config":[{"Gateway":"172.17.0.1"}]}}]'
            return original(args)
        with patch.dict(os.environ, self.env), patch.object(FIXTURE, "command", side_effect=command), patch.object(FIXTURE.socket, "socket"):
            FIXTURE.prepare(self.secret)
        self.assertFalse((self.secret / "postgres-password").exists())
        spec = json.loads((self.secret / "metrics-compose.json").read_text())
        self.assertEqual(set(spec["services"]), {"control-tls"})
        templates = [args[args.index("-f") + 1] for args in calls if args[:2] == ["docker", "compose"] and "config" in args]
        self.assertEqual(templates, [str(FIXTURE.ROOT / "scripts/infra/metrics-control.yml")])
        self.assertNotIn("restart", spec["services"]["control-tls"])
        self.assertNotIn("volumes", spec)
        self.assertEqual(spec["services"]["control-tls"]["network_mode"], "host")
        nginx = (self.secret / "nginx.conf").read_text()
        for module in ("client_body", "proxy", "fastcgi", "uwsgi", "scgi"):
            self.assertIn(module+"_temp_path /tmp/", nginx)
        config = (self.secret / "deployment/prometheus.yml").read_text()
        self.assertNotIn("@@", config)
        self.assertIn("https://172.17.0.1:9444/api/v1/monitor/platform/metrics_discovery", config)
        self.assertEqual({p.name for p in (self.secret / "node-tls").iterdir()}, {"ca.crt", "server.crt", "server.key"})
        for module in FIXTURE.PROCESS_PORTS:
            folder = self.secret / ("process-"+module)
            self.assertEqual({p.name for p in folder.iterdir()}, {"ca.crt", "server.crt", "server.key"})
            subprocess.run(["openssl", "verify", "-verify_ip", "172.17.0.1", "-purpose", "sslserver",
                            "-CAfile", str(self.secret / "source-ca.crt"), str(folder / "server.crt")], check=True, capture_output=True)
            self.assertFalse(any(str(folder) == volume["source"] for volume in spec["services"]["control-tls"]["volumes"]))
        self.assertEqual((self.secret / "query/client.key").stat().st_mode & 0o777, 0o600)
        self.assertEqual((self.secret / "query").stat().st_mode & 0o777, 0o700)
        self.assertNotEqual((self.secret / "query/client.crt").read_bytes(), (self.secret / "center-tls/health.crt").read_bytes())
        self.assertFalse(any("/query" in volume["source"] for volume in spec["services"]["control-tls"]["volumes"]))
        self.assertNotEqual((self.secret / "center-tls/health.crt").read_bytes(), (self.secret / "deployment/collector.crt").read_bytes())
        self.assertNotEqual((self.secret / "control-ca.crt").read_bytes(), (self.secret / "source-ca.crt").read_bytes())
        for cert, ca in (("center-tls/health.crt", "center-ca.crt"), ("node-tls/server.crt", "source-ca.crt"), ("deployment/collector.crt", "source-ca.crt")):
            subprocess.run(["openssl", "verify", "-CAfile", str(self.secret / ca), str(self.secret / cert)], check=True, capture_output=True)
        env = (self.secret / "metrics.env").read_text()
        self.assertEqual((self.secret / "metrics.env").stat().st_mode & 0o777, 0o600)
        self.assertIn(env, (self.secret / "runtime.env").read_text())
        import shlex
        values = dict(line.removeprefix("export ").split("=", 1) for line in env.splitlines())
        self.assertEqual(shlex.split(values["MONITOR_WEBHOOK_ALLOW_PRIVATE_NETWORKS"]), ["true"])
        self.assertEqual(shlex.split(values["SSL_CERT_FILE"]), [str(self.secret / "center-tls/ca.crt")])
        subprocess.run(["openssl", "verify", "-verify_ip", "127.0.0.1", "-purpose", "sslserver",
                        "-CAfile", str(self.secret / "center-tls/ca.crt"), str(self.secret / "center-tls/server.crt")], check=True, capture_output=True)
        self.assertNotIn("ADDP_PROCESS_METRICS_DEPLOYMENTS", values)
        plan = json.loads(shlex.split(values["ADDP_DEV_PROCESS_METRICS_PLAN"])[0])
        self.assertEqual(list(plan), ["system", "monitor"])
        deployments = list(plan.values())
        self.assertEqual(shlex.split(values["MONITOR_METRICS_ALLOWED_PORTS"]), ["19100,19101,19102"])
        for d in deployments:
            self.assertEqual(d["role"], "backend")
            self.assertEqual(d["listen"], "172.17.0.1:"+str(FIXTURE.PROCESS_PORTS[d["module_name"]]))
            self.assertEqual(d["endpoint"], "https://"+d["listen"]+"/metrics")
            self.assertEqual(Path(d["tls_dir"]), self.secret / ("process-"+d["module_name"]))
        # Exercise real HTTPS with the same private CA, then verify listener shutdown on failure.
        import ssl
        import urllib.error
        import urllib.request
        online = importlib.import_module("scripts.test.platform-node-metrics-online")
        receiver = online.ResourceAlertReceiver(self.secret, {}, {}, {})
        with patch.object(online.FIXTURE, "boundary", return_value=self.secret):
            with self.assertRaisesRegex(online.SuiteError, "original receiver failure"):
                with receiver:
                    context = ssl.create_default_context(cafile=str(self.secret / "center-tls/ca.crt"))
                    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPSHandler(context=context))
                    request = urllib.request.Request(receiver.url, data=b"{}", headers={"Content-Type": "application/json"})
                    with self.assertRaises(urllib.error.HTTPError) as failure:
                        opener.open(request, timeout=5)
                    self.assertEqual(failure.exception.code, 400)
                    failure.exception.close()
                    raise online.SuiteError("original receiver failure")
        self.assertTrue(receiver.closed)
        self.assertEqual(receiver.invalid, 1)
        self.assertFalse(receiver.thread.is_alive())
        self.assertEqual(receiver.server.socket.fileno(), -1)
        self.assertFalse(any("up" in args or "run" in args for args in calls))
        with patch.object(FIXTURE, "command") as command:
            with self.assertRaises(ValueError):
                FIXTURE.prepare(self.secret)
            command.assert_not_called()

    def test_query_origin_uses_verified_center_mapping_and_refuses_foreign_or_public_ports(self):
        (self.secret / "query").mkdir(mode=0o700)
        labels = {"com.docker.compose.project": "addp-infra", "com.docker.compose.service": "prometheus",
                  "com.docker.compose.project.working_dir": str(FIXTURE.ROOT)}
        inspect = json.dumps([{"Config": {"Labels": labels}}])
        with patch.object(FIXTURE, "command", side_effect=["center-id", inspect, "127.0.0.1:29990"]):
            FIXTURE.configure_query(self.secret)
        for name in ("metrics.env", "runtime.env"):
            self.assertIn("https://127.0.0.1:29990", (self.secret / name).read_text())
            self.assertEqual((self.secret / name).stat().st_mode & 0o777, 0o600)
        (self.secret / "query/connected").unlink()
        for port in ("0.0.0.0:19090", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:019090", "127.0.0.1:19090\n127.0.0.1:19091"):
            with self.subTest(port=port), patch.object(FIXTURE, "command", side_effect=["center-id", inspect, port]), self.assertRaises(ValueError):
                FIXTURE.configure_query(self.secret)
        labels["com.docker.compose.project.working_dir"] = "foreign"
        with patch.object(FIXTURE, "command", side_effect=["center-id", json.dumps([{"Config": {"Labels": labels}}])]) as command, self.assertRaises(ValueError):
            FIXTURE.center_origin()
        self.assertEqual(command.call_count, 2)

    def test_engine_profile_publishes_only_owned_ports_with_shared_source_trust(self):
        original = FIXTURE.command
        def command(args):
            if args[:4] == ["docker", "network", "inspect", "bridge"]:
                return '[{"IPAM":{"Config":[{"Gateway":"172.17.0.1"}]}}]'
            return original(args)
        with patch.dict(os.environ, dict(self.env, ADDP_ONLINE_NODE_METRICS_CASE="engine-admission")), patch.object(FIXTURE, "command", side_effect=command), patch.object(FIXTURE.socket, "socket"):
            FIXTURE.prepare(self.secret)
        import shlex
        values = {key:shlex.split(value)[0] for key,value in (line.removeprefix('export ').split('=',1) for line in (self.secret/'metrics.env').read_text().splitlines())}
        self.assertEqual(values['MONITOR_METRICS_ALLOWED_PORTS'], '19100,19101,19102,19401,19402')
        self.assertEqual(values['ADDP_POSTGRES_METRICS_STATE_DIR'], str(self.secret/'postgres-sources'))
        for scope in ('infra','business'):
            folder = self.secret/('postgres-'+scope)
            self.assertEqual({path.name for path in folder.iterdir()}, {'ca.crt','server.crt','server.key'})
            self.assertEqual((folder/'ca.crt').read_bytes(), (self.secret/'admission/ca.crt').read_bytes())
            subprocess.run(['openssl','verify','-verify_ip','172.17.0.1','-purpose','sslserver','-CAfile',str(folder/'ca.crt'),str(folder/'server.crt')],check=True,capture_output=True)
        self.assertFalse((self.secret/'postgres-sources').exists())  # TLS preparation performs no SQL or source startup.

    def test_cleanup_refuses_foreign_container_before_compose(self):
        with patch.object(FIXTURE, "command", side_effect=["foreign-id", '[{"Config":{"Labels":{}}}]']) as command:
            with self.assertRaises(ValueError):
                FIXTURE.assert_owned(self.secret)
            self.assertEqual(command.call_count, 2)

    def test_cleanup_rejects_residual_sdk_listener_without_killing_it(self):
        with patch.dict(os.environ, ADDP_ONLINE_METRICS_NODE_IP="172.17.0.1"), patch.object(FIXTURE.socket, "socket") as socket, patch.object(FIXTURE, "command") as command:
            sock = socket.return_value.__enter__.return_value
            sock.connect_ex.return_value = 0
            with self.assertRaises(ValueError): FIXTURE.assert_process_listeners_closed()
            command.assert_not_called()
            sock.connect_ex.return_value = 111
            FIXTURE.assert_process_listeners_closed()
            self.assertEqual([call.args for call in sock.connect_ex.call_args_list][-2:], [(('172.17.0.1', 19101),), (('172.17.0.1', 19102),)])


class CPUPressureFixtureTest(unittest.TestCase):
    def exercise(self, failure=None, body_failure=False):
        online=importlib.import_module('scripts.test.platform-node-metrics-online')
        plan=online.cpu_pressure_plan(2,5);detail={};calls=[]
        image='sha256:'+'a'*64;identifier='b'*64
        state={'exists':False,'running':False,'name':'','labels':{},'stats':0}
        def command(args):
            calls.append(args)
            if args==['docker','info','--format','{{json .}}']:
                return json.dumps({'NCPU':4 if failure=='topology' else 2})
            if args[:2]==['docker','ps']:return identifier if state['exists'] else ''
            if args[:3]==['docker','inspect','transport']:
                return json.dumps([{'Image':image,'State':{'Running':True},'Config':{'Labels':{'com.docker.compose.project':FIXTURE.PROJECT,'com.addp.online-runtime':'platform-node-metrics'}}}])
            if args[:2]==['docker','create']:
                state['exists']=True;state['name']=args[args.index('--name')+1]
                state['labels']=dict(args[i+1].split('=',1) for i,x in enumerate(args) if x=='--label')
                return "invalid" if failure=="receipt" else identifier
            if args[:2]==['docker','inspect']:
                labels=dict(state['labels'])
                if failure=='foreign' and state['running']:labels['io.addp.cpu-pressure.owner']='foreign'
                return json.dumps([{'Id':identifier,'Name':'/'+state['name'],'Image':image,'State':{'Running':state['running']},'Mounts':[],
                    'Config':{'Labels':labels,'User':'65534:65534'},'HostConfig':{'NanoCpus':1_000_000_000 if failure=='quota' else plan['quota_nanos'],
                    'NetworkMode':'none','ReadonlyRootfs':True,'Privileged':False,'CapDrop':['ALL'],'Memory':64*1024**2,'PidsLimit':16}}])
            if args[:2]==['docker','start']:state['running']=True;return identifier
            if args[:2]==['docker','exec']:
                if '-d' in args:return ''
                if args[-1].endswith('cpu.max'):return 'max 100000' if failure=='cgroup' else '50000 100000'
                state['stats']+=1;return 'usage_usec '+str(state['stats']*5000000)+'\nnr_throttled 2\n'
            if args[:3]==['docker','rm','-f']:state['exists']=False;state['running']=False;return identifier
            raise AssertionError(args)
        with patch.object(FIXTURE,'boundary',return_value=Path('/secret')),patch.object(FIXTURE,'compose',return_value='transport'), \
             patch.object(FIXTURE.os,'cpu_count',return_value=2),patch.object(FIXTURE,'command',side_effect=command):
            try:
                with FIXTURE.cpu_pressure(Path('/secret'),plan,detail):
                    if body_failure:raise RuntimeError('original failure')
                    if failure=='expired':state['running']=False
            except (ValueError,RuntimeError) as error:
                state["error"]=str(error)
                if body_failure:
                    self.assertIsInstance(error,RuntimeError);self.assertEqual(str(error),"original failure")
                if failure is None and not body_failure:raise
            else:
                self.assertIsNone(failure)
                self.assertFalse(body_failure)
        return plan,detail,calls,state

    def test_cpu_is_hard_capped_without_network_mounts_or_new_image_and_always_removed(self):
        for body_failure in (False,True):
            plan,detail,calls,state=self.exercise(body_failure=body_failure)
            create=next(args for args in calls if args[:2]==['docker','create'])
            self.assertEqual(create[create.index('--cpus')+1],'0.5')
            self.assertIn('sha256:'+'a'*64,create)
            self.assertIn('300',create[-1])
            self.assertIn('timeout -s KILL',create[-1])
            self.assertIn('--cap-drop',create);self.assertIn('--read-only',create)
            self.assertFalse(state['exists']);self.assertTrue(detail['cpu_released'])
            if not body_failure:self.assertGreater(detail['cpu_usage_usec'],0)

    def test_cpu_rejects_wrong_topology_or_quota_before_start_and_bad_kernel_quota_after_start(self):
        for failure in ('topology','receipt','quota','cgroup','expired'):
            _,detail,calls,state=self.exercise(failure=failure)
            self.assertFalse(state['exists'])
            expected={'topology':'Docker host','receipt':'receipt','quota':'limits','cgroup':'kernel quota','expired':'hard wall-clock'}
            self.assertIn(expected[failure],state['error'])
            if failure=='cgroup':self.assertFalse(any(args[:3]==['docker','exec','-d'] for args in calls))
            if failure in ('topology','receipt','quota'):self.assertFalse(any(args[:2]==['docker','start'] for args in calls))

    def test_cpu_cleanup_never_removes_a_foreign_replacement(self):
        _,detail,calls,state=self.exercise(failure='foreign',body_failure=True)
        self.assertFalse(any(args[:3]==['docker','rm','-f'] for args in calls))
        self.assertTrue(detail['cpu_cleanup_incomplete']);self.assertNotEqual(detail.get('cpu_released'),True)

    def test_outer_cleanup_reclaims_interrupted_cpu_and_rejects_foreign_or_residual_containers(self):
        identifier='b'*64;nonce='a'*32
        for failure in (None,'foreign','residual'):
            labels={'com.docker.compose.project':FIXTURE.PROJECT,'com.docker.compose.service':'cpu-pressure',
                    'com.addp.online-runtime':'platform-node-metrics','io.addp.cpu-pressure.owner':nonce}
            row={'Id':identifier,'Name':'/addp-online-cpu-'+nonce,'Config':{'Labels':labels}}
            if failure=='foreign':labels['io.addp.cpu-pressure.owner']='foreign'
            output=[identifier,json.dumps([row]),identifier,identifier if failure=='residual' else '']
            if failure=='foreign':output=output[:2]
            with self.subTest(failure=failure),patch.object(FIXTURE,'boundary',return_value=Path('/secret')),patch.object(FIXTURE,'command',side_effect=output) as command:
                if failure:
                    with self.assertRaises(ValueError):FIXTURE.cleanup_cpu_pressure(Path('/secret'))
                else:FIXTURE.cleanup_cpu_pressure(Path('/secret'))
                removed=[call.args[0] for call in command.call_args_list if call.args[0][:3]==['docker','rm','-f']]
                self.assertEqual(len(removed),0 if failure=='foreign' else 1)
                if removed:self.assertIn('-v',removed[0]);self.assertEqual(removed[0][-1],identifier)

    def test_cpu_requires_hosted_boundary_before_any_docker_command(self):
        with patch.object(FIXTURE,'boundary',side_effect=ValueError('personal host')),patch.object(FIXTURE,'command') as command:
            with self.assertRaises(ValueError):
                with FIXTURE.cpu_pressure(Path('/secret'),{},{}):pass
            command.assert_not_called()



class EngineSourceFixtureTest(unittest.TestCase):
    def test_professional_source_dispatch_uses_two_private_scopes_without_platform_passwords(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder).resolve()
            env = {"ADDP_POSTGRES_METRICS_STATE_DIR":str(directory/"postgres-sources"), "POSTGRES_USER":"owned_admin",
                   "POSTGRES_PASSWORD":"private-sentinel", "SYSTEM_SERVICE_CLIENT_SECRET":"private-sentinel", "ADDP_ONLINE_METRICS_NODE_IP":"172.17.0.1"}
            with patch.dict(os.environ, env), patch.object(FIXTURE.subprocess, "run") as run:
                FIXTURE.engine_metrics_action(directory, "engine-metrics-up")
                installs = [call for call in run.call_args_list if call.args[0][1].endswith('/lifecycle.py')]
                self.assertEqual(len(installs), 2)
                self.assertEqual([call.args[0][-1] for call in installs], ["business", "infra"])
                for call in installs:
                    self.assertEqual(call.args[0][2:4], ["up", "--scope"])
                    self.assertNotIn("private-sentinel", json.dumps(call.kwargs["env"]))
                    scope = call.args[0][-1]
                    self.assertEqual(call.kwargs["env"]["ADDP_POSTGRES_METRICS_LISTEN"], '172.17.0.1:'+str(FIXTURE.POSTGRES_PORTS[scope]))
                    self.assertEqual(call.kwargs["env"]["ADDP_POSTGRES_METRICS_TLS_DIR"], str(directory/('postgres-'+scope)))
                run.reset_mock()
                FIXTURE.engine_metrics_action(directory, "engine-metrics-pause")
                self.assertEqual(run.call_args_list[0].args[0][-3:], ["down", "--scope", "business"])
            with patch.dict(os.environ, dict(env, ADDP_POSTGRES_METRICS_STATE_DIR='/foreign')), patch.object(FIXTURE.subprocess, "run") as run:
                with self.assertRaises(ValueError): FIXTURE.engine_metrics_action(directory, "engine-metrics-up")
                run.assert_not_called()

    def test_cleanup_attempts_both_sources_and_reports_closed_failure(self):
        with tempfile.TemporaryDirectory() as folder:
            directory = Path(folder).resolve()
            env = {"ADDP_POSTGRES_METRICS_STATE_DIR":str(directory/"postgres-sources"), "POSTGRES_USER":"owned_admin", "ADDP_ONLINE_METRICS_NODE_IP":"172.17.0.1"}
            with patch.dict(os.environ, env), patch.object(FIXTURE.subprocess, "run", side_effect=[subprocess.CalledProcessError(1, ['private-sentinel'], stderr='private-sentinel'), None]) as run:
                with self.assertRaisesRegex(ValueError, '^Owned PostgreSQL source cleanup failed$'): FIXTURE.engine_metrics_action(directory, "engine-down")
                self.assertEqual([call.args[0][-1] for call in run.call_args_list], ['business', 'infra'])

    def test_business_source_is_private_owned_and_refuses_foreign_components(self):
        with tempfile.TemporaryDirectory() as folder,patch.dict(os.environ,{"GITHUB_RUN_ID":"12345","GITHUB_RUN_ATTEMPT":"1","ADDP_ONLINE_NODE_METRICS_CASE":"engine-admission"}),patch.object(FIXTURE,"command",return_value=""),patch.object(FIXTURE.SOURCE,"local_docker"),patch.object(FIXTURE,"engine_compose") as compose:
            directory=Path(folder).resolve()
            FIXTURE.engine_source_action(directory,"engine-up")
            self.assertEqual((directory/"engine-source.env").stat().st_mode&0o777,0o600)
            content=(directory/"engine-source.env").read_text();self.assertIn("POSTGRES_DB=business",content);self.assertNotIn("addp_online",content)
            compose.assert_called_once_with(directory,"up","-d","--wait","--wait-timeout","120","postgres")
            def foreign(args):
                return "foreign-id" if args[:3]==["docker","ps","-aq"] else json.dumps({"com.docker.compose.project.working_dir":"/foreign","com.docker.compose.service":"postgres"})
            with patch.object(FIXTURE,"command",side_effect=foreign):
                with self.assertRaises(ValueError):FIXTURE.engine_source_action(directory,"engine-down")
            self.assertEqual(compose.call_count,1)
            with patch.object(FIXTURE,"engine_metrics_action"):
                FIXTURE.engine_source_action(directory,"engine-down")
            self.assertFalse((directory/"engine-source.env").exists());self.assertFalse((directory/"engine-source-owner.json").exists())

    def test_restart_is_limited_to_owned_postgres_and_refreshes_the_same_source(self):
        with tempfile.TemporaryDirectory() as folder,patch.dict(os.environ,{"GITHUB_RUN_ID":"12345","GITHUB_RUN_ATTEMPT":"1","ADDP_ONLINE_NODE_METRICS_CASE":"engine-admission","POSTGRES_PASSWORD":"private-sentinel"}),patch.object(FIXTURE,"command",return_value=""),patch.object(FIXTURE.SOURCE,"local_docker"),patch.object(FIXTURE,"engine_compose") as compose:
            directory=Path(folder).resolve();FIXTURE.engine_source_action(directory,"engine-up")
            def owned(args):
                return "owned-id" if args[:3]==["docker","ps","-aq"] else json.dumps({"com.docker.compose.project.working_dir":str(FIXTURE.ROOT/"business"),"com.docker.compose.service":"postgres"})
            with patch.object(FIXTURE,"command",side_effect=owned),patch.object(FIXTURE.subprocess,"run") as run:
                FIXTURE.engine_source_action(directory,"engine-restart")
                compose.assert_any_call(directory,"restart","--timeout","10","postgres")
                self.assertIn("deployment-inventory.py",run.call_args.args[0][1]);self.assertIn("business",run.call_args.args[0])
                self.assertNotIn("private-sentinel",json.dumps(run.call_args.kwargs["env"]))

    def test_engine_case_is_required_before_docker_or_private_files(self):
        with tempfile.TemporaryDirectory() as folder,patch.dict(os.environ,{"ADDP_ONLINE_NODE_METRICS_CASE":"regression"}),patch.object(FIXTURE,"command") as command:
            with self.assertRaises(ValueError):FIXTURE.engine_source_action(Path(folder),"engine-up")
            command.assert_not_called();self.assertEqual(list(Path(folder).iterdir()),[])

    def test_remote_docker_override_rejects_before_creating_or_stopping_resources(self):
        with tempfile.TemporaryDirectory() as folder,patch.dict(os.environ,{"ADDP_ONLINE_NODE_METRICS_CASE":"engine-admission","GITHUB_RUN_ID":"12345","GITHUB_RUN_ATTEMPT":"1","DOCKER_HOST":"tcp://foreign:2375"}),patch.object(FIXTURE,"command") as command:
            for action in ("engine-up","engine-down","engine-restart"):
                with self.assertRaises(ValueError):FIXTURE.engine_source_action(Path(folder),action)
            command.assert_not_called();self.assertEqual(list(Path(folder).iterdir()),[])

    def test_workflow_ownership_requires_a_canonical_run_and_attempt_before_docker(self):
        with tempfile.TemporaryDirectory() as folder, patch.dict(os.environ, {"GITHUB_RUN_ID":"12345", "GITHUB_RUN_ATTEMPT":"1", "ADDP_ONLINE_NODE_METRICS_CASE":"engine-admission"}, clear=True), patch.object(FIXTURE.SOURCE, "local_docker") as docker, patch.object(FIXTURE, "command") as command:
            for key, invalid in (("GITHUB_RUN_ID",""), ("GITHUB_RUN_ID","0"), ("GITHUB_RUN_ATTEMPT","other")):
                with patch.dict(os.environ, {key:invalid}), self.assertRaises(ValueError):
                    FIXTURE.engine_source_action(Path(folder), "engine-up")
            docker.assert_not_called(); command.assert_not_called()
            self.assertEqual(list(Path(folder).iterdir()), [])

    def test_engine_docker_does_not_inherit_platform_sql_credentials(self):
        with tempfile.TemporaryDirectory() as folder,patch.dict(os.environ,{"POSTGRES_PASSWORD":"private-sentinel","SYSTEM_SERVICE_CLIENT_SECRET":"private-sentinel"}),patch.object(FIXTURE.subprocess,"run") as run:
            run.return_value.stdout=""
            FIXTURE.engine_compose(Path(folder),"up","postgres")
            self.assertNotIn("private-sentinel",json.dumps(run.call_args.kwargs["env"]))
            self.assertIn("--project-directory",run.call_args.args[0]);self.assertIn("business",run.call_args.args[0])


class EngineSourceCommandTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="addp-engine-command-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve()
        self.repo = self.root / "repository"
        self.secret = self.root / "secret"
        self.secret.mkdir(mode=0o700)
        self.bin = self.root / "bin"
        self.bin.mkdir()
        for folder in ("scripts/test", "scripts/utils", "scripts/infra/postgres-observation", "business/postgres"):
            (self.repo / folder).mkdir(parents=True)
        for relative in ("scripts/test/platform-node-metrics-fixture.py", "scripts/utils/deployment-inventory.py", "scripts/infra/postgres-observation/authentication.py", "scripts/infra/postgres-observation/source.py", "scripts/infra/postgres-observation/lifecycle.py", "scripts/infra/postgres-observation/000002_lifecycle.sql", "business/postgres/pg_hba.conf"):
            shutil.copy2(FIXTURE.ROOT / relative, self.repo / relative)
        # Only OS facts and Docker transport are fixtures. Execute the actual CLI,
        # module loader, source validation and ownership files in a fresh process.
        for folder in ("scripts/test", "scripts/utils", "scripts/infra/postgres-observation"):
            (self.repo / folder / "platform.py").write_text('def system(): return "Linux"\ndef machine(): return "x86_64"\n')
        self.env = dict(PATH=str(self.bin), GITHUB_ACTIONS="true", RUNNER_OS="Linux", ADDP_ONLINE_HOSTED="1",
                        ADDP_ONLINE_HOST="1", ONLINE_SUITE="platform-node-metrics", POSTGRES_DB="addp_online",
                        ADDP_ONLINE_SECRET_DIR=str(self.secret), ADDP_ONLINE_ARTIFACT_DIR=str(self.root / "artifacts"),
                        GITHUB_RUN_ID="12345", GITHUB_RUN_ATTEMPT="1", ADDP_ONLINE_NODE_METRICS_CASE="engine-admission",
                        ADDP_DEPLOYMENT_DISCOVERY_DIR=str(self.root / "discovery"), POSTGRES_USER="addp",
                        ADDP_POSTGRES_METRICS_STATE_DIR=str(self.secret/"postgres-sources"), ADDP_ONLINE_METRICS_NODE_IP="172.17.0.1")
        docker = self.bin / "docker"
        docker.write_text("#!" + sys.executable + "\n" + "ROOT = " + repr(str(self.root)) + "\n" + r'''
import json, sys
from pathlib import Path
root = Path(ROOT)
state = root / "docker-state"
args = sys.argv[1:]
with (root / "docker-calls").open("a") as output: output.write(json.dumps(args) + "\n")
if args[:1] == ["--context"]: args = args[2:]
if args[:2] == ["context", "show"]: print("default")
elif args[:2] == ["context", "inspect"]: print(json.dumps("unix:///var/run/docker.sock"))
elif args[0] == "info": print(json.dumps(dict(id="fixture-daemon", os_type="linux", operating_system="Linux")))
elif args[0] == "ps":
    if state.exists() and not any("addp-postgres-metrics-" in arg for arg in args): print("a" * 64)
elif args[0] in ("network", "volume"):
    if state.exists(): print("owned-resource")
elif args[0] == "inspect":
    service = "redis" if (root / "foreign").exists() else "postgres"
    working_dir = str(root / "repository/business")
    if args[args.index("--format") + 1] == "{{json .Config.Labels}}":
        print(json.dumps({"com.docker.compose.project.working_dir": working_dir, "com.docker.compose.service": service}))
    else:
        print(json.dumps(dict(id="a" * 64, project="business", service=service, working_dir=working_dir, oneoff="False",
            running=True, status="running", started_at="2026-10-19T00:00:00Z", restart_count=int(state.read_text()),
            ports={"5432/tcp": [{"HostIp": "127.0.0.1", "HostPort": "5433"}]})))
elif args[0] == "compose":
    if "up" in args:
        if not state.exists(): state.write_text("0")
        if (root / "fail-up").exists(): sys.exit(17)
    elif "restart" in args: state.write_text(str(int(state.read_text()) + 1))
    elif "down" in args: state.unlink()
    else: sys.exit(18)
else: sys.exit(19)
''')
        docker.chmod(0o700)

    def cli(self, action, success=True):
        result = subprocess.run([sys.executable, str(self.repo / "scripts/test/platform-node-metrics-fixture.py"), action],
                                cwd=self.root, env=self.env, capture_output=True, text=True, timeout=15)
        if success: self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else: self.assertNotEqual(result.returncode, 0)
        return result

    def test_standalone_cli_starts_refreshes_and_cleans_without_pythonpath(self):
        self.cli("engine-up")
        self.assertEqual((self.secret / "engine-source.env").stat().st_mode & 0o777, 0o600)
        self.env["ADDP_ONLINE_TEST_RUN_ID"] = "late-application-identity"
        self.cli("engine-restart")
        source = json.loads((self.root / "discovery/business.json").read_text())
        self.assertEqual(source["state"], "available")
        self.assertEqual(source["services"][0]["containers"][0]["restart_count"], 1)
        self.cli("engine-down")
        self.assertEqual({p.name for p in self.secret.iterdir()}, {"postgres-auth"})
        self.assertFalse((self.root / "docker-state").exists())

    def test_real_cli_cleanup_after_partial_start_refuses_foreign_components(self):
        (self.root / "fail-up").touch()
        result = self.cli("engine-up", success=False)
        self.assertTrue((self.secret / "engine-source-owner.json").exists(), result.stdout + result.stderr)
        self.assertTrue((self.root / "docker-state").exists())
        self.env["GITHUB_RUN_ATTEMPT"] = "2"
        self.cli("engine-down", success=False)
        self.assertTrue((self.root / "docker-state").exists())
        self.env["GITHUB_RUN_ATTEMPT"] = "1"
        (self.root / "foreign").touch()
        self.cli("engine-down", success=False)
        self.assertTrue((self.secret / "engine-source-owner.json").exists())
        self.assertTrue((self.root / "docker-state").exists())
        (self.root / "foreign").unlink()
        self.cli("engine-down")
        self.assertEqual({p.name for p in self.secret.iterdir()}, {"postgres-auth"})
        self.assertFalse((self.root / "docker-state").exists())



class AvailabilityPauseFixtureTest(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='addp-availability-pause-');self.addCleanup(self.temp.cleanup)
        self.directory=Path(self.temp.name)
        self.records={u:{'unit':u,'pid':2100+i,'start_ticks':str(1100+i)} for i,u in enumerate(FIXTURE.AVAILABILITY_UNITS)}
        self.addCleanup(patch.stopall)
        patch.object(FIXTURE,'boundary',return_value=self.directory).start()
        patch.dict(os.environ,ADDP_ONLINE_NODE_METRICS_CASE='service-availability').start()
        self.read=patch.object(FIXTURE,'worker_process_record',side_effect=lambda u:dict(self.records[u])).start()
        self.open=patch.object(FIXTURE.os,'pidfd_open',side_effect=lambda p:p+100,create=True).start()
        self.send=patch.object(FIXTURE.signal,'pidfd_send_signal',create=True).start()
        patch.object(FIXTURE.os,'close').start()
        real_exists=Path.exists
        patch.object(Path,'exists',lambda p:True if str(p).startswith('/proc/') else real_exists(p)).start()

    def test_same_meta_profile_and_processes_recovered_on_failure(self):
        self.assertIs(FIXTURE.WORKER_PROFILES['service-availability'],FIXTURE.WORKER_PROFILES['meta-workers'])
        with self.assertRaisesRegex(RuntimeError,'phase failed'):
            with FIXTURE.availability_pauses(self.directory) as act:
                act('meta-worker-1',True);act('meta-worker-2',True)
                record=self.directory/FIXTURE.PAUSED_WORKERS
                self.assertEqual(record.stat().st_mode & 0o777,0o600)
                self.assertEqual(len(json.loads(record.read_text())['workers']),2)
                raise RuntimeError('phase failed')
        self.assertFalse((self.directory/FIXTURE.PAUSED_WORKERS).exists())
        for pid in (2100,2101):
            self.assertIn(unittest.mock.call(pid+100,FIXTURE.signal.SIGSTOP),self.send.call_args_list)
            self.assertIn(unittest.mock.call(pid+100,FIXTURE.signal.SIGCONT),self.send.call_args_list)

    def test_changed_identity_before_signal_is_rejected_and_recovery_record_retained(self):
        with FIXTURE.availability_pauses(self.directory) as act:
            self.records['meta-worker-1']['start_ticks']='changed'
            with self.assertRaisesRegex(ValueError,'identity changed'):act('meta-worker-1',True)
        self.send.assert_not_called()
        path=self.directory/FIXTURE.PAUSED_WORKERS
        path.write_text(json.dumps({'root':str(FIXTURE.ROOT),'workers':[dict(self.records['meta-worker-1'],start_ticks='old')]}));path.chmod(0o600)
        with self.assertRaisesRegex(ValueError,'incomplete'):FIXTURE.resume_paused_workers(self.directory)
        self.assertTrue(path.exists());self.send.assert_not_called()

    def test_outer_recovery_rejects_wrong_case_or_foreign_scope(self):
        with patch.dict(os.environ,ADDP_ONLINE_NODE_METRICS_CASE='meta-workers'),self.assertRaisesRegex(ValueError,'dedicated Hosted'):
            FIXTURE.resume_paused_workers(self.directory)
        self.send.assert_not_called()
        path=self.directory/FIXTURE.PAUSED_WORKERS
        path.write_text(json.dumps({'root':'/foreign','workers':list(self.records.values())}));path.chmod(0o600)
        with self.assertRaisesRegex(ValueError,'scope'):FIXTURE.resume_paused_workers(self.directory)
        self.send.assert_not_called();self.assertTrue(path.exists())

    def test_recovery_tries_other_worker_after_one_failed(self):
        path=self.directory/FIXTURE.PAUSED_WORKERS
        path.write_text(json.dumps({'root':str(FIXTURE.ROOT),'workers':list(self.records.values())}));path.chmod(0o600)
        self.read.side_effect=lambda u:dict(self.records[u],start_ticks='changed') if u.endswith('-1') else self.records[u]
        with self.assertRaisesRegex(ValueError,'incomplete'):FIXTURE.resume_paused_workers(self.directory)
        self.send.assert_called_once_with(2201,FIXTURE.signal.SIGCONT)
        self.assertTrue(path.exists())


class AvailabilityProcessRecordTest(unittest.TestCase):
    def test_proc_proof_rejects_foreign_binary_cwd_and_dead_process(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp).resolve();(root/'.dev-pids').mkdir();(root/'.dev-bins').mkdir()
            (root/'.dev-pids/meta-worker-1.pid').write_text('51')
            binary=root/'.dev-bins/addp-meta-worker';binary.write_text('fixture')
            original_resolve,original_read=Path.resolve,Path.read_text
            for fault in (None,'cwd','exe','dead'):
                def resolved(path,**kwargs):
                    if str(path)=='/proc/51/cwd': return Path('/foreign') if fault=='cwd' else root
                    if str(path)=='/proc/51/exe': return Path('/foreign') if fault=='exe' else binary
                    return original_resolve(path,**kwargs)
                def read(path,*args,**kwargs):
                    if str(path)=='/proc/51/stat': return '51 (worker with spaces) '+' '.join(['Z' if fault=='dead' else 'S']+['0']*18+['1234','0'])
                    return original_read(path,*args,**kwargs)
                with patch.object(FIXTURE,'ROOT',root),patch.object(Path,'resolve',resolved),patch.object(Path,'read_text',read):
                    if fault:
                        with self.assertRaises(ValueError):FIXTURE.worker_process_record('meta-worker-1')
                    else:self.assertEqual(FIXTURE.worker_process_record('meta-worker-1'),{'unit':'meta-worker-1','pid':51,'start_ticks':'1234'})
            with patch.object(FIXTURE,'ROOT',root):
                with self.assertRaises(ValueError):FIXTURE.worker_process_record('system')
                (root/'.dev-pids/meta-worker-1.pid').write_text('1')
                with self.assertRaises(ValueError):FIXTURE.worker_process_record('meta-worker-1')


if __name__ == "__main__":
    unittest.main()
