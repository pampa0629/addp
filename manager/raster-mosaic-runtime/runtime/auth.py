def require_internal_key(request, expected_key: str) -> str:
    expected = str(expected_key or "").strip()
    if not expected:
        return ""
    actual = str(request.headers.get("X-Internal-API-Key") or "").strip()
    if actual != expected:
        return "invalid internal api key"
    return ""
