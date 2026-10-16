"""Private storage observer, executed only inside the Model3D product image."""
import hashlib
import io
import json
from pathlib import Path
import struct
import sys


def require(value, message):
    if not value:
        raise ValueError(message)


def glb_summary(raw, doc, image_open):
    primitives = [p for mesh in doc["meshes"] for p in mesh["primitives"]]
    require(primitives and all(p.get("mode", 4) == 4 for p in primitives), "triangle primitives required")
    vertices = sum(doc["accessors"][p["attributes"]["POSITION"]]["count"] for p in primitives)
    triangles = sum(doc["accessors"][p["indices"]]["count"] for p in primitives) // 3
    require((vertices, triangles) == (136, 94), "compass geometry changed")
    require(len(doc.get("images", [])) == 1 and doc["images"][0].get("mimeType") == "image/png", "embedded PNG required")
    textured = 0
    for primitive in primitives:
        if "material" not in primitive:
            continue
        material = doc["materials"][primitive["material"]]
        texture = material.get("pbrMetallicRoughness", {}).get("baseColorTexture")
        if texture is None:
            continue
        require("TEXCOORD_0" in primitive["attributes"] and material.get("alphaMode") == "BLEND", "UV/alpha binding lost")
        require(doc["textures"][texture["index"]]["source"] == 0, "wrong embedded image")
        textured += 1
    require(textured > 0, "image is not used")
    binary_offset = 28 + struct.unpack_from("<I", raw, 12)[0]
    view = doc["bufferViews"][doc["images"][0]["bufferView"]]
    start = binary_offset + view.get("byteOffset", 0)
    image = image_open(io.BytesIO(raw[start:start + view["byteLength"]])).convert("RGBA")
    require(image.size == (256, 256) and image.getextrema()[3] == (0, 255), "texture dimensions/alpha changed")
    require(all(lo < hi for lo, hi in image.getextrema()[:3]), "empty RGB texture")
    return {"vertices": vertices, "triangles": triangles, "textured_primitives": textured,
            "image_mime_type": "image/png", "alpha_range": [0, 255], "alpha_mode": "BLEND",
            "glb_sha256": hashlib.sha256(raw).hexdigest(), "texture_rgba_sha256": hashlib.sha256(image.tobytes()).hexdigest()}


def run(action):
    from minio import Minio
    from minio.commonconfig import ENABLED
    from PIL import Image
    from glb_validation import validate_glb

    cfg = json.loads(Path("/tmp/manager-sgm.json").read_text())
    Path("/tmp/manager-sgm.json").unlink()
    business = Minio("business-minio:9000", access_key=cfg["root_user"], secret_key=cfg["root_secret"], secure=False)
    infra = Minio("minio:9000", access_key=cfg["root_user"], secret_key=cfg["root_secret"], secure=False)
    fixture = {name: Path("/tmp/" + name).read_bytes() for name in ("compass.SGM", "compass.png")}

    def get(client, bucket, key):
        response = client.get_object(bucket, key)
        try:
            return response.read()
        finally:
            response.close()
            response.release_conn()

    if action == "seed":
        # The maintained Infra initializer owns the private manager bucket and temp/ policy.
        lifecycle = infra.get_bucket_lifecycle("manager")
        require(any(r.status == ENABLED and r.rule_filter.prefix == "temp/" and r.expiration.days == 7 for r in lifecycle.rules), "Infra temp expiration absent")
        require(infra.get_bucket_versioning("manager").status not in {"Enabled", "Suspended"}, "versioning must be disabled")
        business.make_bucket("sgm-local")
        for name, data in fixture.items():
            business.put_object("sgm-local", "source/" + name, io.BytesIO(data), len(data))
        # Same immutable, pinned mc binary as the product MinIO image; credentials never logged.
        policy = {"Version": "2012-10-17", "Statement": [
            {"Effect": "Allow", "Action": ["s3:ListAllMyBuckets"], "Resource": ["*"]},
            {"Effect": "Allow", "Action": ["s3:ListBucket", "s3:GetBucketLocation"], "Resource": ["arn:aws:s3:::sgm-local"]},
            {"Effect": "Allow", "Action": ["s3:GetObject"], "Resource": ["arn:aws:s3:::sgm-local/source/*"]}]}
        # Root management stays on the host's MinIO container, not in browser or consumer identity.
        return {"size_bytes": len(fixture["compass.SGM"]), "sha256": hashlib.sha256(fixture["compass.SGM"]).hexdigest(),
                "texture_sha256": hashlib.sha256(fixture["compass.png"]).hexdigest(), "policy": policy}
    require(all(get(business, "sgm-local", "source/" + name) == raw for name, raw in fixture.items()), "source bytes changed")
    require(not list(infra.list_objects("manager", prefix="temp/model3d-sgm/", recursive=True)), "intermediate OSGB residue")
    objects = list(infra.list_objects("manager", recursive=True))
    glbs = [item for item in objects if item.object_name.endswith(".glb")]
    if action == "verify":
        require(len(glbs) == 1, "expected exactly one managed GLB")
        raw = get(infra, "manager", glbs[0].object_name)
        require(raw == Path("/tmp/manager-sgm.glb").read_bytes(), "API and physical GLB differ")
        doc = validate_glb(Path("/tmp/manager-sgm.glb"))
        return {**glb_summary(raw, doc, Image.open), "source_unchanged": True, "intermediate_osgb_absent": True,
                "physical_content_matches_api": True}
    require(action == "deleted", "unknown storage observer action")
    require(not objects, "managed object residue after domain deletion")
    return {"source_unchanged": True, "managed_objects": 0, "intermediate_osgb_objects": 0}


if __name__ == "__main__":
    try:
        print(json.dumps(run(sys.argv[1])))
    except Exception as error:
        print("SGM_PROBE_CHECK:private storage observer " + type(error).__name__, file=sys.stderr)
        sys.exit(1)
