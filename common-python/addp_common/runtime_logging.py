"""Single structured application output; launchers own files and instance scope."""
import json
import logging
import sys
from datetime import datetime, timezone
from urllib.parse import urlsplit


class RoutineHTTPFilter(logging.Filter):
    """Keep successful routine HTTP exchanges at DEBUG without hiding failures."""

    def filter(self, record: logging.LogRecord) -> bool:
        if (record.name != "httpx" or record.levelno != logging.INFO
                or record.msg != 'HTTP Request: %s %s "%s %d %s"'
                or not isinstance(record.args, tuple) or len(record.args) != 5):
            return True
        method, url, _, status, _ = record.args
        if not isinstance(status, int) or not 200 <= status < 300:
            return True
        path = urlsplit(str(url)).path
        routine = (method, path) in {
            ("POST", "/api/v1/system/runtime/modules/heartbeat"),
            ("POST", "/api/v1/system/oauth/token"),
            ("GET", "/health/live"),
            ("GET", "/health/ready"),
        }
        if not routine:
            return True
        record.levelno = logging.DEBUG
        record.levelname = "DEBUG"
        return logging.getLogger(record.name).isEnabledFor(logging.DEBUG)

class RuntimeFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        value = {"time": datetime.fromtimestamp(record.created, timezone.utc).isoformat(),
                 "level": record.levelname.lower(), "logger": record.name,
                 "message": record.getMessage()}
        if record.exc_info:
            value["stack"] = self.formatException(record.exc_info)
        return json.dumps(value, ensure_ascii=False)

def setup_runtime_logging(level: int = logging.INFO) -> None:
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(RuntimeFormatter())
    handler.addFilter(RoutineHTTPFilter())
    root = logging.getLogger()
    for old in root.handlers[:]:
        root.removeHandler(old)
        old.close()
    root.addHandler(handler)
    root.setLevel(level)

    # Uvicorn configures independent text handlers before importing ASGI apps.
    # Route its records through the same structured handler as application logs.
    for name in ("uvicorn", "uvicorn.error", "uvicorn.access"):
        server_logger = logging.getLogger(name)
        for old in server_logger.handlers[:]:
            server_logger.removeHandler(old)
            old.close()
        server_logger.propagate = True
        server_logger.disabled = False
        server_logger.setLevel(level)
