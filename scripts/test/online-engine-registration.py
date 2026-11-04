#!/usr/bin/env python3
"""Register one disposable external-profile Online Engine through the System API."""

from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import re
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


SOURCE_INITIALIZER_ROLE = 'tenant.engine_access_delegation_administrator'
SOURCE_INITIALIZER_PERMISSIONS = {
    'iam.department.read',
    'iam.project_group.read',
    'iam.tenant_membership.read',
    'system.engine.read',
    'system.engine_access_delegation.create',
    'system.engine_access_delegation.read',
    'system.engine_access_delegation.revoke',
    'system.engine_access_grant.create',
    'system.engine_access_grant.read',
    'system.engine_access_grant.revoke',
    'system.engine_catalog.read',
}


def read_exact_record_authorization_facts(authorizer, path):
    """Read System's trusted Meta projection; a path alone never proves ownership."""
    engine_id = path['engine_id']
    full_name = path['segments'][1]['name'] + '.' + path['segments'][2]['name']
    fingerprint = hashlib.sha256(f'{engine_id}:{full_name}'.encode()).hexdigest()
    facts = authorizer.request('GET', f'/api/v1/system/engines/{engine_id}/data-items/{fingerprint}/authorization-facts', (200,)).payload
    expected = {'schema_version': 'meta.data_item_authorization_facts/v1',
                'engine_id': str(engine_id), 'item_fingerprint': fingerprint,
                'item_type': path['segments'][2]['term'], 'full_name': full_name, 'basis_state': 'complete',
                'boundary': {'layout': 'single', 'native_target': path}}
    if (not isinstance(facts, dict) or any(facts.get(key) != value for key, value in expected.items())
        or not isinstance(facts.get('boundary_hash'), str) or not re.fullmatch('[0-9a-f]{64}', facts['boundary_hash'])
        or not facts.get('observed_at')):
        raise RegistrationError('source Grant requires a complete matching System authorization boundary')
    return facts


def source_grant_participants(authorizer, consumer, tenant_id):
    """Verify separate tenant Users; preparation confers no consumer authority."""
    if type(tenant_id) is not int or tenant_id <= 1:
        raise RegistrationError('source Grant preparation requires a disposable nondefault Tenant')
    def identity(client):
        context = client.request('GET', '/api/v1/system/auth/context', (200,)).payload
        if not isinstance(context, dict):
            raise RegistrationError('source Grant preparation requires a current User AuthContext')
        principal, scope = context.get('principal', {}), context.get('context', {})
        principal_id = principal.get('id')
        if (principal.get('type') != 'user' or scope.get('type') != 'tenant'
            or scope.get('tenant_id') != str(tenant_id) or not isinstance(principal_id, str)
            or not re.fullmatch('[1-9][0-9]*', principal_id)):
            raise RegistrationError('source Grant preparation requires current Users in the disposable Tenant')
        return context, principal_id
    current, initializer_id = identity(authorizer)
    _, recipient_id = identity(consumer)
    roles = current.get('authorization', {}).get('role_assignments', [])
    permissions = {permission for role in roles for permission in role.get('permissions', [])}
    if (initializer_id == recipient_id or permissions != SOURCE_INITIALIZER_PERMISSIONS
        or not any(role.get('role_key') == SOURCE_INITIALIZER_ROLE for role in roles)):
        raise RegistrationError('source initializer must be separate and have the exact formal engine authorization administrator permissions')
    return initializer_id, recipient_id


class ExactFileMetadataGrants:
    """Existing metadata-only Grant contract; never a content completeness proof."""
    def __init__(self, authorizer, consumer, tenant_id, *, on_change=None):
        self.operator, self.recipient = source_grant_participants(authorizer, consumer, tenant_id)
        self.authorizer, self.consumer = authorizer, consumer
        self.pending = []
        self.on_change = on_change

    def checkpoint(self):
        if self.on_change is not None:
            self.on_change(self.pending)

    @staticmethod
    def target(engine, full_name, storage, *, layout='single'):
        parts = full_name.split('/') if isinstance(full_name, str) else []
        if (type(engine) is not int or engine <= 0 or storage not in {'nfs', 'minio', 'hdfs'} or not parts
            or len(parts) > 63 or any(not p or p in {'.', '..'} or any(c in p for c in '\\:\x00') for p in parts)
            or (storage == 'minio' and len(parts) < 2) or layout not in {'single', 'multi', 'whole'}
            or (layout == 'whole' and storage != 'minio')):
            raise RegistrationError('metadata Grant requires an exact file or object target')
        if storage == 'minio':
            segments = [dict(term='service', kind='service', name=''), dict(term='bucket', kind='bucket', name=parts[0])]
            segments += [dict(term='prefix', kind='prefix', name=p) for p in (parts[1:] if layout == 'whole' else parts[1:-1])]
            if layout != 'whole':
                segments += [dict(term='object', kind='object', name=parts[-1])]
        else:
            segments = [dict(term='root', kind='root', name='')]
            segments += [dict(term='directory', kind='directory', name=p) for p in parts[:-1]]
            segments += [dict(term='file', kind='file', name=parts[-1])]
        return dict(version='catalog.path/v1', engine_id=engine, segments=segments)

    def observe(self, target, visible):
        result = self.consumer.request('POST', '/api/v1/system/engine-access/metadata-checks/meta-query',
                                       (200,), {'targets': [target]}).payload
        if (not isinstance(result, dict) or not result.get('observed_at')
            or result.get('targets') != [dict(target=target, visible=visible)]):
            raise RegistrationError('metadata visibility does not match the current precise Grant')

    def grant(self, engine, full_name, storage, *, data_type, format_name, layout, expires_at=None):
        if expires_at is not None:
            try:
                remaining = (datetime.fromisoformat(expires_at.replace('Z', '+00:00')) - datetime.now(timezone.utc)).total_seconds()
                if not 0 < remaining <= 10800: raise ValueError()
            except (ValueError, TypeError, AttributeError):
                raise RegistrationError('metadata Grant expiry must be within the next 10800 seconds') from None
        target = self.target(engine, full_name, storage, layout=layout)
        fingerprint = hashlib.sha256(f'{engine}:{full_name}'.encode()).hexdigest()
        facts = self.authorizer.request('GET', f'/api/v1/system/engines/{engine}/data-items/{fingerprint}/authorization-facts', (200,)).payload
        boundary = facts.get('boundary') if isinstance(facts, dict) else None
        metadata = boundary.get('metadata_target') if isinstance(boundary, dict) else None
        if not isinstance(metadata, dict):
            raise RegistrationError('metadata Grant requires a complete matching metadata-only boundary')
        expected_metadata = dict(target=target, layout=layout, data_type=data_type, format=format_name,
                                 composition_digest=metadata.get('composition_digest'))
        if (not isinstance(facts, dict) or any(facts.get(k) != v for k,v in {
            'schema_version': 'meta.data_item_authorization_facts/v1', 'engine_id': str(engine),
            'item_fingerprint': fingerprint, 'item_type': 'object' if storage == 'minio' else 'file', 'full_name': full_name,
            'basis_state': 'complete', 'boundary': dict(layout=layout, metadata_target=expected_metadata)}.items())
            or not re.fullmatch('[0-9a-f]{64}', str(metadata.get('composition_digest', '')))
            or not re.fullmatch('[0-9a-f]{64}', str(facts.get('boundary_hash', ''))) or not facts.get('observed_at')):
            raise RegistrationError('metadata Grant requires complete matching System metadata-only facts')
        self.observe(target, False)
        command = str(uuid.uuid4())
        body = dict(request_id=command, catalog_path=target, item_fingerprint=fingerprint,
                    expected_boundary_hash=facts['boundary_hash'], recipient_type='user', recipient_id=self.recipient,
                    action='metadata_read', expiry_mode='until_revoked', reason='Disposable Online exact structural metadata acceptance')
        if expires_at is not None:
            body.update(expiry_mode='at_time', expires_at=expires_at)
        pending = dict(engine=engine, request_id=command, target=target, confirmed=False)
        self.pending.append(pending)  # Unknown write outcomes must also be cleaned.
        self.checkpoint()
        receipt = self.authorizer.request('POST', f'/api/v1/system/engines/{engine}/access_grants', (201,), body).payload
        expected = {k:body[k] for k in ('request_id','catalog_path','item_fingerprint','recipient_type','recipient_id','action','expiry_mode')}
        expected.update(engine_id=str(engine), boundary_hash=facts['boundary_hash'], metadata_target=metadata,
                        approval_mode='independent', operator_principal_id=self.operator, revocation=None)
        if expires_at is None:
            expected['expires_at'] = None
        else:
            try:
                if datetime.fromisoformat(receipt['expires_at'].replace('Z', '+00:00')) != datetime.fromisoformat(expires_at.replace('Z', '+00:00')):
                    raise ValueError()
            except (KeyError, TypeError, ValueError):
                raise RegistrationError('metadata Grant receipt changed its expiry') from None
        if (not isinstance(receipt, dict) or any(receipt.get(k) != v for k,v in expected.items())
            or receipt.get('file_composition') or receipt.get('native_target') or receipt.get('scope')
            or receipt.get('node_recursive') is not None):
            raise RegistrationError('metadata Grant receipt changed its precise metadata-only boundary')
        pending['confirmed'] = True
        self.checkpoint()
        self.observe(target, True)
        return receipt

    def cleanup(self):
        errors = []
        for row in list(self.pending):
            try:
                self.authorizer.request('POST', f"/api/v1/system/engines/{row['engine']}/access_grants/{row['request_id']}/revoke",
                    (200,) if row['confirmed'] else (200,404), {'reason':'Disposable Online metadata cleanup'})
                self.observe(row['target'], False)
                self.pending.remove(row)
                self.checkpoint()
            except Exception:
                errors.append(row['request_id'])
        if errors:
            raise RegistrationError('owned metadata Grant cleanup or withdrawal observation failed')
        return {'grants_revoked': True, 'metadata_hidden': True}


def initialize_exact_record_read_grants(authorizer, consumer, tenant_id, targets, *, item_term="table"):
    """Prepare precise reads with separate Users and the formal System command."""
    if not isinstance(tenant_id, int) or tenant_id <= 1 or not targets or len(targets) > 200:
        raise RegistrationError('source Grant preparation requires a nondefault Tenant and exact record datasets')
    if item_term not in ("table", "collection"):
        raise RegistrationError("source Grant requires a table or collection catalog term")
    for engine_id, namespace_term, namespace, table in targets:
        if (not isinstance(engine_id, int) or engine_id <= 0 or namespace_term not in (('database',) if item_term == 'collection' else ('schema', 'database'))
            or not isinstance(namespace, str) or not namespace or not isinstance(table, str) or not table):
            raise RegistrationError('source Grant preparation requires an exact ordinary record target')
    _, recipient_id = source_grant_participants(authorizer, consumer, tenant_id)
    receipts = []
    for engine_id, namespace_term, namespace, table in targets:
        body = {
            'request_id': str(uuid.uuid4()),
            'catalog_path': {'version': 'catalog.path/v1', 'engine_id': engine_id, 'segments': [
                {'term': 'server', 'kind': 'server', 'name': ''},
                {'term': namespace_term, 'kind': 'namespace', 'name': namespace},
                {'term': item_term, 'kind': item_term, 'name': table},
            ]},
            'recipient_type': 'user', 'recipient_id': recipient_id,
            'action': 'read', 'expiry_mode': 'until_revoked',
            'reason': 'Disposable Online exact record read acceptance',
        }
        # System reads the persisted Meta identity/boundary under its own
        # Service credential and rechecks this administrator's qualification.
        # A recipient without a Grant must not browse Meta to bootstrap a Grant.
        facts = read_exact_record_authorization_facts(authorizer, body['catalog_path'])
        fingerprint = facts['item_fingerprint']
        body.update(item_fingerprint=fingerprint, expected_boundary_hash=facts['boundary_hash'])
        check_path = '/api/v1/system/engine-access/read-checks/manager-preview'
        check_body = {'targets': [body['catalog_path']]}
        consumer.request('POST', check_path, (403,), check_body)
        issued = authorizer.request('POST', f'/api/v1/system/engines/{engine_id}/access_grants', (201,), body).payload
        expected = {key: body[key] for key in (
            'request_id', 'catalog_path', 'recipient_type', 'recipient_id', 'action', 'expiry_mode')}
        expected.update(engine_id=str(engine_id), approval_mode='independent', revocation=None,
                        item_fingerprint=fingerprint, boundary_hash=facts['boundary_hash'])
        if not isinstance(issued, dict) or any(issued.get(key) != value for key, value in expected.items()):
            raise RegistrationError('source Grant receipt differs from its exact consumer and record dataset')
        observed = consumer.request('POST', check_path, (200,), check_body).payload
        if not isinstance(observed, dict) or not observed.get('observed_at'):
            raise RegistrationError('source Grant did not produce a current read observation')
        # Now the recipient must see the same persisted identity through its
        # ordinary, permission-filtered catalog. Do not substitute preparer IO.
        full_name = namespace + '.' + table
        items = consumer.request('GET', f'/api/v1/meta/engines/{engine_id}/items?branch=' + urllib.parse.quote(namespace, safe=''), (200,)).payload
        if not isinstance(items, list):
            raise RegistrationError('source Grant requires a scanned Meta item list')
        matches = [item for item in items if isinstance(item, dict) and item.get('full_name') == full_name and item.get('item_type') == item_term]
        if len(matches) != 1 or matches[0].get('fingerprint') != fingerprint:
            raise RegistrationError('source Grant requires one matching persisted DataItem identity')
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
