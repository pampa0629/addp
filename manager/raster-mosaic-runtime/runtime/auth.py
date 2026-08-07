import json
from urllib import error as urlerror
from urllib import request as urlrequest


def require_manager_service(request, system_url: str, tenant_id: int) -> str:
    authorization = str(request.headers.get("Authorization") or "").strip()
    if not authorization.startswith("Bearer "):
        return "missing bearer service access token"
    req = urlrequest.Request(
        system_url.rstrip("/") + "/api/v1/system/auth/context",
        headers={"Authorization": authorization},
        method="GET",
    )
    try:
        with urlrequest.urlopen(req, timeout=5) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except (OSError, ValueError, urlerror.URLError, urlerror.HTTPError):
        return "service access token validation failed"
    context = payload.get("context") if isinstance(payload, dict) else None
    token = payload.get("token") if isinstance(payload, dict) else None
    client = payload.get("client") if isinstance(payload, dict) else None
    if not isinstance(context, dict) or not isinstance(token, dict) or not isinstance(client, dict):
        return "invalid authorization context"
    if token.get("type") != "service_access_token" or client.get("client_id") != "addp-manager":
        return "manager service access token is required"
    if context.get("type") != "tenant" or str(context.get("tenant_id") or "") != str(tenant_id):
        return "tenant context does not match request"
    return ""
