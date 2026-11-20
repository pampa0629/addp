"""Real S3 half of the Develop gate; never a unit-test storage substitute."""
import io
import os
import sys
import tempfile
from pathlib import Path
from types import SimpleNamespace
from minio import Minio
from addp_common.infra.objectstore.bootstrap import open_object_area

phase = sys.argv[1]
key = "tenant_7/notebooks/跨语言 % key.ipynb"
client = Minio(os.environ["MINIO_ENDPOINT"],access_key=os.environ["MINIO_ROOT_USER"],secret_key=os.environ["MINIO_ROOT_PASSWORD"],secure=False)
if phase == "prepare":
    for bucket, data in (("develop",b'{"cells":[],"metadata":{"origin":"default"},"nbformat":4,"nbformat_minor":5}'),("notebook-exception",b'{"cells":[],"metadata":{"origin":"exception"},"nbformat":4,"nbformat_minor":5}')):
        if not client.bucket_exists(bucket):
            client.make_bucket(bucket)
        client.put_object(bucket,key,io.BytesIO(data),len(data))
    from urllib.request import urlopen
    from urllib.error import HTTPError
    from urllib.parse import quote
    try:
        urlopen("http://"+os.environ["MINIO_ENDPOINT"]+"/notebook-exception/"+quote(key,safe="/"),timeout=10)
    except HTTPError as exc:
        assert exc.code == 403
    else:
        raise AssertionError("mapped Notebook bucket permits anonymous read")
elif phase == "exchange":
    area = open_object_area("develop")
    with area.open(key) as stream:
        assert stream.read() == '{"cells":[],"metadata":{"origin":"Go 中"},"nbformat":4,"nbformat_minor":5}\n'.encode()
    data = '{"cells":[], "metadata":{"origin":"Python 文"},"nbformat":4,"nbformat_minor":5}\n'.encode()
    area.put(key,io.BytesIO(data),len(data),"application/x-ipynb+json")
elif phase in {"save", "save-failure"}:
    sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "engines/jupyter"))
    from interactive_sessions import InteractiveSessionManager
    from execution_container import ExecutionContainer
    from unittest.mock import patch
    if phase == "save-failure":
        os.environ["MINIO_ROOT_USER"] = os.environ["NOTEBOOK_READONLY_USER"]
        os.environ["MINIO_ROOT_PASSWORD"] = os.environ["NOTEBOOK_READONLY_PASSWORD"]
    area = open_object_area("develop")
    with area.open(key) as stream:
        before = stream.read()
    with tempfile.TemporaryDirectory() as root:
        manager = InteractiveSessionManager(area, root)
        container = ExecutionContainer(root, "one-session")
        container.prepare("edited.ipynb", b'{"cells":[],"metadata":{"edited":true},"nbformat":4,"nbformat_minor":5}', {})
        session = SimpleNamespace(session_id="notebook-storage-fixture", tenant_id=7, task_id=1,
                                  notebook_name="edited.ipynb", container=container)
        from addp_common.infra.objectstore import Ref
        session.notebook_locator = Ref("develop", key).uri()
        try:
            with patch.object(container.http, "delete"):
                manager._save_and_cleanup(session)
        except PermissionError:
            if phase != "save-failure":
                raise
        else:
            if phase == "save-failure":
                raise AssertionError("read-only save unexpectedly succeeded")
            before = b'{"cells":[],"metadata":{"edited":true},"nbformat":4,"nbformat_minor":5}'
        assert not container.workspace.exists()
        manager.shutdown()
    with area.open(key) as stream:
        assert stream.read() == before
else:
    raise ValueError("unknown probe phase")
