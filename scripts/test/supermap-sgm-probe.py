"""Private T2 probe, executed inside the unmodified Model3D product container."""
import hashlib
import io
import json
from pathlib import Path
import struct
import sys
import time
import urllib.error
import urllib.request


class ProbeAssertion(RuntimeError):
    pass


def require(condition, label):
    if not condition:
        raise ProbeAssertion(label)


def main():
    # These dependencies belong to the product image, never to the host.
    from minio import Minio
    from minio.error import S3Error
    from PIL import Image
    from glb_validation import validate_glb

    config = json.loads(Path("/tmp/sgm-probe.json").read_text())
    Path("/tmp/sgm-probe.json").unlink()
    root = Minio("minio:9000", access_key=config["root_user"], secret_key=config["root_secret"], secure=False)
    client = Minio("minio:9000", access_key=config["user"], secret_key=config["secret"], secure=False)
    bucket = "sgm-t2"
    model_bytes, texture_bytes = Path("/tmp/compass.SGM").read_bytes(), Path("/tmp/compass.png").read_bytes()
    original = Image.open(io.BytesIO(texture_bytes)).convert("RGBA")

    def put(key, data):
        root.put_object(bucket, key, io.BytesIO(data), len(data))

    def get(key, identity=root):
        response = identity.get_object(bucket, key)
        try:
            return response.read()
        finally:
            response.close()
            response.release_conn()

    def denied(action):
        try:
            action()
        except S3Error as error:
            require(error.code == "AccessDenied", "expected real AccessDenied")
        else:
            raise RuntimeError("restricted identity unexpectedly authorized")

    sources = {}
    for case in ("adjacent", "fallback", "denied", "corrupt", "missing"):
        sources[f"source/{case}/compass.SGM"] = model_bytes
    for case in ("fallback", "denied", "corrupt"):
        sources[f"source/{case}/Textures/compass.png"] = texture_bytes
    sources.update({"source/adjacent/compass.png": texture_bytes,
                    "source/adjacent/Textures/compass.png": b"invalid fallback must not be read",
                    "source/denied/compass.png": texture_bytes,
                    "source/corrupt/compass.png": b"corrupt image"})
    for key, data in sources.items():
        put(key, data)
    # Independent IAM assertions: source write/delete and unrelated objects denied.
    put("forbidden/private.txt", b"never readable")
    denied(lambda: get("forbidden/private.txt", client))
    denied(lambda: client.put_object(bucket, "source/forbidden.txt", io.BytesIO(b"x"), 1))
    denied(lambda: client.remove_object(bucket, "source/adjacent/compass.SGM"))

    urls = {"supermap": "http://supermap:8103", "model3d": "http://127.0.0.1:8101"}
    for url in urls.values():
        for _ in range(120):
            try:
                with urllib.request.urlopen(url + "/health", timeout=2) as response:
                    require(response.status == 200, "runtime readiness")
                break
            except (OSError, RuntimeError):
                time.sleep(0.5)
        else:
            raise RuntimeError("runtime readiness timed out")

    def access(key):
        return {"method": "object_store", "endpoint": "http://minio:9000", "use_ssl": False,
                "region": "us-east-1", "access_key": config["user"], "secret_key": config["secret"],
                "bucket": bucket, "object": key}

    def invoke(runtime, operator, source_key, output_key, mode="replace", expect=True):
        source_format, target_format = ("sgm", "osgb") if runtime == "supermap" else ("osgb", "glb")
        plan = {"schema_version": "addp.workflow.access-plan/v1",
                "source": {"kind": "file", "format": source_format, "access": access(source_key)},
                "target": {"kind": "file", "format": target_format, "name": Path(output_key).name,
                           "write_mode": mode, "access": access(output_key)}}
        request = urllib.request.Request(urls[runtime] + f"/api/operators/{operator}/invoke",
                                        json.dumps({"params": {"access_plan": plan}, "timeout_seconds": 60}).encode(),
                                        {"Content-Type": "application/json"})
        try:
            with urllib.request.urlopen(request, timeout=90) as response:
                status, body = response.status, json.load(response)
        except urllib.error.HTTPError as error:
            status, body = error.code, json.load(error)
        require((status == 200 and body.get("status") == "success") == expect,
                f"{runtime}/{operator} unexpected result for {Path(source_key).parent.name}")
        if expect:
            return body["result"]
        require(status >= 400 and body.get("status") in {"failed", "error"}, "expected explicit conversion failure")

    def verify_glb(key):
        data = get(key)
        path = Path("/tmp/sgm-probe.glb")
        path.write_bytes(data)
        doc = validate_glb(path)
        primitives = [p for mesh in doc["meshes"] for p in mesh["primitives"]]
        require(primitives and all(p.get("mode", 4) == 4 for p in primitives), "triangle primitives missing")
        vertices = sum(doc["accessors"][p["attributes"]["POSITION"]]["count"] for p in primitives)
        triangles = sum(doc["accessors"][p["indices"]]["count"] for p in primitives) // 3
        require((vertices, triangles) == (136, 94), "compass geometry changed")
        require(all("TEXCOORD_0" in p["attributes"] for p in primitives), "texture UV lost")
        require(len(doc.get("images", [])) == 1 and all("uri" not in i for i in doc["images"]), "image must be embedded")
        # Read the standard GLB BIN chunk independently of the production validator.
        json_length = struct.unpack_from("<I", data, 12)[0]
        bin_offset = 20 + json_length
        bin_length, bin_kind = struct.unpack_from("<II", data, bin_offset)
        require(bin_kind == 0x004E4942 and bin_offset + 8 + bin_length == len(data), "GLB BIN layout")
        view = doc["bufferViews"][doc["images"][0]["bufferView"]]
        start = bin_offset + 8 + view.get("byteOffset", 0)
        image = Image.open(io.BytesIO(data[start:start + view["byteLength"]])).convert("RGBA")
        require(image.size == original.size and image.tobytes() == original.tobytes(), "embedded texture pixels changed")
        for primitive in primitives:
            pbr = doc["materials"][primitive["material"]]["pbrMetallicRoughness"]
            image_index = doc["textures"][pbr["baseColorTexture"]["index"]]["source"]
            require(image_index == 0, "material does not use the embedded texture")
        path.unlink()
        return {"vertices": vertices, "triangles": triangles, "uv": True, "embedded_pixel_match": True,
                "texture_rgba_sha256": hashlib.sha256(original.tobytes()).hexdigest(),
                "glb_sha256": hashlib.sha256(data).hexdigest()}

    checks = {"restricted_storage_permissions": True, "layouts": {}}
    for case in ("adjacent", "fallback"):
        result = invoke("supermap", "sgm_to_osgb", f"source/{case}/compass.SGM", "stage/compass.osgb", "create" if case == "adjacent" else "replace")
        require((result["vertex_count"], result["triangle_count"]) == (136, 94), "native geometry counts changed")
        invoke("model3d", "osgb_to_glb", "stage/compass.osgb", "result/compass.glb")
        checks["layouts"][case] = verify_glb("result/compass.glb")

    stable_osgb, stable_glb = get("stage/compass.osgb"), get("result/compass.glb")
    for case in ("denied", "corrupt", "missing"):
        invoke("supermap", "sgm_to_osgb", f"source/{case}/compass.SGM", "stage/compass.osgb", expect=False)
        require(get("stage/compass.osgb") == stable_osgb and get("result/compass.glb") == stable_glb,
                "failed conversion modified existing output")
    for runtime, operator, source, target in (("supermap", "sgm_to_osgb", "source/adjacent/compass.SGM", "stage/compass.osgb"),
                                               ("model3d", "osgb_to_glb", "stage/compass.osgb", "result/compass.glb")):
        invoke(runtime, operator, source, target, "create", expect=False)
    require(get("stage/compass.osgb") == stable_osgb and get("result/compass.glb") == stable_glb, "create overwrote artifact")
    put("stage/invalid.osgb", b"invalid OSGB")
    invoke("model3d", "osgb_to_glb", "stage/invalid.osgb", "result/compass.glb", expect=False)
    require(get("result/compass.glb") == stable_glb, "invalid OSGB replaced GLB")
    require(all(get(key) == value for key, value in sources.items()), "source bytes changed")
    checks.update({"source_bytes_unchanged": True, "denied_missing_corrupt_preserve": True,
                   "create_conflict_preserves": True, "invalid_osgb_preserves": True})
    print(json.dumps(checks))


if __name__ == "__main__":
    try:
        main()
    except ProbeAssertion as error:
        print("SGM_PROBE_CHECK:" + str(error), file=sys.stderr)
        sys.exit(1)
    except Exception as error:
        # Never print exception values: S3/HTTP errors can contain signed headers.
        print("SGM private probe assertion failed: " + type(error).__name__, file=sys.stderr)
        sys.exit(1)
