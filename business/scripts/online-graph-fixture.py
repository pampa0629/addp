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
GRAPHS = ("graph_protection_fixture", "graph_protection_control", "graph_review_fixture")

# Complete directed-by-order graph, plus reverse, parallel and self relations.
DENSE_NODES = {f"n{i+1:02}": labels for i, labels in enumerate(
    [["Person"]] * 3 + [["Employee", "Person"]] * 3 + [["Organization"]] * 3 + [["Organization", "Partner"]] * 3)}
DENSE_EDGES = [(source, target, "Link") for i, source in enumerate(DENSE_NODES)
               for target in list(DENSE_NODES)[i+1:]] + [("n02", "n01", "Link"), ("n01", "n02", "Parallel"), ("n12", "n12", "Link"), ("n01", "n12", "Parallel"), ("n04", "n09", "Parallel"), ("n05", "n08", "Parallel")]


def exploration(secret_dir):
    # This phase runs only after the old control and review acceptance has finished.
    # A foreign, changed or missing control fixture must never be overwritten.
    descriptor = secret_dir / ("graph-control.json" if engine_type() == "neo4j" else "graph-target.json")
    data = json.loads(descriptor.read_text())
    container = containers()[1 if engine_type() == "neo4j" else 0]
    if data.get("engine_type") != engine_type() or data.get("connection_info", {}).get("host") != container:
        raise RuntimeError("Dense fixture descriptor ownership mismatch")
    owner = command(["docker", "inspect", "--format", '{{ index .Config.Labels "com.docker.compose.project" }}', container])
    if owner != PROJECT:
        raise RuntimeError("Dense fixture container ownership mismatch")
    graph = GRAPHS[1]
    if (native(graph, "MATCH (n) RETURN count(n)") != 3 or
        native(graph, "MATCH ()-[r]->() RETURN count(r)") != 2 or
        native(graph, "MATCH (n:Person) WHERE n.id IN ['p1','p2','p3'] RETURN count(n)") != 3):
        raise RuntimeError("Dense phase refuses changed control fixture")
    native(graph, "MATCH (n) DETACH DELETE n", False)
    nodes = [f"({key}:{':'.join(labels)} {{id:'{key}', name:'Dense {key[1:]}'}})" for key, labels in DENSE_NODES.items()]
    edges = [f"({source})-[:{kind}]->({target})" for source, target, kind in DENSE_EDGES]
    native(graph, "CREATE " + ", ".join(nodes + edges), False)
    if native(graph, "MATCH (n) RETURN count(n)") != len(DENSE_NODES) or native(graph, "MATCH ()-[r]->() RETURN count(r)") != len(DENSE_EDGES):
        raise RuntimeError("Dense fixture native counts differ")
    print("Synthetic Graph dense fixture ready: 12 nodes, 4 label sets, 72 relationships")


def command(arguments, environment=None, stdin=None):
    result = subprocess.run(arguments, env=environment, input=stdin, capture_output=True, text=True, timeout=300)
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


def engine_type():
    value = os.environ.get("ADDP_ONLINE_GRAPH_ENGINE")
    if value not in {"falkordb", "neo4j"}:
        raise RuntimeError("Graph fixture requires an explicit falkordb or neo4j engine")
    return value


def containers():
    return [CONTAINER] if engine_type() == "falkordb" else ["addp-graph-online-neo4j", "addp-graph-online-neo4j-control", "addp-graph-online-neo4j-review"]


def compose(arguments, password):
    environment = dict(os.environ, INFRA_FALKORDB_PASSWORD=password, NEO4J_T2_PASSWORD=password)
    return command(["docker", "compose", "--env-file", "/dev/null", "-p", PROJECT, "-f",
                    str(ROOT / "business/docker-compose.online-graph.yml"), "--profile", engine_type(), *arguments], environment)


def native(graph, query, readonly=True):
    if engine_type() == "neo4j":
        container = containers()[GRAPHS.index(graph)]
        result = command(["docker", "exec", "-i", container, "sh", "-ec",
                          'exec cypher-shell -u neo4j -p "${NEO4J_AUTH#neo4j/}" --format plain --non-interactive'], stdin=query.rstrip(";") + ";\n")
        return int(result.splitlines()[-1]) if "RETURN count(" in query else None
    response = json.loads(command(["docker", "exec", CONTAINER, "redis-cli", "--json",
                                   "GRAPH.RO_QUERY" if readonly else "GRAPH.QUERY", graph, query]))
    if not isinstance(response, list):
        raise RuntimeError("Graph fixture native query failed")
    return response[1][0][0] if "RETURN count(" in query else None


def owned_resources(kind):
    return command(["docker", kind, "ls", "-q", *(["-a"] if kind == "container" else []),
                    "--filter", "label=com.docker.compose.project=" + PROJECT]).splitlines()


def start(secret_dir):
    if any(owned_resources(kind) for kind in ("container", "network", "volume")):
        raise RuntimeError("Refusing existing Graph fixture resources")
    if any((secret_dir / name).exists() for name in ("graph-target.json", "graph-control.json", "graph-review.json")):
        raise RuntimeError("Refusing existing Graph fixture descriptor")
    password = secrets.token_hex(32)
    compose(["up", "-d", "--wait", "--wait-timeout", "240"], password)
    for container in containers():
        bindings = json.loads(command(["docker", "inspect", "--format", "{{json .HostConfig.PortBindings}}", container]))
        if bindings:
            raise RuntimeError("Graph fixture must not publish host ports")
    for graph in GRAPHS:
        if graph == "graph_review_fixture":
            native(graph, "CREATE (:Person {id:'p1', name:'Review endpoint one'}), (:Person {id:'p2', name:'Review endpoint two'})", False)
            if native(graph, "MATCH (n) RETURN count(n)") != 2 or native(graph, "MATCH ()-[r]->() RETURN count(r)") != 0:
                raise RuntimeError("Review graph must start with exactly two endpoints and no relationships")
            continue
        native(graph, "CREATE (a:Person {id:'p1', name:'Fixture one'}), (b:Person {id:'p2', name:'Fixture two'}), (c:Person {id:'p3', name:'Fixture three'}), (a)-[:Link]->(b), (b)-[:Link]->(c)", False)
        if native(graph, "MATCH (n) RETURN count(n)") != 3 or native(graph, "MATCH ()-[r]->() RETURN count(r)") != 2:
            raise RuntimeError("Synthetic graph must contain exactly 3 nodes and 2 relationships")
    for index, role in enumerate(("target", "control", "review") if engine_type() == "neo4j" else ("target",)):
        data = {"name": "Hosted synthetic " + ("FalkorDB" if engine_type() == "falkordb" else "Neo4j " + role),
                "engine_type": engine_type(), "engine_origin": "general",
                "description": "Disposable Graph permission/protection fixture",
                "connection_info": {"host": containers()[index], "port": 6379 if engine_type() == "falkordb" else 7687,
                                    "user": "default" if engine_type() == "falkordb" else "neo4j", "password": password,
                                    "database": 0 if engine_type() == "falkordb" else "neo4j"}}
        descriptor = secret_dir / ("graph-" + role + ".json")
        with os.fdopen(os.open(descriptor, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), "w") as output:
            json.dump(data, output)
    print("Synthetic Graph fixture ready: three graphs, eight nodes, four relationships")


def stop(secret_dir):
    # Never remove an unowned container that happens to use the fixed fixture name.
    for container in containers():
        names = command(["docker", "ps", "-a", "--filter", "name=^/" + container + "$", "--format", "{{.Names}}"])
        if names:
            owner = command(["docker", "inspect", "--format", '{{ index .Config.Labels "com.docker.compose.project" }}', container])
            if owner != PROJECT:
                raise RuntimeError("Graph fixture container ownership mismatch")
    compose(["down", "--volumes", "--remove-orphans"], "cleanup-only")
    if any(owned_resources(kind) for kind in ("container", "network", "volume")):
        raise RuntimeError("Graph fixture cleanup left owned resources")
    for role in ("target", "control", "review"):
        (secret_dir / ("graph-" + role + ".json")).unlink(missing_ok=True)
    print("Synthetic Graph fixture cleanup: zero containers, networks, volumes")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("operation", choices=["start", "stop", "exploration"])
    args = parser.parse_args()
    engine_type()
    secret_dir = admission()
    {"start": start, "stop": stop, "exploration": exploration}[args.operation](secret_dir)


if __name__ == "__main__":
    main()
