#!/usr/bin/env python3
"""Own the synthetic graph database for the Hosted Graph protection suite."""
import argparse
import json
import os
import platform
import secrets
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
PROJECT = "addp-graph-online"
CONTAINER = "addp-graph-online-falkordb"
GRAPHS = ("graph_protection_fixture", "graph_protection_control")


def command(arguments, environment=None):
    result = subprocess.run(arguments, env=environment, capture_output=True, text=True, timeout=180)
    if result.returncode:
        raise RuntimeError("Graph fixture Docker operation failed")
    return result.stdout.strip()


def admission():
    expected = {"GITHUB_ACTIONS": "true", "RUNNER_OS": "Linux", "ADDP_ONLINE_HOSTED": "1", "ADDP_ONLINE_HOST": "1"}
    if any(os.environ.get(key) != value for key, value in expected.items()) or platform.system() != "Linux" or platform.machine() != "x86_64":
        raise RuntimeError("Graph fixture requires disposable Hosted Linux x86_64")
    secret_dir = Path(os.environ["ADDP_ONLINE_SECRET_DIR"]).resolve()
    if ROOT == secret_dir or ROOT in secret_dir.parents or not secret_dir.is_dir():
        raise RuntimeError("Graph fixture secret directory must exist outside checkout")
    return secret_dir


def compose(arguments, password):
    environment = dict(os.environ, INFRA_FALKORDB_PASSWORD=password)
    return command(["docker", "compose", "--env-file", "/dev/null", "-p", PROJECT, "-f",
                    str(ROOT / "business/docker-compose.online-graph-falkordb.yml"), *arguments], environment)


def native(graph, query, readonly=True):
    response = json.loads(command(["docker", "exec", CONTAINER, "redis-cli", "--json",
                                   "GRAPH.RO_QUERY" if readonly else "GRAPH.QUERY", graph, query]))
    if not isinstance(response, list):
        raise RuntimeError("Graph fixture native query failed")
    return response


def owned_resources(kind):
    return command(["docker", kind, "ls", "-q", *(["-a"] if kind == "container" else []),
                    "--filter", "label=com.docker.compose.project=" + PROJECT]).splitlines()


def start(secret_dir):
    if any(owned_resources(kind) for kind in ("container", "network", "volume")):
        raise RuntimeError("Refusing existing Graph fixture resources")
    descriptor = secret_dir / "graph-falkordb.json"
    if descriptor.exists():
        raise RuntimeError("Refusing existing Graph fixture descriptor")
    password = secrets.token_hex(32)
    compose(["up", "-d", "--wait", "--wait-timeout", "90"], password)
    bindings = json.loads(command(["docker", "inspect", "--format", "{{json .HostConfig.PortBindings}}", CONTAINER]))
    if bindings:
        raise RuntimeError("Graph fixture must not publish host ports")
    for graph in GRAPHS:
        native(graph, "CREATE (a:Person {id:'p1', name:'Fixture one'}), (b:Person {id:'p2', name:'Fixture two'}), (c:Person {id:'p3', name:'Fixture three'}), (a)-[:Link]->(b), (b)-[:Link]->(c)", False)
        if native(graph, "MATCH (n) RETURN count(n)")[1] != [[3]] or native(graph, "MATCH ()-[r]->() RETURN count(r)")[1] != [[2]]:
            raise RuntimeError("Synthetic graph must contain exactly 3 nodes and 2 relationships")
    data = {"name": "Hosted synthetic FalkorDB", "engine_type": "falkordb", "engine_origin": "general",
            "description": "Disposable Graph permission/protection fixture",
            "connection_info": {"host": CONTAINER, "port": 6379,
                                "user": "default", "password": password, "database": 0}}
    with os.fdopen(os.open(descriptor, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
        json.dump(data, output)
    print("Synthetic Graph fixture ready: two graphs, six nodes, four relationships")


def stop(secret_dir):
    # Never remove an unowned container that happens to use the fixed fixture name.
    names = command(["docker", "ps", "-a", "--filter", "name=^/" + CONTAINER + "$", "--format", "{{.Names}}"])
    if names:
        owner = command(["docker", "inspect", "--format", '{{ index .Config.Labels "com.docker.compose.project" }}', CONTAINER])
        if owner != PROJECT:
            raise RuntimeError("Graph fixture container ownership mismatch")
    compose(["down", "--volumes", "--remove-orphans"], "cleanup-only")
    if any(owned_resources(kind) for kind in ("container", "network", "volume")):
        raise RuntimeError("Graph fixture cleanup left owned resources")
    (secret_dir / "graph-falkordb.json").unlink(missing_ok=True)
    print("Synthetic Graph fixture cleanup: zero containers, networks, volumes")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=["start", "stop"])
    args = parser.parse_args()
    secret_dir = admission()
    (start if args.operation == "start" else stop)(secret_dir)


if __name__ == "__main__":
    main()
