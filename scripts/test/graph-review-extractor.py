#!/usr/bin/env python3
"""Controlled extraction transport for the disposable Hosted review fixture only."""
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MATERIAL = "ADDP Hosted review fixture/v1"
REVIEW_IDS = ("approve", "reject", "modify", "batch-before", "bad-native", "batch-after")


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
            payload = extraction(json.loads(self.rfile.read(size)))
        except (ValueError, TypeError, AttributeError):
            return self.reply(400, {})
        self.reply(200, payload)

    def log_message(self, *args):
        pass  # Never log request bodies or credentials.


if __name__ == "__main__":
    if os.environ.get("ADDP_ONLINE_HOSTED") != "1" or os.environ.get("GITHUB_ACTIONS") != "true":
        raise SystemExit("Controlled extraction requires disposable Hosted deployment")
    ThreadingHTTPServer(("0.0.0.0", 8087), Handler).serve_forever()
