#!/usr/bin/env python3
"""Pinned R1 Gateway acceptance. Preparation and physical observers do not grant access."""
from __future__ import annotations

import base64
from dataclasses import dataclass
import hashlib
import http.client
from importlib import import_module
import json
import os
import re
from pathlib import Path
import socket
import subprocess
import time
import sys
from urllib.parse import urlencode, urlsplit
from uuid import UUID

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

transport = import_module("scripts.test.redis-core-transport")
CONTRACT = json.loads(Path(__file__).with_name("redis-core-http-contract.json").read_text())
PRODUCT_SHA = CONTRACT["product_sha"]
TOKEN_PATH = "/api/v1/system/oauth/token"
CONTEXT_PATH = "/api/v1/system/auth/context"


class EvidenceError(RuntimeError):
    pass


def require(ok, message):
    if not ok:
        raise EvidenceError(message)


@dataclass
class Response:
    status: int
    payload: object
    request_id: str


def response_request_id(raw):
    # Fixed Gateway's reverse proxy appends the Backend's same request ID to
    # its own response header. Accept repeated identical values, never choose
    # between different IDs or trust a caller-provided external ID.
    if not raw: return ""
    values=[value.strip() for value in raw.split(",")]
    require(len(set(values)) == 1 and str(UUID(values[0])) == values[0], "response request IDs conflict")
    return values[0]


def request(origin, method, path, *, headers=None, body=None, timeout=5):
    parsed = urlsplit(origin)
    require(parsed.scheme == "http" and parsed.hostname in {"127.0.0.1", "localhost"} and not parsed.path,
            "owned loopback origin required")
    connection = http.client.HTTPConnection(parsed.hostname, parsed.port or 80, timeout=timeout)
    try:
        connection.request(method, path, body=body, headers=headers or {})
        response = connection.getresponse()
        raw = response.read(1048577)
        require(len(raw) <= 1048576, "unexpected response size")
        return Response(response.status, json.loads(raw) if raw else None, response_request_id(response.getheader("X-Request-ID", "")))
    finally:
        connection.close()


def oauth(origin, identity, timeout=5):
    auth = base64.b64encode((identity["ADDP_REDIS_HTTP_CLIENT_ID"] + ":" + identity["ADDP_REDIS_HTTP_CLIENT_SECRET"]).encode()).decode()
    return request(origin, "POST", TOKEN_PATH, timeout=timeout,
                   headers={"Authorization": "Basic " + auth, "Content-Type": "application/x-www-form-urlencoded"},
                   body=urlencode({"grant_type": "client_credentials", "scope": "addp.api", "audience": "addp.api",
                                   "tenant_id": identity["ADDP_ONLINE_TEST_TENANT_ID"]}))


def token_projection(context, identity):
    require(isinstance(context, dict) and context.get("schema_version") == "addp.auth_context/v1", "AuthContext schema missing")
    principal, scope, client, token = (context.get(x, {}) for x in ("principal", "context", "client", "token"))
    require(principal.get("type") == "service_principal" and str(principal.get("id")) == identity["ADDP_REDIS_HTTP_ACCOUNT_ID"], "wrong OAuth Principal")
    require(scope.get("type") == "tenant" and str(scope.get("tenant_id")) == identity["ADDP_ONLINE_TEST_TENANT_ID"]
            and scope.get("tenant_membership_id") == identity["ADDP_REDIS_HTTP_MEMBERSHIP_ID"], "wrong OAuth Tenant")
    require(client.get("client_id") == identity["ADDP_REDIS_HTTP_CLIENT_ID"] and client.get("scopes") == [] and client.get("scope_mode") == "unrestricted"
            and client.get("audiences") == ["addp.api"], "wrong OAuth Client constraints")
    require(token.get("type") == "service_access_token" and token.get("issued_at") and token.get("expires_at"), "wrong OAuth Token")
    require(context.get("authorization", {}).get("role_assignments") == [], "OAuth probe gained permission")
    return {key: context[key] for key in ("principal", "context", "client", "token", "authorization")}


class RedisObserver:
    """RESP2 read-only observation; no Python Redis package or shell credential."""
    def __init__(self, endpoint, password):
        self.endpoint = tuple(endpoint)
        self.password = password

    def execute(self, *commands):
        def read(stream):
            line = stream.readline()
            if not line:
                raise EvidenceError("Redis observation closed")
            kind, value = line[:1], line[1:-2]
            if kind == b"+": return value.decode()
            if kind == b":": return int(value)
            if kind == b"$":
                length = int(value)
                if length == -1: return None
                raw = stream.read(length)
                require(len(raw) == length and stream.read(2) == b"\r\n", "invalid Redis observation")
                return raw.decode()
            if kind == b"*": return [read(stream) for _ in range(int(value))]
            raise EvidenceError("Redis observation refused")
        with socket.create_connection(self.endpoint, timeout=2) as connection, connection.makefile("rb") as stream:
            result = []
            for command in (("AUTH", self.password), *commands):
                pieces = [str(x).encode() for x in command]
                connection.sendall(b"*%d\r\n" % len(pieces) + b"".join(b"$%d\r\n" % len(x) + x + b"\r\n" for x in pieces))
                result.append(read(stream))
            return result[1:]

    def sample(self, consumer):
        script = "return {redis.call('GET',KEYS[1]),redis.call('PTTL',KEYS[1]),redis.call('PEXPIRETIME',KEYS[1])}"
        row = self.execute(("EVAL_RO", script, 1, "ratelimit:api-consumer:" + str(consumer)))[0]
        return {"count": int(row[0]) if row[0] is not None else None, "pttl_ms": row[1], "expires_at_ms": row[2]}

    def identity(self):
        text = self.execute(("INFO", "server"))[0]
        return next(line.split(":", 1)[1] for line in text.splitlines() if line.startswith("run_id:"))


def assert_sample(sample, count, expiry=None):
    require(sample["count"] == count and 0 < sample["pttl_ms"] <= 60000 and sample["expires_at_ms"] > 0,
            "original limit window missing or count differs")
    if expiry is not None:
        require(sample["expires_at_ms"] == expiry, "Redis window was reset")
    return sample["expires_at_ms"]


def assert_forwarded(snapshot, owner, response, count):
    require(response.request_id and str(UUID(response.request_id)) == response.request_id, "actual Gateway request ID missing")
    require(snapshot[owner]["valid"], "HTTP counter incomplete")
    row = snapshot[owner]["records"].get(response.request_id)
    if count == 0:
        require(row is None, "rejected request reached upstream")
    else:
        require(row == {"received": 1, "forwarded": 1, "completed": 1, "status": response.status}, "upstream request was not completed exactly once")


def assert_unavailable(response):
    require(response.status == 503 and isinstance(response.payload, dict)
            and response.payload.get("error_code") == "module_not_ready", "wrong Redis outage refusal")


def ready(origin, expected, timeout=2):
    response = request(origin, "GET", "/health/ready", timeout=timeout)
    require(response.status == expected, "wrong readiness HTTP status")
    checks = {x["name"]: x for x in response.payload.get("checks", [])}
    require(checks.get("redis", {}).get("status") == ("ready" if expected == 200 else "not_ready"), "Redis readiness check missing")
    if expected == 503:
        require(checks["redis"].get("error_code") == "redis_unavailable", "wrong Redis readiness machine code")
    original = {"gateway": ("system_registry_snapshot",), "system": ("local_dependencies", "iam_bootstrap")}
    for name in original.get(response.payload.get("module"), ()):
        require(checks.get(name, {}).get("status") == "ready", "original readiness condition changed")
    return response.payload


def run_window(config, identity, original_token, original_context, redis, facts, progress=None):
    """Every accepted business request is issued once; no retry can repair evidence."""
    gateway, system = config["gateway_url"], config["system_url"]
    control = lambda command: transport.control(config["control"], command)
    fault = lambda owner, available: control({"action": "fault", "owner": owner, "available": available})
    snapshot = lambda: control({"action": "snapshot"})
    consumer = int(identity["ADDP_REDIS_HTTP_CONSUMER_ID"])
    key = identity["ADDP_REDIS_HTTP_CONSUMER_KEY"]
    headers = {"X-API-Key": key, "Content-Type": "application/json"}
    payload = json.dumps(config["query_body"])
    before = facts()
    require(redis.sample(consumer)["count"] is None, "Consumer must start without a window")
    redis_identity = redis.identity()
    processes = {name: request(url, "GET", "/health/live").payload for name, url in (("system", system), ("gateway", gateway))}
    started = time.monotonic()
    deadline = started + CONTRACT["window_deadline_seconds"]
    progress = progress if progress is not None else {}
    progress["phase"] = "normal_query"
    stage_started = started
    progress["stage_durations_ms"] = {}

    def stage(name):
        nonlocal stage_started
        now = time.monotonic()
        progress["stage_durations_ms"][progress["phase"]] = round((now-stage_started)*1000)
        progress["phase"], stage_started = name, now


    def remaining():
        value = deadline - time.monotonic()
        require(value > 0, "original window deadline exceeded")
        return min(5, value)

    def query():
        return request(gateway, "POST", config["query_path"], headers=headers, body=payload, timeout=remaining())

    def recovery(origin):
        while True:
            remaining()
            response = request(origin, "GET", "/health/ready", timeout=min(2, remaining()))
            if response.status == 200:
                return ready(origin, 200, min(2, remaining()))
            require(response.status == 503, "unexpected recovery response")
            time.sleep(min(.1, remaining()))

    def query_projection(response):
        require(response.status == 200 and isinstance(response.payload, dict), "normal Query failed")
        value = response.payload
        require(value.get("data") == config["expected_rows"] and value.get("page", {}).get("has_more") is False
                and isinstance(value.get("service_version"), str) and value["service_version"], "normal Query fields/rows differ")
        return {"data": value["data"], "service_version": value["service_version"]}

    try:
        q1 = query()
        baseline_query = query_projection(q1)
        samples = [redis.sample(consumer)]
        expiry = assert_sample(samples[-1], 1)
        assert_forwarded(snapshot(), "service", q1, 1)
        stage("gateway_outage")
        fault("gateway", False)
        ready(gateway, 503, min(2, remaining()))
        ready(system, 200, min(2, remaining()))
        require(request(gateway, "GET", "/health/live", timeout=remaining()).status == 200, "Gateway died")
        qfault, ofault = query(), oauth(gateway, identity, remaining())
        for response in (qfault, ofault): assert_unavailable(response)
        snap = snapshot()
        assert_forwarded(snap, "service", qfault, 0)
        assert_forwarded(snap, "system", ofault, 0)
        samples.append(redis.sample(consumer)); assert_sample(samples[-1], 1, expiry)
        require(facts() == before, "Gateway refusal changed scoped business facts")
        stage("system_outage")
        fault("system", False)
        fault("gateway", True)
        recovery(gateway)
        ready(system, 503, min(2, remaining()))
        require(request(system, "GET", "/health/live", timeout=remaining()).status == 200, "System died")
        sfault = oauth(gateway, identity, remaining()); assert_unavailable(sfault)
        assert_forwarded(snapshot(), "system", sfault, 1)
        samples.append(redis.sample(consumer)); assert_sample(samples[-1], 1, expiry)
        require(facts() == before, "System refusal changed scoped business facts")
        stage("original_token")
        fault("system", True)
        recovery(system); recovery(gateway)
        service = request(config["service_url"], "GET", "/health/ready", timeout=remaining())
        require(service.status == 200 and service.payload.get("module") == "service", "original Service Backend is not ready")
        context = request(gateway, "GET", CONTEXT_PATH, headers={"Authorization": "Bearer " + original_token}, timeout=remaining())
        require(context.status == 200 and token_projection(context.payload, identity) == original_context, "original Token changed or failed")
        assert_forwarded(snapshot(), "system", context, 1)
        require(facts() == before, "original Token or authorization records changed")
        stage("same_window")
        q2 = query()
        require(query_projection(q2) == baseline_query, "recovered Query definition/result differs")
        samples.append(redis.sample(consumer)); assert_sample(samples[-1], 2, expiry)
        q3 = query()
        require(q3.status == 429 and q3.payload == {"error": "Rate limit exceeded", "limit": 2, "window": "1 minute"}, "original quota not enforced")
        samples.append(redis.sample(consumer)); assert_sample(samples[-1], 3, expiry)
        snap = snapshot()
        assert_forwarded(snap, "service", q2, 1); assert_forwarded(snap, "service", q3, 0)
        require(redis.identity() == redis_identity, "Redis process changed")
        for name, url in (("system", system), ("gateway", gateway)):
            require(request(url, "GET", "/health/live", timeout=remaining()).payload == processes[name], "product process identity changed")
        remaining()
        stage("completed")
        responses = (q1, qfault, ofault, sfault, context, q2, q3)
        require(len({x.request_id for x in responses}) == len(responses), "probe request IDs were reused")
        return {"checks": {name: True for name in CONTRACT["checks"] if name != "owner_cleanup"}, "window": samples,
                "elapsed_seconds": time.monotonic() - started, "requests": [
                    {"request_id": x.request_id, "status": x.status,
                     "forwarded_system": snap["system"]["records"].get(x.request_id, {}).get("forwarded", 0),
                     "forwarded_service": snap["service"]["records"].get(x.request_id, {}).get("forwarded", 0)} for x in responses]}
    finally:
        # Recovery is an exit obligation, never a success oracle or business retry.
        try: fault("system", True)
        finally: fault("gateway", True)


def validate_report(report, run_id):
    require(report.get("schema_version") == "addp.online-suite/v1" and report.get("suite") == CONTRACT["suite"]
            and report.get("product_sha") == PRODUCT_SHA and report.get("run_id") == run_id
            and report.get("result") == "passed" and report.get("cleanup") == "passed", "wrong Redis acceptance archive")
    require(isinstance(report.get("harness_sha"), str) and re.fullmatch("[0-9a-f]{40}", report["harness_sha"]), "acceptance source identity missing")
    identities=report.get("identities",{})
    require(set(identities)=={"tenant_id","oauth_principal_id","consumer_id","query_service_id","engine_id"}
            and all(type(value) is int and value>0 for value in identities.values()) and identities["tenant_id"]>1, "acceptance resource bindings missing")
    checks = report.get("checks")
    require(isinstance(checks, dict) and set(checks) == set(CONTRACT["checks"]) and all(value is True for value in checks.values()), "Redis evidence stages incomplete")
    samples = report.get("window", [])
    require(len(samples) == 5, "Redis window evidence incomplete")
    expiry = samples[0]["expires_at_ms"]
    for row, count in zip(samples, (1, 1, 1, 2, 3)): assert_sample(row, count, expiry)
    require(type(report.get("elapsed_seconds")) in (int, float) and 0 <= report["elapsed_seconds"] < CONTRACT["window_deadline_seconds"], "Redis evidence deadline exceeded")
    requests = report.get("requests", [])
    require(len(requests) == 7 and [x.get("status") for x in requests] == [200, 503, 503, 503, 200, 200, 429], "Redis request evidence incomplete")
    require(len({x.get("request_id") for x in requests}) == 7, "Redis request identity reused")
    for row, system, service in zip(requests, (0,0,0,1,1,0,0), (1,0,0,0,0,1,0)):
        require(str(UUID(row["request_id"])) == row["request_id"] and row.get("forwarded_system") == system and row.get("forwarded_service") == service,
                "Redis forwarding evidence differs")


def pinned_product(root):
    require(subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip() == PRODUCT_SHA,
            "wrong pinned product SHA")
    require(not subprocess.check_output(["git", "status", "--porcelain"], cwd=root, text=True).strip(), "product checkout is dirty")


def main():
    # The owner-maintained Hosted preparer writes all private bindings. Merely
    # invoking this observer cannot create a Tenant, alter a Grant or run T4.
    require(os.environ.get("GITHUB_ACTIONS") == "true" and os.environ.get("ADDP_ONLINE_HOSTED") == "1"
            and os.environ.get("ONLINE_SUITE") == CONTRACT["suite"] and os.environ.get("POSTGRES_DB") == "addp_online",
            "isolated Hosted Redis profile required")
    pinned_product(Path.cwd())
    runner = import_module("scripts.test.redis-core-http-preparation")
    runner.accept()


if __name__ == "__main__":
    main()
