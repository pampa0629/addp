"""Spark submission dependencies shared by native startup, builds and gates."""
import hashlib
import os
from pathlib import Path
import shlex
import sys
import tempfile
import threading
from urllib.request import urlopen

ES_JAR_NAME = "elasticsearch-spark-30_2.12-9.5.4.jar"
ES_JAR_URL = "https://repo.maven.apache.org/maven2/org/elasticsearch/elasticsearch-spark-30_2.12/9.5.4/" + ES_JAR_NAME
ES_JAR_SHA256 = "ba21a7a39cc5133b150087a9d283f2b18ae14a76ffb2fec7c31e26d04d8d8641"
_lock = threading.Lock()

SPARK_MAVEN_PACKAGES = ",".join([
    "org.apache.sedona:sedona-spark-shaded-3.5_2.12:1.5.3",
    "org.datasyslab:geotools-wrapper:1.5.3-28.2",
    "org.postgresql:postgresql:42.7.4",
    "com.mysql:mysql-connector-j:8.4.0",
])


def configure_spark_dependencies(builder, directory=None):
    """Merge local JARs at submit time, preserving Spark's resolved Maven JARs.

    Setting spark.jars on the Python builder overwrites the full list resolved
    by SparkSubmit: Driver classes remain available but Executors lose them.
    Normalize inherited submit JAR declarations into one --jars argument.
    """
    connector = str(ensure_elasticsearch_jar(directory))
    with _lock:
        arguments = shlex.split(os.environ.get('PYSPARK_SUBMIT_ARGS', 'pyspark-shell'))
        if not arguments or arguments[-1] != 'pyspark-shell':
            raise ValueError('PYSPARK_SUBMIT_ARGS must end with pyspark-shell')
        remaining, jars = [], []
        position = 0
        while position < len(arguments) - 1:
            argument = arguments[position]
            value = None
            if argument in ('--jars', '--conf'):
                if position + 1 >= len(arguments) - 1:
                    raise ValueError('Spark submit option is missing its value')
                next_argument = arguments[position + 1]
                if argument == '--jars':
                    value = next_argument
                elif next_argument.startswith('spark.jars='):
                    value = next_argument.removeprefix('spark.jars=')
                else:
                    remaining.extend((argument, next_argument))
                position += 2
            else:
                if argument.startswith('--jars='):
                    value = argument.removeprefix('--jars=')
                elif argument.startswith('--conf=spark.jars='):
                    value = argument.removeprefix('--conf=spark.jars=')
                else:
                    remaining.append(argument)
                position += 1
            if value is not None:
                jars.extend(value.split(','))
        jars = list(dict.fromkeys([*jars, connector]))
        os.environ['PYSPARK_SUBMIT_ARGS'] = shlex.join([
            *remaining, '--jars', ','.join(jars), 'pyspark-shell',
        ])
    return (builder.config('spark.jars.packages', SPARK_MAVEN_PACKAGES)
            .config('spark.sql.extensions', 'org.apache.sedona.sql.SedonaSqlExtensions')
            .config('spark.serializer', 'org.apache.spark.serializer.KryoSerializer')
            .config('spark.kryo.registrator', 'org.apache.sedona.core.serde.SedonaKryoRegistrator'))


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
