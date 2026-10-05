"""Pinned, complete ES connector JAR shared by native startup, builds and gates."""
import hashlib
from pathlib import Path
import sys
import tempfile
import threading
from urllib.request import urlopen

ES_JAR_NAME = "elasticsearch-spark-30_2.12-9.5.4.jar"
ES_JAR_URL = "https://repo.maven.apache.org/maven2/org/elasticsearch/elasticsearch-spark-30_2.12/9.5.4/" + ES_JAR_NAME
ES_JAR_SHA256 = "ba21a7a39cc5133b150087a9d283f2b18ae14a76ffb2fec7c31e26d04d8d8641"
_lock = threading.Lock()


def ensure_elasticsearch_jar(directory=None):
    directory = Path(directory) if directory is not None else Path(sys.prefix) / "share" / "addp-spark"
    with _lock:
        directory.mkdir(parents=True, exist_ok=True)
        target = directory / ES_JAR_NAME
        if target.exists():
            if hashlib.sha256(target.read_bytes()).hexdigest() != ES_JAR_SHA256:
                raise ValueError("Elasticsearch connector JAR checksum mismatch")
            return target.resolve()
        with urlopen(ES_JAR_URL, timeout=30) as response:
            data = response.read(16 * 1024 * 1024 + 1)
        if hashlib.sha256(data).hexdigest() != ES_JAR_SHA256:
            raise ValueError("Elasticsearch connector JAR checksum mismatch")
        with tempfile.NamedTemporaryFile(dir=directory, suffix=".jar", delete=False) as temporary:
            temporary.write(data)
            temporary_path = Path(temporary.name)
        try:
            temporary_path.replace(target)
        finally:
            temporary_path.unlink(missing_ok=True)
        return target.resolve()
