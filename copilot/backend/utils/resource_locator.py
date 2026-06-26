from urllib.parse import quote


def table_locator(engine_id: int, schema: str, table: str) -> str:
    return f"addp://engine/{engine_id}/path/{quote(schema)}/{quote(table)}?type=table"


def schema_locator(engine_id: int, schema: str) -> str:
    return f"addp://engine/{engine_id}/path/{quote(schema)}?type=schema"


def object_locator(engine_id: int, bucket: str, object_path: str) -> str:
    encoded_path = "/".join(quote(part) for part in [bucket, *object_path.strip("/").split("/")] if part)
    return f"addp://engine/{engine_id}/path/{encoded_path}?type=object"


def bucket_locator(engine_id: int, bucket: str) -> str:
    return f"addp://engine/{engine_id}/path/{quote(bucket)}?type=bucket"
