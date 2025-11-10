import csv
import json
import os
import random
import argparse
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse


DATA_DIR = "data"
PUBLIC_DIR = "public"
CSV_PATH = os.path.join(DATA_DIR, "data.csv")
# Prefer env PORT, default 8000
DEFAULT_PORT = int(os.getenv("PORT", "8000"))


def ensure_dirs():
    os.makedirs(DATA_DIR, exist_ok=True)
    os.makedirs(PUBLIC_DIR, exist_ok=True)


def generate_csv(path: str):
    rows = [(m, random.randint(1, 10)) for m in range(1, 11)]
    with open(path, "w", newline="", encoding="utf-8") as f:
        w = csv.writer(f)
        w.writerow(["month", "value"])
        for m, v in rows:
            w.writerow([m, v])


def read_csv(path: str):
    data = []
    with open(path, newline="", encoding="utf-8") as f:
        r = csv.DictReader(f)
        for row in r:
            data.append({"month": int(row["month"]), "value": int(row["value"])})
    return data


class Handler(SimpleHTTPRequestHandler):
    def __init__(self, *args, **kwargs):
        # Serve static files from PUBLIC_DIR
        super().__init__(*args, directory=PUBLIC_DIR, **kwargs)

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path == "/api/data":
            self._handle_api_data()
            return
        if parsed.path == "/api/regenerate":
            self._handle_api_regenerate()
            return
        # Fallback to static files
        super().do_GET()

    def _handle_api_data(self):
        try:
            data = read_csv(CSV_PATH)
            payload = json.dumps({"items": data}, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except FileNotFoundError:
            self.send_error(500, "CSV not found")
        except Exception as e:
            self.send_error(500, f"Error reading CSV: {e}")

    def _handle_api_regenerate(self):
        try:
            generate_csv(CSV_PATH)
            payload = json.dumps({"ok": True}, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json; charset=utf-8")
            self.send_header("Cache-Control", "no-store")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
        except Exception as e:
            self.send_error(500, f"Error regenerating CSV: {e}")


def main():
    parser = argparse.ArgumentParser(description="CSV chart demo server")
    parser.add_argument("--port", type=int, default=None, help="Port to listen on")
    args = parser.parse_args()

    ensure_dirs()
    if not os.path.exists(CSV_PATH):
        generate_csv(CSV_PATH)

    # Try preferred/default port first; if busy, fall back to an ephemeral port
    port_to_use = args.port if args.port else DEFAULT_PORT
    httpd = None
    try:
        httpd = ThreadingHTTPServer(("0.0.0.0", port_to_use), Handler)
    except OSError:
        # Any bind error: fall back to an ephemeral port allocated by OS
        httpd = ThreadingHTTPServer(("0.0.0.0", 0), Handler)
        port_to_use = httpd.server_address[1]

    with httpd:
        print(f"Server running: http://localhost:{port_to_use}")
        print("Endpoints: /api/data, /api/regenerate")
        print(f"CSV: {CSV_PATH}")
        httpd.serve_forever()


if __name__ == "__main__":
    main()



