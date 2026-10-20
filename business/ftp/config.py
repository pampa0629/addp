"""FTP deployment facts; Compose owns the pinned official image."""
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]


def image():
    matches = re.findall(r'^  ftp:\n    image: (drakkan/sftpgo:[^\s]+@sha256:[a-f0-9]{64})$',
                         (ROOT / 'business/docker-compose.yml').read_text(), re.MULTILINE)
    if len(matches) != 1:
        raise ValueError('exactly one pinned FTP image required')
    return matches[0]


def server(passive_start, passive_end, tls_mode=1):
    if not 1024 <= passive_start <= passive_end <= 65535:
        raise ValueError('invalid matching passive port range')
    return {
        'common': {'idle_timeout': 2},
        'sftpd': {'bindings': [{'port': 0}]},
        'httpd': {'bindings': [{'port': 0}]},
        'webdavd': {'bindings': [{'port': 0}]},
        'telemetry': {'bind_port': 0},
        'data_provider': {'driver': 'sqlite', 'name': '/var/lib/sftpgo/sftpgo.db'},
        'ftpd': {
            'bindings': [{'port': 2121, 'address': '', 'tls_mode': tls_mode,
                          'min_tls_version': 12, 'passive_connections_security': 0}],
            'passive_port_range': {'start': passive_start, 'end': passive_end},
            'disable_active_mode': True,
            'certificate_file': '/addp/config/server.crt',
            'certificate_key_file': '/addp/config/server.key',
        },
    }


def users(username, password):
    return {'version': 16, 'users': [{
        'username': username, 'password': password, 'status': 1,
        'home_dir': '/srv/sftpgo/data', 'permissions': {'/': ['list', 'download']},
        'filesystem': {'provider': 0},
    }]}
