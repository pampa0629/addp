"""Local Unix-context Docker transport for deployment-owned lifecycle operations."""
import importlib.util
import json
import subprocess
from pathlib import Path

_spec = importlib.util.spec_from_file_location('addp_deployment_inventory_transport', Path(__file__).with_name('deployment-inventory.py'))
inventory = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(inventory)


class DeploymentError(ValueError):
    """Closed command failure; never propagate raw output or arguments."""


FACT = ('{"id":{{json .Id}},"running":{{json .State.Running}},'
        '"started_at":{{json .State.StartedAt}},'
        '"labels":{{json .Config.Labels}},"networks":{{json .NetworkSettings.Networks}},'
        '"mounts":{{json .Mounts}},"network_mode":{{json .HostConfig.NetworkMode}},'
        '"ports":{{json .HostConfig.PortBindings}}}')


class Docker:
    def __init__(self, env):
        self.env = {key: env[key] for key in ('PATH', 'HOME', 'DOCKER_CONFIG', 'DOCKER_CONTEXT',
                    'LANG', 'LC_ALL', 'TMPDIR') if key in env}
        self.context = None
        _, self.info, _ = inventory.local_docker(env=env, reader=self._discovery)

    def _discovery(self, *args):
        value = self.run(list(args), raw=True)
        if args == ('context', 'show'):
            self.context = value.strip()
        return value.strip()

    def run(self, args, *, input=None, timeout=15, check=True, raw=False, extra_env=None):
        prefix = [] if raw else ['--context', self.context]
        environment = dict(self.env, **(extra_env or {}))
        try:
            result = subprocess.run(['docker', *prefix, *args], input=input, capture_output=True,
                                    text=True, timeout=timeout, env=environment)
        except (OSError, subprocess.SubprocessError):
            raise DeploymentError('Deployment command unavailable or timed out') from None
        if len(result.stdout.encode()) > 1024 * 1024 or len(result.stderr.encode()) > 1024 * 1024:
            raise DeploymentError('Deployment response exceeds budget')
        if check and result.returncode:
            raise DeploymentError('Deployment command failed')
        return result.stdout if check else result

    def fact(self, identity):
        value = json.loads(self.run(['inspect', '--format', FACT, identity]))
        # Docker does not promise mount-array ordering between inspections.
        # Preserve every mount property while comparing the unordered facts.
        value['mounts'] = sorted(value['mounts'], key=lambda item: json.dumps(item, sort_keys=True))
        return value
