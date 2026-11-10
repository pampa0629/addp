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
FAILURE_REASONS = {
    'source-layout-changed': '采集来源部署分组已变更；请显式执行 up 应用当前来源',
    'source-image-unavailable': '原采集镜像不可用；请通过标准指标启动入口重新构建并应用来源',
    'node-proof-missing': '主机安装证明缺失；请核对后显式执行 up 重新应用',
    'node-proof-disabled': '主机安装已停用；如需重新启用，请显式执行 up',
    'node-proof-invalid': '主机安装证明读取或校验失败；请核对私有安装文件的归属、保护及完整性',
    'node-source-incomplete': '主机来源组件不完整；请核对后显式执行 up 重新应用',
    'node-ownership-rejected': '主机资源归属校验失败；请核对当前工作区及来源容器、容量卷的归属',
    'node-inputs-changed': '主机部署输入已变化（配置、证书、模板或 Docker 环境）；请核对后显式执行 up 重新应用',
    'node-installation-changed': '主机安装身份或安全配置已变化；请核对后显式执行 up 重新应用',
}
FAILURE_STAGES = {
    'session-check': '核对采集会话阶段失败；请核对既有采集会话及账号状态',
    'transport-check': '校验采集端口与 TLS 阶段失败；请核对监听地址、端口、证书及私钥保护',
    'private-state': '核对私有安装状态阶段失败；请核对当前工作区、安装记录及文件保护',
    'authentication-read': '读取 PostgreSQL 认证规则阶段失败；请核对受管认证文件及容器内可读性',
    'account-install': '安装采集账号阶段失败；请核对数据库管理连接及既有采集账号状态',
    'authentication-update': '更新 PostgreSQL 认证规则阶段失败；请核对受管文件的读写、发布及重载状态',
    'account-enable': '启用采集账号阶段失败；请核对账号及认证规则的实际状态',
    'connection-check': '验证采集连接阶段失败；请核对采集账号、认证配置及引擎可达性',
    'source-files': '准备采集来源文件阶段失败；请核对私有目录保护、可写性及证书文件',
    'identity-recheck': '重新核对引擎身份阶段失败；请核对引擎容器是否发生变化',
    'source-start': '部署采集来源阶段失败；请核对来源配置、私有卷、镜像及容器实际状态',
    'source-health': '检查来源容器运行状态阶段失败；请核对来源组件及退出状态，运行中仍不等于采集成功',
    'deployment-check': '核对引擎部署阶段失败；请核对引擎运行状态、归属及部署管理连接',
    'account-plan': '准备采集账号配置阶段失败；请核对已安装镜像及私有账号记录',
    'account-update': '核对及更新采集账号阶段失败；请核对账号归属、权限、持久记录及实际启停状态',
    'source-check': '核验既有采集来源阶段失败；请核对安装记录、账号、证书及来源归属',
    'source-stop': '移除采集来源阶段失败；请核对来源容器及专属卷的归属和实际状态',
    'source-build': '构建采集来源镜像阶段失败；请核对构建输入、Docker 状态及构建依赖的可达性',
}
TLS_FAILURES = {
    'chain': '证书链或必要扩展不符合严格要求；请核对签发 CA 和证书扩展',
    'not-yet-valid': '证书尚未生效；请核对证书有效期与系统时间',
    'expired': '证书已过期；请更新证书并核对系统时间',
    'purpose': '证书用途不符合服务器认证要求；请核对服务器用途',
    'name': '证书名称与监听 IP 不匹配；请核对服务器 IP SAN',
    'key': '证书与私钥不匹配；请核对是否使用同一组证书和私钥',
    'unknown': '证书校验失败，原因未能确认；请核对证书材料和校验工具',
}


def tls_failure_detail(stage, reasons):
    if stage == 'transport-check' and len(reasons) == 1 and isinstance(reasons[0],str) and reasons[0] in TLS_FAILURES:
        return '校验采集证书阶段失败；'+TLS_FAILURES[reasons[0]]
    return None


def load(path):
    spec = importlib.util.spec_from_file_location('metrics_manager_'+path.stem.replace('-', '_'), path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def environment(scope, inherited, root=None):
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
        config = (ROOT if root is None else root) / ('business/.env' if scope == 'business' else '.env')
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
    group = deployment.inventory.metrics_project
    project = group.PROJECT
    namespace = 'io.addp.'+('node' if kind == 'node' else 'postgres' if kind == 'postgresql' else kind)+'-metrics'
    service = group.service('postgres' if kind == 'postgresql' else kind, scope)
    expected = {service}
    if kind == 'postgresql':
        expected.add(service+'-web')
    if kind == 'node' and env.get('ADDP_NODE_BLOCK_CAPACITY_ENABLED', 'false') == 'true':
        expected.add('block-capacity')
    ids = group.container_ids(docker.run,project,expected)
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
        cleanup = '；清理未确认' if '[cleanup=failed]' in result.stderr else ''
        reason = re.search(r'\[reason=([a-z-]+)\]', result.stderr)
        if reason and reason[1] in FAILURE_REASONS:
            command = ('./scripts/infra/host-metrics.sh up' if kind == 'node'
                       else f'python3 scripts/infra/metrics-sources.py up --scope {scope} --kind {kind}')
            raise ValueError('操作失败：'+FAILURE_REASONS[reason[1]]+cleanup+'；命令：'+command)
        stage = re.search(r'\[stage=([a-z-]+)\]', result.stderr)
        tls = tls_failure_detail(stage[1] if stage else None, re.findall(r'\[tls-reason=([a-z-]+)\]', result.stderr))
        detail = '：'+tls if tls else ('：'+FAILURE_STAGES[stage[1]]) if stage and stage[1] in FAILURE_STAGES else ''
        raise ValueError('操作失败'+detail+cleanup)
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
            elif action == 'recover' and kind == 'node' and 'host' not in scopes:
                result = '未选择恢复（主机须显式指定 --scope host）'
            elif action == 'up' and kind == 'node' and not selected:
                result = '未启用（未改变现有来源）'
            else:
                result = apply(action, kind, scope, env)
                if action == 'up' and not selected:
                    result = '未启用（已核对停用）'
            print(f'{label:<24} {result}')
        except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
            failures += 1
            tls = tls_failure_detail(getattr(error,'observation_stage',None), [getattr(error,'source_tls_reason',None)]) if action == 'check' else None
            if isinstance(error, subprocess.TimeoutExpired):
                detail = '操作超时，结果未知；请核对状态，勿自动重试'
            elif tls:
                detail = tls
            elif type(error) is ValueError and str(error).startswith(('操作失败', '配置读取失败', '尚不支持')):
                detail = str(error)
            else:
                detail = '检查或操作未通过，请核对部署配置与来源归属'
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
