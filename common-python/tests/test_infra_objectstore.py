import json
import subprocess
import sys
from pathlib import Path
import pytest
from addp_common.infra.objectstore import Ref
from addp_common.infra.objectstore.bootstrap import resolve_bucket

ROOT = Path(__file__).resolve().parents[2]
CASES = json.loads((ROOT / "common/infra/objectstore/testdata/contract.json").read_text())

@pytest.mark.parametrize("value", CASES["valid_refs"])
def test_ref_round_trip(value):
    assert Ref.parse(value).uri() == value

@pytest.mark.parametrize("value", CASES["invalid_refs"])
def test_ref_reject(value):
    with pytest.raises(ValueError):
        Ref.parse(value)

@pytest.mark.parametrize("case", CASES["bindings"])
def test_binding_contract(case):
    if case.get("error"):
        with pytest.raises(ValueError):
            resolve_bucket(case["area"], case["raw"])
    else:
        assert resolve_bucket(case["area"], case["raw"]) == case["bucket"]

def test_contract_import_has_no_sdk_dependency():
    subprocess.run([sys.executable,"-c","import sys; import addp_common.infra.objectstore; assert 'minio' not in sys.modules"],check=True)

@pytest.mark.parametrize("case", CASES["bindings"])
def test_deployment_initializer_uses_the_same_mapping(case, tmp_path):
    import os
    docker = tmp_path / "docker"
    record = tmp_path / "created.jsonl"
    docker.write_text(f"#!{sys.executable}\n" + '''import json, os, sys
args=sys.argv[1:]
if 'stat' in args or 'ls' in args:
    sys.exit(1)
if 'mb' in args or 'anonymous' in args:
    with open(os.environ['BINDING_PROBE_RECORD'],'a') as out:
        out.write(json.dumps({'operation':'create' if 'mb' in args else 'private','target':args[-1]})+'\\n')
if 'ps' in args:
    print('owned-test-container')
''')
    docker.chmod(0o700)
    env=dict(os.environ, PATH=str(tmp_path)+os.pathsep+os.environ['PATH'],
             ADDP_INFRA_RESOLVED='1', ADDP_INFRA_OBJECTSTORE_PROVIDER='minio',
             ADDP_INFRA_OBJECTSTORE_BINDINGS=case['raw'], MINIO_ROOT_USER='fixture',
             MINIO_ROOT_PASSWORD='fixture-only', BINDING_PROBE_RECORD=str(record))
    result=subprocess.run(['bash',str(ROOT/'scripts/infra/init-minio.sh')],env=env,capture_output=True,text=True,timeout=10)
    if case.get('error'):
        assert result.returncode != 0
        assert not record.exists()
    else:
        assert result.returncode == 0, result.stderr
        events=[json.loads(row) for row in record.read_text().splitlines()]
        expected='local/'+case['bucket']
        assert {'operation':'create','target':expected} in events
        assert {'operation':'private','target':expected} in events
