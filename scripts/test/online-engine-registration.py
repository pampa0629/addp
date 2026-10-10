#!/usr/bin/env python3
"""Register one disposable external-profile Online Engine through the System API."""

from __future__ import annotations

import argparse
import json
import os
import stat
import sys
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path
from typing import Callable, Mapping


class RegistrationError(RuntimeError):
    pass


SOURCE_INITIALIZER_PERMISSIONS = {
    'system.engine_access_approval_requirement.initialize',
    'system.engine_access_delegation.create',
    'system.engine_access_grant.create',
}


def initialize_exact_record_read_grants(authorizer, consumer, tenant_id, targets, *, initialize_approval=True, item_term="table"):
    """Prepare precise reads with separate Users and the formal System command."""
    def identity(client):
        context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
        if not isinstance(context, dict):
            raise RegistrationError('source Grant preparation requires a current User AuthContext')
        principal, scope = context.get('principal', {}), context.get('context', {})
        if (principal.get('type') != 'user' or scope.get('type') != 'tenant'
            or scope.get('tenant_id') != str(tenant_id) or not principal.get('id')):
            raise RegistrationError('source Grant preparation requires current Users in the disposable Tenant')
        return context, str(principal['id'])
    if not isinstance(tenant_id, int) or tenant_id <= 1 or not targets or len(targets) > 200:
        raise RegistrationError('source Grant preparation requires a nondefault Tenant and exact record datasets')
    if item_term not in ("table", "collection"):
        raise RegistrationError("source Grant requires a table or collection catalog term")
    for engine_id, namespace_term, namespace, table in targets:
        if (not isinstance(engine_id, int) or engine_id <= 0 or namespace_term not in (('database',) if item_term == 'collection' else ('schema', 'database'))
            or not isinstance(namespace, str) or not namespace or not isinstance(table, str) or not table):
            raise RegistrationError('source Grant preparation requires an exact ordinary record target')
    current, initializer_id = identity(authorizer)
    _, recipient_id = identity(consumer)
    permissions = {permission for role in current.get('authorization', {}).get('role_assignments', [])
                   for permission in role.get('permissions', [])}
    if initializer_id == recipient_id or permissions != SOURCE_INITIALIZER_PERMISSIONS:
        raise RegistrationError('source initializer must be separate and have only its three preparation permissions')
    receipts = []
    for engine_id, namespace_term, namespace, table in targets:
        body = {
            'request_id': str(uuid.uuid4()),
            'catalog_path': {'version': 'catalog.path/v1', 'engine_id': engine_id, 'segments': [
                {'term': 'server', 'kind': 'server', 'name': ''},
                {'term': namespace_term, 'kind': 'namespace', 'name': namespace},
                {'term': item_term, 'kind': item_term, 'name': table},
            ]},
            'requirement_version': '1', 'initialize_approval': initialize_approval,
            'recipient_type': 'user', 'recipient_id': recipient_id,
            'action': 'read', 'expiry_mode': 'until_revoked',
            'reason': 'Disposable Online exact record read acceptance',
        }
        check_path = '/api/v1/system/engine-access/read-checks/manager-preview'
        check_body = {'targets': [body['catalog_path']]}
        consumer.request('POST', check_path, (403,), check_body)
        issued = authorizer.request('POST', f'/api/v1/system/engines/{engine_id}/access_grants', (201,), body).payload
        expected = {key: body[key] for key in (
            'request_id', 'catalog_path', 'requirement_version', 'recipient_type', 'recipient_id', 'action', 'expiry_mode')}
        expected.update(engine_id=str(engine_id), approval_mode='independent', revocation=None)
        if not isinstance(issued, dict) or any(issued.get(key) != value for key, value in expected.items()):
            raise RegistrationError('source Grant receipt differs from its exact consumer and record dataset')
        observed = consumer.request('POST', check_path, (200,), check_body).payload
        if not isinstance(observed, dict) or not observed.get('observed_at'):
            raise RegistrationError('source Grant did not produce a current read observation')
        receipts.append(issued)
    return receipts


def require_external_environment(environment: Mapping[str, str]) -> tuple[str, str]:
    profiles = {
        name
        for name in ("ADDP_ONLINE_HOSTED", "ADDP_ONLINE_OWNER_MANAGED")
        if environment.get(name) == "1"
    }
    if environment.get("GITHUB_ACTIONS") != "true" or environment.get("RUNNER_OS") != "Linux" or len(profiles) != 1:
        raise RegistrationError(
            "Engine registration requires exactly one GitHub Hosted or owner-managed Linux Online profile"
        )
    system_url = environment.get("SYSTEM_URL", "").rstrip("/")
    parsed = urllib.parse.urlsplit(system_url)
    if parsed.scheme != "http" or parsed.hostname not in {"127.0.0.1", "localhost"}:
        raise RegistrationError("SYSTEM_URL must be a loopback HTTP endpoint")
    token = environment.get("ADDP_ONLINE_FIXTURE_ENGINE_ACCESS_TOKEN", "")
    if not token:
        raise RegistrationError("Engine provisioner access token is required")
    return system_url, token


def load_descriptor(path: Path) -> dict[str, object]:
    if not path.is_absolute() or not path.is_file():
        raise RegistrationError("Engine descriptor must be an existing absolute path")
    if stat.S_IMODE(path.stat().st_mode) & 0o077:
        raise RegistrationError("Engine descriptor must be owner-only")
    try:
        raw = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as error:
        raise RegistrationError("Engine descriptor is not valid JSON") from error
    if not isinstance(raw, dict):
        raise RegistrationError("Engine descriptor must be a JSON object")
    required = {
        "name",
        "engine_type",
        "engine_origin",
        "connection_info",
        "description",
    }
    if set(raw) != required:
        raise RegistrationError("Engine descriptor must use the canonical create fields")
    if not isinstance(raw.get("name"), str) or not raw["name"]:
        raise RegistrationError("Engine descriptor name is required")
    if not isinstance(raw.get("engine_type"), str) or not raw["engine_type"]:
        raise RegistrationError("Engine descriptor engine_type is required")
    if raw.get("engine_origin") != "general":
        raise RegistrationError("External Online profiles only register general Engines")
    if not isinstance(raw.get("connection_info"), dict) or not raw["connection_info"]:
        raise RegistrationError("Engine descriptor connection_info is required")
    return raw


def request_json(
    base_url: str,
    token: str,
    method: str,
    path: str,
    body: dict[str, object] | None,
    expected_statuses: tuple[int, ...],
) -> dict[str, object]:
    encoded = None if body is None else json.dumps(body).encode()
    headers = {"Authorization": f"Bearer {token}"}
    if encoded is not None:
        headers["Content-Type"] = "application/json"
    request = urllib.request.Request(
        base_url + path, data=encoded, headers=headers, method=method
    )
    try:
        with urllib.request.urlopen(request, timeout=30) as response:
            if response.status not in expected_statuses:
                raise RegistrationError(
                    f"{method} {path} returned HTTP {response.status}"
                )
            raw = response.read()
    except urllib.error.HTTPError as error:
        raise RegistrationError(
            f"{method} {path} returned HTTP {error.code}"
        ) from error
    except urllib.error.URLError as error:
        raise RegistrationError(f"{method} {path} could not reach System") from error
    if not raw:
        return {}
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError as error:
        raise RegistrationError(f"{method} {path} did not return valid JSON") from error
    if not isinstance(parsed, dict):
        raise RegistrationError(f"{method} {path} did not return a JSON object")
    return parsed


def register_engine(
    base_url: str,
    token: str,
    descriptor: dict[str, object],
    requester: Callable[..., dict[str, object]] = request_json,
) -> int:
    created = requester(
        base_url,
        token,
        "POST",
        "/api/v1/system/engines",
        descriptor,
        (200, 201),
    )
    engine = created.get("data", created)
    if not isinstance(engine, dict):
        raise RegistrationError("System did not return an Engine object")
    engine_id = engine.get("id")
    if not isinstance(engine_id, int) or engine_id <= 0:
        raise RegistrationError("System did not return a positive Engine ID")
    if engine.get("engine_type") != descriptor["engine_type"]:
        raise RegistrationError("System returned a different Engine type")
    tested = requester(
        base_url,
        token,
        "POST",
        f"/api/v1/system/engines/{engine_id}/test",
        None,
        (200,),
    )
    test_result = tested.get("data", tested)
    if not isinstance(test_result, dict) or test_result.get("success") is not True:
        detail = str(test_result.get("error", "missing successful probe")) if isinstance(test_result, dict) else "invalid probe response"
        # Connection errors may contain a DSN. Redact every configured string,
        # including nested credentials and URL-encoded values, before logging.
        def strings(value):
            if isinstance(value, str) and value:
                yield value
            elif isinstance(value, dict):
                for item in value.values():
                    yield from strings(item)
            elif isinstance(value, list):
                for item in value:
                    yield from strings(item)
        values = {variant for value in strings(descriptor["connection_info"])
                  for variant in (value, urllib.parse.quote(value, safe=""), urllib.parse.quote_plus(value),
                                  json.dumps(value, ensure_ascii=True)[1:-1])}
        for value in sorted(values, key=len, reverse=True):
            detail = detail.replace(value, "<configured>")
        detail = " ".join(detail.split())[:384]
        raise RegistrationError(f"System Engine connection test did not succeed (type={descriptor['engine_type']}, id={engine_id}): {detail}")
    return engine_id


def write_result(path: Path, engine_id: int) -> None:
    if not path.is_absolute():
        raise RegistrationError("Engine result path must be absolute")
    path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    temporary = path.with_name(path.name + ".tmp")
    descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        os.fchmod(descriptor, 0o600)
        os.write(
            descriptor,
            f"export ADDP_ONLINE_CONSUMER_ENGINE_ID='{engine_id}'\n".encode(),
        )
    finally:
        os.close(descriptor)
    temporary.replace(path)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--descriptor", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.descriptor.resolve() == args.output.resolve():
        raise RegistrationError("Engine descriptor and result paths must be different")
    base_url, token = require_external_environment(os.environ)
    descriptor = load_descriptor(args.descriptor)
    engine_id = register_engine(base_url, token, descriptor)
    write_result(args.output, engine_id)
    print(
        f"External Online Engine registered: type={descriptor['engine_type']}, id={engine_id}"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except RegistrationError as error:
        print(f"External Online Engine registration failed: {error}", file=sys.stderr)
        raise SystemExit(1)
