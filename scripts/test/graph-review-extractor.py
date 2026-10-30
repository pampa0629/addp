#!/usr/bin/env python3
"""Controlled extraction transport for the disposable Hosted review fixture only."""
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import threading
import uuid

MATERIAL = "ADDP Hosted review fixture/v1"
REVIEW_IDS = ("approve", "reject", "modify", "batch-before", "bad-native", "batch-after")
LIFECYCLE_MATERIAL = 'ADDP Hosted execution fixture/v1/'


class LifecycleCalls:
    """Bounded fixture barriers; only loopback controls may arm/release calls."""
    def __init__(self):
        self.lock = threading.Lock()
        self.calls = {}

    def control(self, action, key):
        if str(uuid.UUID(key)) != key or uuid.UUID(key).int == 0:
            raise ValueError('invalid fixture call')
        with self.lock:
            if action == 'arm':
                if key in self.calls or len(self.calls) >= 4:
                    raise ValueError('duplicate or excessive fixture barrier')
                self.calls[key] = {'release': threading.Event(), 'entered': 0, 'returned': 0}
            elif action == 'release':
                self.calls[key]['release'].set()
            elif action != 'read':
                raise ValueError('unsupported fixture control')
            row = self.calls[key]
            return {'entered': row['entered'], 'returned': row['returned'], 'released': row['release'].is_set()}

    def extract(self, key):
        with self.lock:
            row = self.calls[key]
            if row['entered'] != 0:
                raise ValueError('fixture extraction replayed')
            row['entered'] += 1
        if not row['release'].wait(180):
            raise ValueError('fixture extraction release timed out')
        with self.lock:
            row['returned'] += 1
        return {'entities': [], 'relations': []}


LIFECYCLE = LifecycleCalls()


def extraction(body):
    if body.get("text", "").strip() != MATERIAL:
        raise ValueError("unexpected fixture material")
    entities = []
    for key in ("p1", "p2", *REVIEW_IDS):
        properties = {"id": key, "name": key}
        if key == "bad-native":
            properties["unsupported"] = {"nested": "native graph properties cannot store maps"}
        entities.append({"temp_id": key, "type": "Person", "properties": properties,
                         "confidence": 0.99 if key in {"p1", "p2"} else 0.4, "source_text": key})
    return {"entities": entities, "relations": [{"type": "Link", "source_temp_id": "p1", "target_temp_id": "p2",
             "properties": {}, "confidence": 0.4, "source_text": "review-link"}]}


class Handler(BaseHTTPRequestHandler):
    def reply(self, status, payload):
        data = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        self.reply(200 if self.path == "/health/ready" else 404, {})

    def do_POST(self):
        if self.path.startswith('/fixture/'):
            if (self.client_address[0] != '127.0.0.1'
                    or os.environ.get('ADDP_ONLINE_GRAPH_CASE') != 'execution-lifecycle'):
                return self.reply(403, {})
            try:
                size = int(self.headers.get('Content-Length', '0'))
                if not 0 < size <= 1024:
                    raise ValueError('invalid control size')
                body = json.loads(self.rfile.read(size))
                if set(body) != {'key'}:
                    raise ValueError('invalid control scope')
                value = LIFECYCLE.control(self.path.removeprefix('/fixture/'), body['key'])
            except (ValueError, TypeError, KeyError):
                return self.reply(400, {})
            return self.reply(200, value)
        if self.path != "/api/v1/copilot/kg-build/extract":
            return self.reply(404, {})
        # Issuance uses real System OAuth. This controlled transport does not
        # claim to verify Copilot's production Service authentication boundary.
        if not self.headers.get("Authorization", "").startswith("Bearer "):
            return self.reply(401, {})
        try:
            size = int(self.headers.get("Content-Length", "0"))
            if not 0 < size <= 65536:
                raise ValueError("invalid size")
            body = json.loads(self.rfile.read(size))
            text = body.get('text', '').strip()
            if text.startswith(LIFECYCLE_MATERIAL) and os.environ.get('ADDP_ONLINE_GRAPH_CASE') == 'execution-lifecycle':
                payload = LIFECYCLE.extract(text.removeprefix(LIFECYCLE_MATERIAL))
            else:
                payload = extraction(body)
        except (ValueError, TypeError, AttributeError, KeyError):
            return self.reply(400, {})
        self.reply(200, payload)

    def log_message(self, *args):
        pass  # Never log request bodies or credentials.


if __name__ == "__main__":
    if os.environ.get("ADDP_ONLINE_HOSTED") != "1" or os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("Controlled extraction requires disposable Hosted deployment")
    ThreadingHTTPServer(("0.0.0.0", 8087), Handler).serve_forever()
