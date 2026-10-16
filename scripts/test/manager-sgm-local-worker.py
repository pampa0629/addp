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


SAMPLES = {"compass": {"name": "compass.SGM", "geometry": (136, 94), "image_size": 256,
                       "mime_types": ["image/png"]},
           "autumn-tree": {"name": "AutumnTree.SGM", "geometry": (9589, 7273), "image_size": 1024,
                           "mime_types": ["image/jpeg", "image/png"]}}


def glb_summary(raw, doc, image_open, sample_id):
    expected = SAMPLES[sample_id]
    primitives = [p for mesh in doc["meshes"] for p in mesh["primitives"]]
    require(primitives and all(p.get("mode", 4) == 4 for p in primitives), "triangle primitives required")
    vertices = sum(doc["accessors"][p["attributes"]["POSITION"]]["count"] for p in primitives)
    triangles = sum(doc["accessors"][p["indices"]]["count"] for p in primitives) // 3
    require((vertices, triangles) == expected["geometry"], "fixture geometry changed")
    images = doc.get("images", [])
    require(sorted(i.get("mimeType", "") for i in images) == expected["mime_types"], "embedded images changed")
    bindings = {index: [] for index in range(len(images))}
    for primitive in primitives:
        require("material" in primitive, "primitive material lost")
        material = doc["materials"][primitive["material"]]
        texture = material.get("pbrMetallicRoughness", {}).get("baseColorTexture")
        require(texture is not None, "untextured primitive")
        require(texture.get("texCoord", 0) == 0 and "TEXCOORD_0" in primitive["attributes"], "UV binding lost")
        index = doc["textures"][texture["index"]]["source"]
        require(index in bindings, "wrong embedded image")
        alpha_mode = material.get("alphaMode", "OPAQUE")
        require(alpha_mode == ("BLEND" if images[index]["mimeType"] == "image/png" else "OPAQUE"), "alpha binding lost")
        bindings[index].append(alpha_mode)
    require(all(bindings.values()), "image is not used")
    binary_offset = 28 + struct.unpack_from("<I", raw, 12)[0]
    summaries = []
    for index, embedded in enumerate(images):
        view = doc["bufferViews"][embedded["bufferView"]]
        start = binary_offset + view.get("byteOffset", 0)
        image = image_open(io.BytesIO(raw[start:start + view["byteLength"]]))
        require(image.mode == ("RGBA" if embedded["mimeType"] == "image/png" else "RGB"), "image channels changed")
        image = image.convert("RGBA")
        require(image.size == (expected["image_size"],) * 2, "texture dimensions changed")
        require(all(lo < hi for lo, hi in image.getextrema()[:3]), "empty RGB texture")
        histogram = image.getchannel("A").histogram()
        counts = [histogram[0], sum(histogram[1:255]), histogram[255]]
        if embedded["mimeType"] == "image/png":
            require(image.getextrema()[3] == (0, 255), "texture alpha lost")
            if sample_id == "autumn-tree":
                require(counts == [774205, 126685, 147686], "autumn leaf alpha pixels changed")
        else:
            require(counts == [0, 0, expected["image_size"] ** 2], "trunk opacity changed")
        summaries.append({"mime_type": embedded["mimeType"], "size": list(image.size),
            "alpha_pixel_counts": counts, "alpha_modes": sorted(set(bindings[index])),
            "textured_primitives": len(bindings[index]),
            "rgba_sha256": hashlib.sha256(image.tobytes()).hexdigest()})
    return {"vertices": vertices, "triangles": triangles, "images": summaries,
            "textured_primitives": sum(len(values) for values in bindings.values()),
            "glb_sha256": hashlib.sha256(raw).hexdigest()}


def run(action):
    from minio import Minio
    from minio.commonconfig import ENABLED
    from PIL import Image
    from glb_validation import validate_glb

    cfg = json.loads(Path("/tmp/manager-sgm.json").read_text())
    Path("/tmp/manager-sgm.json").unlink()
    business = Minio("business-minio:9000", access_key=cfg["root_user"], secret_key=cfg["root_secret"], secure=False)
    infra = Minio("minio:9000", access_key=cfg["root_user"], secret_key=cfg["root_secret"], secure=False)
    root = Path("/tmp/sgm-fixtures")
    fixture = {path.relative_to(root).as_posix(): path.read_bytes() for path in root.rglob("*") if path.is_file()}
    require(set(fixture) == {"compass/compass.SGM", "compass/compass.png", "autumn-tree/AutumnTree.SGM",
                             "autumn-tree/Textures/hongye.PNG", "autumn-tree/Textures/shugan.jpg"}, "fixture file set changed")

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
        return {"samples": [{"id": key, "name": spec["name"],
                "size_bytes": len(fixture[key + "/" + spec["name"]]),
                "sha256": hashlib.sha256(fixture[key + "/" + spec["name"]]).hexdigest()} for key, spec in SAMPLES.items()],
                "source_file_sha256": {key: hashlib.sha256(raw).hexdigest() for key, raw in fixture.items()}, "policy": policy}
    require(all(get(business, "sgm-local", "source/" + name) == raw for name, raw in fixture.items()), "source bytes changed")
    require(not list(infra.list_objects("manager", prefix="temp/model3d-sgm/", recursive=True)), "intermediate OSGB residue")
    objects = list(infra.list_objects("manager", recursive=True))
    glbs = [item for item in objects if item.object_name.endswith(".glb")]
    if action == "verify":
        require(len(glbs) == 1, "expected exactly one managed GLB")
        raw = get(infra, "manager", glbs[0].object_name)
        require(raw == Path("/tmp/manager-sgm.glb").read_bytes(), "API and physical GLB differ")
        doc = validate_glb(Path("/tmp/manager-sgm.glb"))
        return {**glb_summary(raw, doc, Image.open, cfg["sample_id"]), "source_unchanged": True, "intermediate_osgb_absent": True,
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
