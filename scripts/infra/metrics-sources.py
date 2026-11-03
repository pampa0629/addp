#!/usr/bin/env python3
"""One deployment-owned entry for optional metrics sources; never manages engines."""
import argparse
import importlib.util
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
PROVIDERS = {
    'node': ('node-metrics.py', 'ADDP_NODE_METRICS', '主机'),
    'postgresql': ('postgres-observation/lifecycle.py', 'ADDP_POSTGRES_METRICS', 'PostgreSQL'),
    'redis': ('redis-metrics.py', 'ADDP_REDIS_METRICS', 'Redis'),
    'minio': ('minio-observation.py', 'ADDP_MINIO_METRICS', 'MinIO'),
}
SCOPES = ('host', 'infra', 'business')
OS_KEYS = ('PATH', 'HOME', 'LANG', 'LC_ALL', 'TMPDIR', 'DOCKER_CONFIG', 'DOCKER_CONTEXT', 'DOCKER_HOST')


def load(path):
    spec = importlib.util.spec_from_file_location('metrics_manager_'+path.stem.replace('-', '_'), path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def environment(scope, inherited):
    """Load only the selected deployment's trusted shell configuration, privately."""
    base = {key: inherited[key] for key in OS_KEYS if key in inherited}
    if inherited.get('ADDP_ONLINE_HOST') == '1':
        filename = inherited.get('ADDP_ONLINE_ENV_FILE', '')
        if not filename or not Path(filename).is_absolute() or not Path(filename).is_file():
            raise ValueError('Hosted environment unavailable')
        base['ADDP_ONLINE_HOST'] = '1'
        base['ADDP_ONLINE_ENV_FILE'] = filename
        config = Path(filename)
    else:
        config = ROOT / ('business/.env' if scope == 'business' else '.env')
    if not config.exists():
        raise ValueError('Deployment configuration is missing')
    result = subprocess.run(['bash', '-eu', '-c', 'set -a; source "$1" >&2; env -0',
                             'metrics-sources', str(config)], env=base, cwd=config.parent, capture_output=True, timeout=10)
    if result.returncode or max(len(result.stdout), len(result.stderr)) > 1024 * 1024:
        raise ValueError('Deployment configuration rejected')
    return dict(value.decode().split('=', 1) for value in result.stdout.split(b'\0') if value)


def selection(scopes, kinds):
    return [(scope, kind) for scope in SCOPES if not scopes or scope in scopes
            for kind in PROVIDERS if (scope == 'host') == (kind == 'node') and (not kinds or kind in kinds)]


def enabled(kind, env):
    value = env.get(PROVIDERS[kind][1]+'_ENABLED', 'false')
    if value not in ('true', 'false'):
        raise ValueError('Invalid source selection')
    return value == 'true'


def check(kind, scope, env):
    provider = load(ROOT/'scripts/infra'/PROVIDERS[kind][0])
    if kind == 'node':
        provider.deployment(env)
        provider.capacity_enabled(env)
    else:
        provider.state_directory(env, scope)
        provider.transport(env) if kind != 'minio' else provider.sources.transport(env, ROOT, PROVIDERS[kind][1], provider.Error)


def status(kind, scope, env):
    """Metadata only: container presence cannot prove successful scraping."""
    deployment = load(ROOT/'scripts/utils/deployment-docker.py')
    docker = deployment.Docker(env)
    project = ('addp-node-metrics' if kind == 'node' else 'addp-postgres-metrics-'+scope if kind == 'postgresql'
               else deployment.inventory.engine_metrics_project(kind, scope))
    namespace = 'io.addp.'+('node' if kind == 'node' else 'postgres' if kind == 'postgresql' else kind)+'-metrics'
    service = {'node': 'node-exporter', 'postgresql': 'postgres-metrics'}.get(kind, kind+'-metrics')
    expected = {service}
    if kind == 'postgresql':
        expected.add('postgres-metrics-web')
    if kind == 'node' and env.get('ADDP_NODE_BLOCK_CAPACITY_ENABLED', 'false') == 'true':
        expected.add('block-capacity')
    ids = docker.run(['ps', '-aq', '--filter', 'label=com.docker.compose.project='+project]).split()
    if not ids:
        return '未部署', False
    facts = [docker.fact(identity) for identity in ids]
    for fact in facts:
        labels = fact['labels'] or {}
        if (labels.get(namespace+'.owner') != str(ROOT) or labels.get('com.docker.compose.project') != project
                or kind != 'node' and labels.get(namespace+'.scope') != scope):
            return '归属不符', False
        if kind != 'node':
            identity = labels.get(namespace+'.database-id', '')
            if not re.fullmatch('[0-9a-f]{64}', identity):
                return '来源身份无效', False
            database = docker.fact(identity)
            db_labels = database['labels'] or {}
            source_project, working = deployment.inventory.SCOPES[scope]
            if (not database['running'] or database['started_at'] != labels.get(namespace+'.database-started-at')
                    or db_labels.get('com.docker.compose.project') != source_project
                    or db_labels.get('com.docker.compose.project.working_dir') != str(working)
                    or db_labels.get('com.docker.compose.service') != ('postgres' if kind == 'postgresql' else kind)):
                return '数据库运行身份已变化', False
    services = [fact['labels'].get('com.docker.compose.service') for fact in facts]
    if len(services) != len(expected) or set(services) != expected:
        return '组件不完整', False
    running = all(fact['running'] for fact in facts)
    return ('运行中' if running else '已停止'), running


def apply(action, kind, scope, env):
    script = ROOT/'scripts/infra'/PROVIDERS[kind][0]
    args = [sys.executable, str(script), action]
    if kind != 'node':
        args += ['--scope', scope]
    result = subprocess.run(args, env=env, capture_output=True, text=True, timeout=600)
    # Provider logs may contain deployment information. Publish closed stages only.
    if result.returncode:
        stage = re.search(r'\[stage=([a-z-]+)\]', result.stderr)
        detail = ('；阶段 '+stage[1]) if stage and stage[1] in {
            'session-check', 'transport-check', 'private-state', 'authentication-read', 'account-install',
            'authentication-update', 'account-enable', 'connection-check', 'source-files', 'identity-recheck',
            'source-start', 'source-health'} else ''
        raise ValueError('操作失败'+detail+('；清理未确认' if '[cleanup=failed]' in result.stderr else ''))
    if action == 'recover':
        if 'healthy; source unchanged' in result.stdout:
            return '来源运行正常，未重建（采集待验证）'
        if 'not installed' in result.stdout:
            return '未安装，未执行恢复'
        return '已检查／恢复（采集待验证）'
    return '已停用' if action == 'down' else '已应用（采集待验证）'


def run(action, scopes=(), kinds=(), inherited=None, resolved=False):
    if resolved and (not scopes or len(set(scopes)) != 1):
        raise ValueError('Resolved deployment environment requires one explicit scope')
    rows = selection(scopes, kinds)
    if not rows:
        raise ValueError('No matching source')
    print('ADDP 采集来源 · '+{'status':'状态', 'check':'配置检查', 'up':'安装／应用', 'down':'停用', 'recover':'既有来源恢复'}[action])
    failures = 0
    environments = {}
    for scope, kind in rows:
        label = PROVIDERS[kind][2] if scope == 'host' else scope.capitalize()+' '+PROVIDERS[kind][2]
        try:
            if scope not in environments:
                try:
                    current = inherited if inherited is not None else os.environ
                    environments[scope] = dict(current) if resolved else environment(scope, current)
                except (OSError, ValueError, subprocess.SubprocessError):
                    environments[scope] = None
            env = environments[scope]
            if env is None:
                raise ValueError('配置读取失败')
            # Shutdown must remain available even if the enable flag is invalid.
            selected = enabled(kind, env) if action != 'down' else False
            if action == 'status':
                state, running = status(kind, scope, env)
                bad = selected != running or selected and state != '运行中' or not selected and state != '未部署'
                failures += bool(bad)
                result = ('启用 / ' if selected else '停用 / ')+state
            elif action == 'check':
                if selected:
                    check(kind, scope, env)
                result = '部署输入检查通过（采集待验证）' if selected else '未启用'
            elif action == 'recover' and not selected:
                result = '未选择恢复'
            elif action == 'recover' and kind != 'postgresql':
                raise ValueError('尚不支持无账号修改恢复；请显式执行 up')
            elif action == 'up' and kind == 'node' and not selected:
                result = '未启用（未改变现有来源）'
            else:
                result = apply(action, kind, scope, env)
                if action == 'up' and not selected:
                    result = '未启用（已核对停用）'
            print(f'{label:<24} {result}')
        except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
            failures += 1
            detail = ('操作超时，结果未知；请核对状态，勿自动重试' if isinstance(error, subprocess.TimeoutExpired)
                      else str(error) if type(error) is ValueError and str(error).startswith(('操作失败', '配置读取失败', '尚不支持'))
                      else '检查或操作未通过，请核对部署配置与来源归属')
            print(f'{label:<24} 失败：{detail}')
    print(f'汇总：共 {len(rows)} 项，失败 {failures} 项。来源运行状态不代表实际采集成功，请在 Monitor 核对。')
    return int(bool(failures))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', nargs='?', default='status', choices=('status', 'check', 'up', 'down', 'recover'))
    parser.add_argument('--scope', action='append', choices=SCOPES)
    parser.add_argument('--kind', action='append', choices=tuple(PROVIDERS))
    parser.add_argument('--resolved-env', action='store_true', help='Use the standard deployment caller environment; requires exactly one explicit scope')
    args = parser.parse_args()
    if not selection(args.scope, args.kind):
        parser.error('No matching source for scope/kind')
    if args.resolved_env and (not args.scope or len(set(args.scope)) != 1):
        parser.error('Resolved environment requires one explicit deployment scope')
    return run(args.action, args.scope, args.kind, resolved=args.resolved_env)


if __name__ == '__main__':
    sys.exit(main())
