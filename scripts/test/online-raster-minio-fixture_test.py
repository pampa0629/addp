import http.client
import http.server
import hashlib
import threading
import importlib.util
import json
import math
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

PATH = Path(__file__).parents[2] / 'business/scripts/online-raster-minio-fixture.py'
spec = importlib.util.spec_from_file_location('raster_physical_fixture', PATH)
m = importlib.util.module_from_spec(spec)
spec.loader.exec_module(m)


class FakeS3Error(Exception):
    def __init__(self, code): self.code = code


class RasterPhysicalFixtureTest(unittest.TestCase):
    def test_multiband_aggregation_oracle_keeps_separate_nan_zero_partial_alpha_and_rotated_edges(self):
        first = list(m.aggregate_pixels('aggregate-multiband-mean', 1))
        second = list(m.aggregate_pixels('aggregate-multiband-mean', 2))
        self.assertEqual(first[:6], [None, 2., None, 4., 0., 6.])
        self.assertEqual(second[:6], [2., None, None, 8., 0., 12.])
        expectation = m.aggregate_expectation('aggregate-multiband-mean')
        self.assertEqual(expectation['band_valid_pixels'], [16382, 16382])
        self.assertEqual(expectation['band_count'], 2)
        self.assertEqual(expectation['transform'], [110, .02, 0, 20.32, 0, -.02])
        expectation = m.aggregate_expectation('aggregate-rotated-sum')
        self.assertEqual(expectation['band_valid_pixels'], [4472, 4472])
        self.assertEqual(expectation['transform'], [110, .03, .02, 20.32, .006, -.1])
        for band, first, right, bottom, last in ((1, 1552., 1152., 48772., 16384.),
                                                (2, 3104., 2304., 97544., 32768.)):
            pixels = list(m.aggregate_pixels('aggregate-rotated-sum', band))
            self.assertEqual([pixels[0], pixels[85], pixels[51*86], pixels[-1]],
                             [first, right, bottom, last])
            self.assertEqual(list(m.aggregate_pixels('aggregate-rotated-sum', band, 2)),
                             [pixels[y*86+x] for y in range(0, 52, 2) for x in range(0, 86, 2)])
        for case in m.AGGREGATE_MULTIBAND_CASES:
            self.assertIn('verify-'+case, m.ACTIONS)
            self.assertIsNone(m.aggregate_config(case)['bands'])

    def test_aggregate_min_max_oracle_uses_selected_band_valid_cells_and_partial_edges(self):
        for case, first, right, bottom, interior in (
            ('aggregate-min', 4., 512., 130562., 2568.),
            ('aggregate-max', 2054., 2560., 130566., 4620.)):
            with self.subTest(case=case):
                pixels = list(m.aggregate_pixels(case))
                self.assertEqual(len(pixels), 86*52)
                self.assertEqual(pixels[0], first)
                self.assertEqual(pixels[85], right)
                self.assertEqual(pixels[51*86], bottom)
                self.assertEqual(pixels[87], interior)
                self.assertEqual(pixels[-1], 131072.)
                self.assertEqual(list(m.aggregate_pixels(case, level=2)), [pixels[y*86+x] for y in range(0,52,2) for x in range(0,86,2)])
                self.assertIn('verify-'+case, m.ACTIONS)
        with self.assertRaises(m.FixtureError): list(m.aggregate_pixels('aggregate-median'))

    def test_fixed_expected_pixels_are_immutable_and_reused_without_recomputing_formulas(self):
        for oracle, calculation, kwargs in (
            (m.average_pixels, 'area_average', {'width': 171}),
            (m.bilinear_pixels, 'bilinear_value', {}),
        ):
            with self.subTest(oracle=oracle.__name__):
                expected = oracle(1, **kwargs)
                self.assertIsInstance(expected, tuple)
                self.assertGreater(len(expected), 0)
                with patch.object(m, calculation, side_effect=AssertionError('fixed oracle recomputed')):
                    self.assertEqual(oracle(1, **kwargs), expected)
                    self.assertEqual(tuple(iter(oracle(1, **kwargs))), expected)
                with self.assertRaises(TypeError):
                    expected[0] = 123.

    def test_manager_physical_refuses_non_hosted_owner_or_unknown_action_before_mutation(self):
        for values, action in (({}, 'seed'),
                ({'ADDP_ONLINE_HOSTED': '1', 'ONLINE_SUITE': 'manager-internal-artifact-lineage'}, 'unknown')):
            with self.subTest(action=action), patch.dict(os.environ, values, clear=True), patch.object(m, 'command') as command:
                with self.assertRaises(m.FixtureError): m.manager_physical(action)
                command.assert_not_called()

    def test_analysis_oracle_counts_boundaries_and_outside_range_independently(self):
        results = m.analysis_expectations()
        self.assertEqual(results['statistics-band-2']['mean'], 65538)
        self.assertEqual(results['statistics-band-2']['min'], 4)
        self.assertEqual(results['statistics-band-2']['max'], 131072)
        self.assertIsNone(results['statistics-all-invalid']['mean'])
        self.assertEqual(results['histogram-auto']['edges'], [2, 16385.5, 32769, 49152.5, 65536])
        self.assertEqual(results['histogram-auto']['counts'], [16384, 16383, 16384, 16384])
        self.assertEqual(results['histogram-range']['counts'], [8192, 8192, 8192, 8193])
        self.assertEqual(results['histogram-range']['outside_count'], 32766)

    def test_multiband_independent_oracle_keeps_separate_holes_partial_alpha_zero_and_joint_mask(self):
        first, second, alpha = [list(m.multiband_pixels(band)) for band in (1, 2, 3)]
        self.assertEqual(first[:6], [None, 2., None, 4., 0., 6.])
        self.assertEqual(second[:6], [2., None, None, 8., 0., 12.])
        self.assertEqual(alpha[:6], [128., 128., 0., 128., 255., 255.])
        self.assertEqual(list(m.multiband_pixels(1, joint=True))[:6], [None, None, None, 12., 0., 18.])
        self.assertEqual([sum(value is not None for value in values) for values in (first, second)], [16382, 16382])
        self.assertEqual(m.multiband_expectation('multiband-joint')['valid_pixels'], 16381)
        source = list(m.multiband_pixels(1, source=True))
        self.assertEqual(source[4:6], [1e6, 1e6])
        self.assertEqual(source[:256], source[256:512])
        for case in m.MULTIBAND_CASES: self.assertIn('verify-' + case, m.ACTIONS)

    def test_area_average_oracle_excludes_each_bands_holes_and_differs_from_nearest_and_early_math(self):
        first, second, alpha = [list(m.average_pixels(band)) for band in (1, 2, 3)]
        self.assertEqual(first[:6], [None, 22., None, 54., 0., 88.])
        self.assertEqual(second[:6], [12., None, None, 108., 0., 168.])
        self.assertAlmostEqual(first[6], 308/3)
        self.assertAlmostEqual(second[6], 608/3)
        self.assertEqual(alpha[:7], [255., 255., 0., 255., 255., 255., 255.])
        self.assertEqual(list(m.average_pixels(1, joint=True))[:6], [None, None, None, 162., 0., 256.])
        self.assertEqual(m.average_source_pixel(1, 1, 7), 60.)
        self.assertNotEqual(first[3], m.average_source_pixel(1, 1, 7))
        # Intersecting source masks before math wrongly discards one contributor from each band.
        early_math = (84+168+88+176)/2
        self.assertEqual(early_math, 258.)
        self.assertNotEqual(first[5]+second[5], early_math)
        self.assertEqual([sum(value is not None for value in values) for values in (first, second)], [16382, 16382])

    def test_bilinear_oracle_keeps_invalid_centres_and_independent_neighbour_weights(self):
        self.assertEqual(m.bilinear_value(1, 1, 13), 51.)
        self.assertNotEqual(m.bilinear_value(1, 1, 13), m.average_source_pixel(1, 0, 6))
        self.assertEqual(m.bilinear_value(2, 1, 13), 102.)
        self.assertIsNone(m.bilinear_value(1, 1, 20))
        self.assertEqual(m.bilinear_value(2, 1, 20), 123.)
        self.assertAlmostEqual(m.bilinear_value(1, 1, 22), 1120/13)
        self.assertAlmostEqual(m.bilinear_value(2, 1, 22), 2168/13)
        self.assertNotAlmostEqual(m.bilinear_value(1, 1, 22)+m.bilinear_value(2, 1, 22), 253.2)
        self.assertEqual(m.bilinear_value(1, 1, 17), 0.)
        self.assertIsNone(m.bilinear_value(1, 1, 8))
        for band in (1, 2):
            self.assertEqual(sum(value is not None for value in m.bilinear_pixels(band)), 262104)
        self.assertEqual(sum(value is not None for value in m.bilinear_pixels(1, joint=True)), 262080)
        self.assertEqual(sum(0 < value < 255 for value in m.bilinear_pixels(3)), 64)

    def test_fractional_area_oracle_uses_overlap_weights_and_independent_band_validity(self):
        first, second, alpha = [list(m.average_pixels(band, width=171)) for band in (1, 2, 3)]
        self.assertIsNone(first[0])
        self.assertAlmostEqual(second[0], 255/32)
        self.assertIsNone(second[2])
        self.assertIsNone(first[3])
        self.assertIsNone(second[3])
        self.assertEqual(alpha[3], 0.)
        self.assertAlmostEqual(first[4], 211179/4064)
        self.assertNotAlmostEqual(first[4], 54.)  # Incorrect equal contributor weights.
        self.assertAlmostEqual(first[5], 19239/1024)  # Covered source zeros contribute.
        self.assertAlmostEqual(first[7], 4370448/50317)
        self.assertAlmostEqual(second[7], 8564056/51341)
        self.assertNotAlmostEqual(first[7]+second[7], 4596762/18061)  # Premature joint mask.
        self.assertAlmostEqual(first[-1], 16776705/64)
        self.assertEqual([sum(value is not None for value in values) for values in (first, second)], [29239, 29239])
        self.assertEqual(sum(value is not None for value in m.average_pixels(1, joint=True, width=171)), 29238)
        self.assertEqual(sum(0 < value < 255 for value in alpha), 0)

    def test_topology_source_has_independent_valid_zero_bands(self):
        sources = {name: (spatial, nodata) for name, spatial, nodata in m.SOURCE_FILES}
        self.assertEqual(sources[m.TOPOLOGY_SOURCE], (False, -9999.))
        for case, band in m.TOPOLOGY_BANDS.items():
            self.assertEqual(m.footprint_source_name(case), m.TOPOLOGY_SOURCE)
            values = m.source_pixels(m.TOPOLOGY_SOURCE, band, False, -9999.)
            positions = {index for index, value in enumerate(values) if value == 0.}
            expected = ({i*m.SIZE+i for i in range(64, 68)} if band == 1 else
                        {row*m.SIZE+column for row in range(64, 68) for column in range(64, 68)} -
                        {65*m.SIZE+65, 66*m.SIZE+66})
            self.assertEqual(positions, expected)
            self.assertTrue(all(value == -9999. for index, value in enumerate(values) if index not in expected))

    def test_source_declarations_keep_finite_and_nan_sentinels_and_explicit_grids(self):
        sources = {name: (spatial, nodata) for name, spatial, nodata in m.SOURCE_FILES}
        self.assertEqual(len(sources), 10)
        self.assertFalse(sources[m.ROTATED_SOURCE][0])
        self.assertTrue(math.isnan(sources[m.ROTATED_SOURCE][1]))
        self.assertEqual(m.footprint_source_name('footprint-rotated'), m.ROTATED_SOURCE)
        self.assertEqual(m.footprint_transform('footprint-rotated'), (110,.01,.004,20.32,.002,-.02))
        self.assertEqual(sources['multiband-average-finite.tif'], (False, -9999.))
        self.assertTrue(math.isnan(sources['multiband-average.tif'][1]))
        self.assertEqual(m.multiband_source_name('multiband-average-finite'), 'multiband-average-finite.tif')
        self.assertEqual(m.multiband_source_name('multiband-average-finite-joint'), 'multiband-average-finite.cog.tif')
        self.assertEqual(m.multiband_expectation('multiband-average-finite')['width'], 171)
        self.assertEqual(m.multiband_expectation('multiband-average-finite')['valid_pixels'], 29239)
        self.assertEqual(m.multiband_expectation('multiband-average-finite-joint')['valid_pixels'], 29238)

    def test_independent_mosaic_formulas_cover_all_three_sources_and_preserve_valid_zero(self):
        for case in m.MOSAIC_SOURCE_CASES:
            self.assertEqual(m.mosaic_source_expectation(case)['band_valid_pixels'], [65534, 65533])
            self.assertEqual(m.mosaic_source_pixels(case)[1], 0.)
            self.assertEqual(m.mosaic_source_pixels(case, 2)[256 + 240], 0.)
            self.assertIsNone(m.mosaic_source_pixels(case, 2)[64 * 256 + 10])
        self.assertEqual(m.mosaic_source_pixels('multisource-first')[100 * 256 + 150], 325751.)
        self.assertEqual(m.mosaic_source_pixels('multisource-last')[100 * 256 + 150], 225751.)
        self.assertEqual([m.source_grid(name, False)[0] for name in m.MOSAIC_SOURCE_WINDOWS], [160, 112, 112])

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='addp-raster-fixture-test-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / 'addp-online-secret-test'
        self.root.mkdir(mode=0o700)
        self.env = {'GITHUB_ACTIONS': 'true', 'RUNNER_OS': 'Linux', 'ADDP_ONLINE_HOST': '1', 'ADDP_ONLINE_HOSTED': '1',
                    'RUNNER_TEMP': self.temp.name, 'ADDP_ONLINE_SECRET_DIR': str(self.root), 'ADDP_ONLINE_TEST_RUN_ID': 'raster-test'}
        self.commands = []

    def command(self, args, **kwargs):
        self.commands.append(args)
        return ''

    def test_admission_rejects_shared_personal_and_public_secret_directory(self):
        with patch.dict(os.environ, self.env), patch.object(m.os, 'uname', return_value=SimpleNamespace(sysname='Linux', machine='x86_64')):
            self.assertEqual(m.environment(), self.root)
            for changes in ({'GITHUB_ACTIONS': 'false'}, {'ADDP_ONLINE_HOSTED': '0'}, {'ADDP_ONLINE_SECRET_DIR': self.temp.name}):
                with patch.dict(os.environ, changes), self.assertRaises(m.FixtureError): m.environment()
            self.root.chmod(0o755)
            with self.assertRaises(m.FixtureError): m.environment()

    def test_cleanup_proxy_identity_uses_host_uid_and_actual_docker_socket_group(self):
        with patch.object(m.os, 'getuid', return_value=1001), patch.object(m.os, 'stat', return_value=SimpleNamespace(st_gid=987)) as stat_socket:
            self.assertEqual(m.cleanup_proxy_identity(), '1001:987')
            stat_socket.assert_called_once_with('/var/run/docker.sock')

    def test_worker_failure_identifies_stage_without_private_command_diagnostic(self):
        with patch.dict(os.environ, {'ADDP_ONLINE_RASTER_RUNTIME_IMAGE':'verified-image'}), patch.object(m, 'command', side_effect=m.FixtureError('private command text')):
            with self.assertRaisesRegex(m.FixtureError, '^physical_worker_failed$'):
                m.physical_worker('verify-tiles-cleanup-mid', self.root)

    def test_cleanup_event_permission_failure_is_named_without_private_path(self):
        with patch.object(m.Path, 'read_text', side_effect=PermissionError('/private/secret/event.json')):
            with self.assertRaisesRegex(m.FixtureError, '^cleanup_event_permission_denied$'):
                m.cleanup_evidence(self.root, 'tiles-cleanup-mid')
        with self.assertRaisesRegex(m.FixtureError, '^cleanup_event_missing$'):
            m.cleanup_evidence(self.root, 'tiles-cleanup-mid')

    def test_start_keeps_distinct_credentials_owner_only_and_uses_tmpfs_without_platform_api(self):
        with patch.dict(os.environ, self.env), patch.object(m, 'owned', return_value=False), patch.object(m, 'command', self.command), \
             patch.object(m.urllib.request, 'urlopen') as request, patch.object(m, 'cleanup_proxy_identity', create=True, return_value='1001:121'):
            request.return_value.__enter__.return_value.status = 200
            m.start(self.root)
        config = json.loads((self.root / 'raster-fixture.json').read_text())
        self.assertNotEqual(config['source']['secret_key'], config['target']['secret_key'])
        self.assertNotEqual(config['source']['endpoint'], config['target']['endpoint'])
        for file in self.root.iterdir():
            self.assertEqual(stat.S_IMODE(file.stat().st_mode), 0o600)
        for role in ('source', 'target'):
            descriptor = json.loads((self.root / f'raster-{role}-engine.json').read_text())
            self.assertEqual(descriptor['engine_type'], 'minio')
            self.assertIn('raster-test', descriptor['name'])
            credentials = config[role]['writer'] if role == 'target' else config[role]
            for key in ('access_key', 'secret_key'):
                self.assertEqual(descriptor['connection_info'][key], credentials[key])
                if role == 'target':
                    self.assertNotEqual(credentials[key], config[role][key])
        self.assertEqual(len(self.commands), 3)
        proxy = self.commands[-1]
        self.assertIn(m.CONTAINERS[3], proxy)
        self.assertEqual(proxy[proxy.index('--user')+1], '1001:121')
        self.assertIn("/var/run/docker.sock:/var/run/docker.sock", proxy)
        self.assertEqual(json.loads((self.root / "raster-target-engine.json").read_text())["connection_info"]["endpoint"], "127.0.0.1:59014")
        for args in self.commands[:2]:
            self.assertIn('type=tmpfs,destination=/data', args)
            self.assertIn('com.addp.online-fixture=raster-workflow', args)
            self.assertIn('--env-file', args)
            for entry in config.values(): self.assertNotIn(entry['secret_key'], ' '.join(args))

    def test_cleanup_proxy_preserves_signed_request_and_confirms_backend_before_fault_and_reply(self):
        received, injected = [], []
        class Backend(http.server.BaseHTTPRequestHandler):
            def log_message(self, *args): pass
            def do_PUT(inner):
                body = inner.rfile.read(int(inner.headers['Content-Length']))
                received.append((inner.path, body, inner.headers['Host'], inner.headers['Authorization']))
                status = 403 if 'denied' in body.decode() else 200
                inner.send_response(status); inner.send_header('ETag', 'fixture-etag'); inner.end_headers()
                inner.wfile.write(b'backend-body')
        backend = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Backend)
        self.addCleanup(backend.server_close)
        thread = threading.Thread(target=backend.serve_forever, daemon=True); thread.start()
        self.addCleanup(backend.shutdown)
        m.private_json(self.root/'raster-fixture.json', {'target':{'endpoint':'127.0.0.1:'+str(backend.server_port),'bucket':'raster-target'}})
        def inject(root, prefix, key, body, status):
            self.assertEqual(received[-1][1], body)
            self.assertEqual(status, 200)
            injected.append((prefix, key, body))
        proxy = m.cleanup_proxy(self.root, ('127.0.0.1',0), inject)
        self.addCleanup(proxy.server_close)
        threading.Thread(target=proxy.serve_forever, daemon=True).start()
        self.addCleanup(proxy.shutdown)
        for path, body, expected in (
            ('/raster-target/ordinary.tif', b'normal', 200),
            ('/raster-target/tiles-cleanup-mid/r0_c0.tif', b'denied', 403),
            ('/raster-target/tiles-cleanup-mid/r0_c0.tif', b'confirmed-first', 200),
            ('/raster-target/tiles-cleanup-final/r160_c160.tif', b'confirmed-last', 200),
            ('/raster-target/tiles-cleanup-mid/r0_c0.tif', b'repeated', 502)):
            connection = http.client.HTTPConnection('127.0.0.1', proxy.server_port, timeout=5)
            connection.request('PUT', path, body, {'Host':'signed-host:59014','Authorization':'private-signature'})
            response = connection.getresponse(); data = response.read(); connection.close()
            self.assertEqual(response.status, expected)
            if expected != 502: self.assertEqual(data, b'backend-body')
        self.assertEqual(len(injected), 2)
        self.assertTrue(all(host == 'signed-host:59014' and auth == 'private-signature' for _,_,host,auth in received))

    def test_obstacle_requires_one_exact_regular_file_and_keeps_uploaded_bytes(self):
        body = b'confirmed COG bytes'
        digest = hashlib.sha256(body).hexdigest()
        workspace = self.root/'addp-raster-test'; workspace.mkdir()
        path = workspace/'encoded.tif'; path.write_bytes(body)
        args = [sys.executable, '-c', m.CLEANUP_OBSTACLE_SCRIPT, str(self.root), digest, str(len(body))]
        for fault in ('wrong-hash', 'duplicate', 'symlink'):
            with self.subTest(fault=fault):
                other = workspace/'other.tif'
                if fault == 'duplicate': other.write_bytes(body)
                if fault == 'symlink':
                    path.unlink(); path.symlink_to(self.root/'outside.tif'); (self.root/'outside.tif').write_bytes(body)
                result = subprocess.run(args if fault != 'wrong-hash' else args[:-2]+['0'*64,args[-1]], capture_output=True, text=True)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(Path(str(path)+'.msk').exists())
                if other.exists(): other.unlink()
                if path.is_symlink(): path.unlink(); path.write_bytes(body)
        event = json.loads(subprocess.check_output(args, text=True))
        self.assertEqual(event['sha256'], digest)
        self.assertEqual(event['size_bytes'], len(body))
        self.assertEqual(path.read_bytes(), body)
        self.assertTrue(Path(str(path)+'.msk').is_dir())
        with self.assertRaises(OSError): Path(str(path)+'.msk').unlink()
        self.assertNotEqual(subprocess.run(args, capture_output=True).returncode, 0)

    def test_runtime_exec_checks_exit_and_parses_only_complete_stdout_frames(self):
        container = {'Config':{'Labels':{'com.addp.online-runtime':m.SUITE}},'State':{'Running':True}}
        body = b'{"obstacle_created":true}'
        frame = lambda stream, data: bytes([stream,0,0,0])+len(data).to_bytes(4,'big')+data
        valid = frame(2,b'private stderr')+frame(1,body)
        for output, status, accepted in (
            (valid, {'Running':False,'ExitCode':0}, True),
            (valid, {'Running':False,'ExitCode':1}, False),
            (valid, {'Running':True,'ExitCode':0}, False),
            (valid[:-1], {'Running':False,'ExitCode':0}, False),
            (frame(3,body), {'Running':False,'ExitCode':0}, False)):
            with self.subTest(accepted=accepted, status=status), patch.object(m,'docker_request',side_effect=[
                json.dumps(container).encode(), b'{"Id":"owned-exec"}', output, json.dumps(status).encode()]) as request:
                if accepted: self.assertEqual(m.runtime_exec(['script','arg']), {'obstacle_created':True})
                else:
                    with self.assertRaises(m.FixtureError): m.runtime_exec(['script','arg'])
                self.assertEqual(request.call_args_list[1].args[2]['Cmd'], ['python','-c','script','arg'])

    def test_runtime_exec_rejects_unowned_container_before_exec_and_redacts_private_errors(self):
        for container in ({'Config':{'Labels':{}},'State':{'Running':True}},
                          {'Config':{'Labels':{'com.addp.online-runtime':m.SUITE}},'State':{'Running':False}}):
            with patch.object(m, 'docker_request', return_value=json.dumps(container).encode()) as request:
                with self.assertRaises(m.FixtureError): m.runtime_exec(['private script'])
                self.assertEqual(request.call_count, 1)

    def provision_writer(self, fault=None):
        entry = {'endpoint': 'target:9000', 'bucket': 'raster-target',
            'access_key': 'fixture-root', 'secret_key': 'private-root',
            'writer': {'access_key': 'fixture-writer', 'secret_key': 'private-writer'}}
        if fault == 'root': entry['writer']['access_key'] = entry['access_key']
        denied = m.TILE_FAILURE_PREFIX + '/' + m.TILE_FAILURE_KEY + '.tif'
        client, admin, provider = Mock(), Mock(), Mock()
        def upload(bucket, key, path):
            if key == '_writer_probe' and fault == 'allowed': raise FakeS3Error('AccessDenied')
            if key == denied and fault != 'ignored':
                raise FakeS3Error('NoSuchBucket' if fault == 'wrong-error' else 'AccessDenied')
        client.fput_object.side_effect = upload
        client.list_objects.return_value = [object()] if fault == 'residual' else []
        minio = Mock(return_value=client)
        minio_admin = Mock(return_value=admin)
        modules = {'minio': SimpleNamespace(Minio=minio),
            'minio.credentials': SimpleNamespace(StaticProvider=provider),
            'minio.error': SimpleNamespace(S3Error=FakeS3Error),
            'minio.minioadmin': SimpleNamespace(MinioAdmin=minio_admin)}
        with patch.dict(sys.modules, modules):
            result = m.provision_target_writer(entry, self.root)
        return result, entry, client, admin, provider, minio

    def test_target_writer_policy_denies_only_the_second_failure_tile_and_proves_it(self):
        result, entry, client, admin, provider, minio = self.provision_writer()
        self.assertEqual(result, {'writer_is_root': False,
            'denied_object': 'tiles-write-failure/r0_c160.tif', 'error_code': 'AccessDenied'})
        provider.assert_called_once_with(entry['access_key'], entry['secret_key'])
        minio.assert_called_once_with(entry['endpoint'], **entry['writer'], secure=False)
        admin.user_add.assert_called_once_with(entry['writer']['access_key'], entry['writer']['secret_key'])
        policy_name, policy_path = admin.policy_add.call_args.args
        policy = json.loads(Path(policy_path).read_text())
        self.assertEqual([item for item in policy['Statement'] if item['Effect'] == 'Deny'], [{
            'Effect': 'Deny', 'Action': ['s3:PutObject'],
            'Resource': ['arn:aws:s3:::raster-target/tiles-write-failure/r0_c160.tif']}])
        admin.policy_set.assert_called_once_with(policy_name, user=entry['writer']['access_key'])
        self.assertEqual([call.args[1] for call in client.fput_object.call_args_list],
            ['_writer_probe', 'tiles-write-failure/r0_c160.tif'])
        client.remove_object.assert_called_once_with('raster-target', '_writer_probe')
        client.list_objects.assert_called_once_with('raster-target', recursive=True)
        self.assertNotIn('private', json.dumps(result))

    def test_target_writer_refuses_ineffective_fault_or_unusable_or_dirty_storage(self):
        for fault in ('root', 'ignored', 'wrong-error', 'residual', 'allowed'):
            expected = FakeS3Error if fault == 'allowed' else m.FixtureError
            with self.subTest(fault=fault), self.assertRaises(expected):
                self.provision_writer(fault)

    def test_seed_retains_source_fingerprints_in_private_directory(self):
        evidence = {'seeded': True, 'source_sha256': {'source.tif': 'source-hash', 'spatial.tif': 'spatial-hash'}}
        image = 'localhost:5001/addp-geopython-workflow-engine:raster-online'
        with patch.dict(os.environ, {'ADDP_ONLINE_RASTER_RUNTIME_IMAGE': image}), \
             patch.object(m, 'command', return_value=json.dumps(evidence)) as command:
            self.assertEqual(m.physical_worker('seed', self.root), evidence)
        path = self.root / 'raster-source-sha256.json'
        self.assertEqual(json.loads(path.read_text()), evidence['source_sha256'])
        self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        self.assertIn(str(self.root) + ':/secrets:ro', command.call_args.args[0])
        self.assertIn(image, command.call_args.args[0])
        self.assertNotIn('addp-geopython-workflow-engine:dev', command.call_args.args[0])

    def test_physical_worker_rejects_missing_product_image_before_docker(self):
        with patch.dict(os.environ, {'ADDP_ONLINE_RASTER_RUNTIME_IMAGE': ''}), \
             patch.object(m, 'command') as command:
            with self.assertRaisesRegex(m.FixtureError, 'product Runtime image is required'):
                m.physical_worker('verify-create', self.root)
            command.assert_not_called()

    def test_partial_start_failure_cleans_both_owned_containers(self):
        def fail(args, **kwargs): raise m.FixtureError('run failed')
        with patch.dict(os.environ, self.env), patch.object(m, 'owned', return_value=False), patch.object(m, 'command', fail), \
             patch.object(m, 'stop') as stop:
            with self.assertRaises(m.FixtureError): m.start(self.root)
            stop.assert_called_once()

    def test_stop_never_removes_foreign_containers_and_attempts_all_owned_cleanup(self):
        def ownership(name):
            if name == 'addp-raster-target': raise m.FixtureError('foreign')
            return False
        with patch.object(m, 'owned', side_effect=ownership) as owned, patch.object(m, 'command', self.command):
            with self.assertRaises(m.FixtureError): m.stop()
            self.assertEqual({call.args[0] for call in owned.call_args_list}, set(m.CONTAINERS))
            self.assertEqual(self.commands, [])

    def test_docker_daemon_failure_is_not_reported_as_zero_residuals(self):
        with patch.object(m.subprocess, 'run', return_value=subprocess.CompletedProcess([], 1, '', 'daemon unavailable')), \
             patch.object(m, 'command', side_effect=m.FixtureError('daemon unavailable')):
            with self.assertRaises(m.FixtureError): m.owned('addp-raster-source')

    def test_owned_container_inspection_rejects_foreign_label(self):
        raw = json.dumps([{'Config': {'Labels': {'com.addp.online-fixture': 'other'}}}])
        with patch.object(m.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, raw, '')):
            with self.assertRaises(m.FixtureError): m.owned('addp-raster-source')


if __name__ == '__main__': unittest.main()
