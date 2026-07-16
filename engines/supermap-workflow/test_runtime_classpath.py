from pathlib import Path


RUN_SH = (Path(__file__).parent / "run.sh").read_text(encoding="utf-8")


def test_runtime_classpath_excludes_conflicting_logging_stacks():
    for pattern in (
        "logback-*.jar",
        "logstash-logback-*.jar",
        "tlog-*.jar",
        "log4j-slf4j-impl-*.jar",
    ):
        assert pattern in RUN_SH

    assert 'gpa_classpath="${gpa_classpath:+${gpa_classpath}:}${jar}"' in RUN_SH
    assert '-cp "${CLASS_DIR}:${LIB_DIR}/*:${gpa_classpath}:${SUPERMAP_BIN}/*"' in RUN_SH
    assert '-cp "${CLASS_DIR}:${LIB_DIR}/*:${GPA_LIB_DIR}/*:${SUPERMAP_BIN}/*"' not in RUN_SH


def test_runtime_rejects_an_empty_filtered_gpa_classpath():
    assert 'if [ -z "${gpa_classpath}" ]; then' in RUN_SH
    assert 'echo "No GPA runtime jars found in ${GPA_LIB_DIR}" >&2' in RUN_SH
