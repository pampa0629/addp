"""Build-time JVM dialect artifact; queries only verify/load it, never compile."""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import zipfile

SOURCE = Path(__file__).with_name('jvm') / 'ExactIdentifierDialects.java'
JAR_NAME = 'addp-spark-query-dialect.jar'


def _inputs(spark_home=None):
    if spark_home is None:
        import pyspark
        if pyspark.__version__ != '3.5.0':
            raise ValueError('query dialect requires Spark 3.5.0')
        from pyspark.find_spark_home import _find_spark_home
        spark_home = Path(_find_spark_home())
    jars = sorted((Path(spark_home) / 'jars').glob('*.jar'))
    if not jars or not any(jar.name == 'spark-sql_2.12-3.5.0.jar' for jar in jars):
        raise ValueError('query dialect requires fixed Spark 3.5.0 jars')
    digest = hashlib.sha256(SOURCE.read_bytes())
    for jar in jars:
        digest.update(jar.name.encode() + b'\0')
        digest.update(hashlib.sha256(jar.read_bytes()).digest())
    return jars, digest.hexdigest()


def artifact_path():
    return Path(sys.prefix) / 'share' / 'addp-spark' / JAR_NAME


def verified_artifact(spark_home=None, output=None):
    target = Path(output) if output is not None else artifact_path()
    _, fingerprint = _inputs(spark_home)
    try:
        manifest = json.loads(target.with_suffix('.json').read_text())
        if manifest != {'inputs': fingerprint, 'sha256': hashlib.sha256(target.read_bytes()).hexdigest(), 'release': 11}:
            raise ValueError()
    except (OSError, ValueError):
        raise ValueError('query dialect missing or stale; run the standard Spark preparation/build') from None
    return target.resolve()


def build(spark_home=None, output=None):
    target = Path(output) if output is not None else artifact_path()
    jars, fingerprint = _inputs(spark_home)
    try:
        return verified_artifact(spark_home, target)
    except ValueError:
        pass
    java_home = os.environ.get('JAVA_HOME')
    compiler = str(Path(java_home) / 'bin' / 'javac') if java_home else 'javac'
    target.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='addp-spark-dialect-build-') as temporary:
        classes = Path(temporary) / 'classes'
        classes.mkdir()
        subprocess.run([compiler, '--release', '11', '-encoding', 'UTF-8', '-cp', os.pathsep.join(map(str, jars)), '-d', str(classes), str(SOURCE)], check=True, timeout=60)
        data = Path(temporary) / JAR_NAME
        with zipfile.ZipFile(data, 'w', compression=zipfile.ZIP_DEFLATED) as archive:
            for path in sorted(classes.rglob('*.class')):
                entry = zipfile.ZipInfo(path.relative_to(classes).as_posix(), date_time=(1980, 1, 1, 0, 0, 0))
                entry.external_attr = 0o644 << 16
                archive.writestr(entry, path.read_bytes())
        manifest = {'inputs': fingerprint, 'sha256': hashlib.sha256(data.read_bytes()).hexdigest(), 'release': 11}
        # Publish a complete artifact; an interruption between replacements is
        # rejected by verification, never served as a partially built helper.
        for destination, content in ((target, data.read_bytes()), (target.with_suffix('.json'), json.dumps(manifest, sort_keys=True).encode())):
            with tempfile.NamedTemporaryFile(dir=target.parent, delete=False) as stream:
                stream.write(content)
                pending = Path(stream.name)
            try:
                pending.replace(destination)
            finally:
                pending.unlink(missing_ok=True)
    return verified_artifact(spark_home, target)


if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('operation', choices=['build', 'verify'])
    parser.add_argument('--spark-home')
    parser.add_argument('--output')
    args = parser.parse_args()
    result = build(args.spark_home, args.output) if args.operation == 'build' else verified_artifact(args.spark_home, args.output)
    print(result)
