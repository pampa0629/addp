import importlib
import json
import os
import subprocess
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


if __name__ == "__main__":
    unittest.main()
