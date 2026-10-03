#!/usr/bin/env python3
"""Real Redis assertions; invoked only by the owned Business T2 gate."""
import os
from pathlib import Path
import socket
import subprocess
import time
import unittest

ROOT = Path(__file__).resolve().parents[2]
PROJECT = os.environ.get('ADDP_REDIS_T2_PROJECT', '')
if not PROJECT.startswith('addp-redis-t2-'):
    raise SystemExit('Business Redis tests require the owned T2 gate')
COMPOSE = ['docker', 'compose', '-p', PROJECT, '-f', str(ROOT / 'scripts/test/docker-compose.redis-t2.yml')]


def cli(user, *arguments, data=None, wrong_password=False):
    environment = dict(os.environ)
    command = COMPOSE + ['exec', '-T']
    if user:
        password_key = 'BUSINESS_REDIS_ADMIN_PASSWORD' if user == 'addp_business_admin' else 'BUSINESS_REDIS_READER_PASSWORD'
        environment['REDISCLI_AUTH'] = 'wrong-password' if wrong_password else environment[password_key]
        command += ['-e', 'REDISCLI_AUTH']
    command += ['redis', 'redis-cli', '-e', '--raw']
    if user:
        command += ['--user', user]
    if data is not None:
        command += ['-x']
    return subprocess.run(command + list(arguments), env=environment, input=data,
                          capture_output=True, timeout=15)


class BusinessRedisContract(unittest.TestCase):
    def read(self, *arguments, data=None):
        result = cli('addp_business_reader', *arguments, data=data)
        self.assertEqual(result.returncode, 0, 'read-only Redis command failed')
        return result.stdout

    def test_authentication_and_command_boundaries(self):
        host, port = os.environ['ADDP_REDIS_T2_ENDPOINT'].rsplit(':', 1)
        self.assertEqual(host, '127.0.0.1')
        with socket.create_connection((host, int(port)), timeout=5):
            pass
        self.assertNotEqual(cli(None, 'PING').returncode, 0)
        self.assertNotEqual(cli('addp_business_reader', 'PING', wrong_password=True).returncode, 0)
        self.assertEqual(self.read('PING'), b'PONG\n')
        self.assertIn(b'7.2.13', self.read('HELLO', '3'))
        self.assertEqual(self.read('DBSIZE'), b'9\n')
        for arguments in [('SET', 'addp:sample:write-probe', 'denied'),
                          ('FLUSHALL',), ('EVAL', 'return 42', '0'),
                          ('CONFIG', 'GET', 'maxmemory'), ('ACL', 'LIST')]:
            result = cli('addp_business_reader', *arguments)
            self.assertNotEqual(result.returncode, 0, 'reader unexpectedly allowed an administrative or write command')
            self.assertIn(b'NOPERM', result.stdout + result.stderr)
        self.assertNotEqual(cli('addp_business_reader', 'SELECT', '1').returncode, 0)

    def test_native_types_precision_and_idempotence(self):
        for name, kind in [('string', 'string'), ('counter', 'string'), ('hash', 'hash'),
                           ('list', 'list'), ('set', 'set'), ('zset', 'zset'), ('stream', 'stream')]:
            self.assertEqual(self.read('TYPE', 'addp:sample:' + name), kind.encode() + b'\n')
        self.assertEqual(self.read('GET', 'addp:sample:counter'), b'9007199254740993\n')
        self.assertEqual(self.read('HGET', 'addp:sample:hash', 'order_id'), b'9007199254740993\n')
        self.assertEqual(self.read('LRANGE', 'addp:sample:list', '0', '-1'), b'one\ntwo\n')
        self.assertEqual(self.read('SCARD', 'addp:sample:set'), b'2\n')
        self.assertEqual(self.read('ZRANGE', 'addp:sample:zset', '0', '-1'), b'first\nsecond\n')
        self.assertEqual(self.read('XLEN', 'addp:sample:stream'), b'1\n')

    def test_ttl_and_binary_key_value_are_native(self):
        ttl = int(self.read('PTTL', 'addp:sample:ttl'))
        self.assertGreater(ttl, 0)
        self.assertLessEqual(ttl, 3600000)
        self.assertEqual(self.read('GET', data=b'addp:sample:binary\x00\xff'), b'\x00\xffADDP\n')

    def test_sample_type_conflict_rejects_before_creating_missing_keys(self):
        self.assertEqual(cli('addp_business_admin', 'DEL', 'addp:sample:string').returncode, 0)
        self.assertEqual(cli('addp_business_admin', 'SET', 'addp:sample:hash', 'wrong-type').returncode, 0)
        result = subprocess.run(COMPOSE + ['exec', '-T', 'redis', 'sh', '/addp/redis/init.sh'],
                                capture_output=True, timeout=15)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn(b'unexpected native type', result.stdout + result.stderr)
        self.assertEqual(self.read('EXISTS', 'addp:sample:string'), b'0\n')
        self.assertEqual(self.read('GET', 'addp:sample:hash'), b'wrong-type\n')

    def test_restart_preserves_samples_and_unrelated_data(self):
        result = cli('addp_business_admin', 'SET', 'business:preserve', 'survives-restart')
        self.assertEqual(result.returncode, 0)
        subprocess.run(COMPOSE + ['restart', 'redis'], check=True, capture_output=True, timeout=30)
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if cli('addp_business_admin', 'PING').returncode == 0:
                break
            time.sleep(0.5)
        else:
            self.fail('Redis did not become ready after restart')
        subprocess.run(COMPOSE + ['exec', '-T', 'redis', 'sh', '/addp/redis/init.sh'],
                       check=True, capture_output=True, timeout=15)
        self.assertEqual(self.read('GET', 'business:preserve'), b'survives-restart\n')
        self.assertEqual(self.read('LLEN', 'addp:sample:list'), b'2\n')
        self.assertEqual(self.read('XLEN', 'addp:sample:stream'), b'1\n')


if __name__ == '__main__':
    unittest.main(verbosity=2)
