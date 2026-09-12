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
from pathlib import Path
from typing import Callable, Mapping


class RegistrationError(RuntimeError):
    pass


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
        raise RegistrationError("System Engine connection test did not succeed")
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
