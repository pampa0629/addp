"""Bounded, owned conversation windows and lightweight message projections."""
import base64
import copy
import hmac
import json
from uuid import UUID

from fastapi import HTTPException
from sqlalchemy import String, and_, cast, func, or_, select

from config import settings
from i18n import workspace_message
from models.interaction import Interaction
from models.message import Message
from services.interactions import format_resume_display
from services.messages import bounded_message_parts


def _invalid(request):
    return HTTPException(400, workspace_message(request.headers.get("accept-language"), "history_cursor_invalid"))


def _key():
    if not settings.AGENT_SERVICE_CLIENT_SECRET:
        raise RuntimeError("Agent history cursor signing unavailable")
    return hmac.digest(settings.AGENT_SERVICE_CLIENT_SECRET.encode(), b"agent-history/v1", "sha256")


def _cursor(request, session_id, kind, snapshot, position, direction):
    value = [1, int(request.state.principal_id), int(request.state.tenant_id),
             session_id, kind, snapshot, position, direction]
    raw = json.dumps(value, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(raw + hmac.digest(_key(), raw, "sha256")).decode().rstrip("=")


def _read_cursor(request, session_id, kind, token):
    try:
        if not token or len(token) > 1024:
            raise ValueError
        decoded = base64.b64decode(token + "=" * (-len(token) % 4), altchars=b"-_", validate=True)
        raw, signature = decoded[:-32], decoded[-32:]
        if not hmac.compare_digest(signature, hmac.digest(_key(), raw, "sha256")):
            raise ValueError
        value = json.loads(raw)
        if not isinstance(value, list) or len(value) != 8 or value[:5] != [
            1, int(request.state.principal_id), int(request.state.tenant_id), session_id, kind
        ] or any(type(value[i]) is not int or value[i] <= 0 for i in (5, 6)) or value[7] not in ("older", "newer"):
            raise ValueError
        return value[5:]
    except (ValueError, TypeError, json.JSONDecodeError):
        raise _invalid(request) from None


def _before(message):
    return or_(Message.created_at < message.created_at,
               and_(Message.created_at == message.created_at, Message.id < message.id))


def _after(message):
    return or_(Message.created_at > message.created_at,
               and_(Message.created_at == message.created_at, Message.id > message.id))


async def message_window(db, request, session_id, *, limit, cursor=None, anchor=None, kind="messages"):
    if cursor is not None and anchor is not None:
        raise _invalid(request)
    position, direction = None, "older"
    if cursor is not None:
        snapshot, position_id, direction = _read_cursor(request, session_id, kind, cursor)
        position = (await db.execute(select(Message).where(
            Message.session_id == session_id, Message.id == position_id, Message.id <= snapshot
        ))).scalar_one_or_none()
        if position is None:
            raise _invalid(request)
    else:
        snapshot = (await db.execute(select(func.max(Message.id)).where(Message.session_id == session_id))).scalar_one() or 0
    base = select(Message).where(Message.session_id == session_id, Message.id <= snapshot)
    descending = (Message.created_at.desc(), Message.id.desc())
    ascending = (Message.created_at.asc(), Message.id.asc())
    if anchor is not None:
        position = (await db.execute(select(Message).where(
            Message.session_id == session_id, Message.id == anchor, Message.id <= snapshot
        ))).scalar_one_or_none()
        if position is None:
            raise HTTPException(404, workspace_message(request.headers.get("accept-language"), "history_message_missing"))
        older = (await db.execute(base.where(_before(position)).order_by(*descending).limit(limit // 2))).scalars().all()
        newer = (await db.execute(base.where(or_(_after(position), Message.id == position.id))
                                 .order_by(*ascending).limit(limit - len(older)))).scalars().all()
        rows = list(reversed(older)) + list(newer)
    else:
        statement = base
        if position is not None:
            statement = statement.where(_before(position) if direction == "older" else _after(position))
        rows = list((await db.execute(statement.order_by(*(descending if direction == "older" else ascending)).limit(limit))).scalars().all())
        if direction == "older":
            rows.reverse()
    page = {"limit": limit, "older_cursor": None, "newer_cursor": None}
    if rows:
        has_older = (await db.execute(select(base.where(_before(rows[0])).exists()))).scalar_one()
        has_newer = (await db.execute(select(base.where(_after(rows[-1])).exists()))).scalar_one()
        if has_older:
            page["older_cursor"] = _cursor(request, session_id, kind, snapshot, rows[0].id, "older")
        if has_newer:
            page["newer_cursor"] = _cursor(request, session_id, kind, snapshot, rows[-1].id, "newer")
    return rows, page


async def project_messages(db, request, messages):
    identifiers, replies = set(), set()
    for message in messages:
        for part in message.parts or []:
            if part.get("type") == "interaction_ref":
                identifiers.add(part.get("interaction_id"))
        if message.role == "user" and (message.protocol_message_id or "").startswith("resume:"):
            replies.add(message.protocol_message_id[7:])
    valid_ids = set()
    for value in identifiers | replies:
        try:
            valid_ids.add(UUID(str(value)))
        except (ValueError, TypeError, AttributeError):
            continue
    interactions = {}
    if valid_ids and messages:
        values = (await db.execute(select(Interaction).where(
            Interaction.id.in_(valid_ids), Interaction.session_id == messages[0].session_id,
            Interaction.user_id == int(request.state.principal_id),
            Interaction.tenant_id == int(request.state.tenant_id)
        ))).scalars().all()
        interactions = {str(item.id): item for item in values}
    result = []
    for message in messages:
        parts = copy.deepcopy(message.parts or [])
        content = message.content
        reply = interactions.get((message.protocol_message_id or "")[7:]) if message.role == "user" and (message.protocol_message_id or "").startswith("resume:") else None
        if reply is not None:
            content = format_resume_display(reply)
            parts = bounded_message_parts([{"type": "text", "text": content}])
        for part in parts:
            if part.get("type") == "interaction_ref":
                linked = interactions.get(part.get("interaction_id"))
                if linked is not None:
                    part["status"] = linked.status
        result.append(dict(id=message.id, protocol_message_id=message.protocol_message_id,
                           is_complete=message.is_complete, role=message.role, content=content,
                           parts=parts, created_at=message.created_at.isoformat()))
    return result


RESULT_COMPONENTS = {"WorkflowDag", "TransferDraftSnapshot", "TransferConfigCheckReport", "MapView", "TablePreview"}


def message_entries(message, kind):
    entries = {}
    for part in message["parts"]:
        if kind == "pending" and part.get("type") == "interaction_ref" and part.get("status") == "pending":
            key = part["interaction_id"]
            entries[key] = dict(key=key, kind=part.get("kind"), interaction_id=key)
        if kind == "results" and part.get("type") == "result_ref":
            key = ":".join(str(part.get(field, "")) for field in ("owner_module", "kind", "ref"))
            entries[key] = dict(key=key, kind="execution", label=str(part.get("ref", ""))[:300])
        if part.get("type") != "presentation_ref" or part.get("protocol") != "a2ui" or part.get("interaction_id"):
            continue
        content = part.get("content") or {}
        for operation in content.get("operations", []):
            for component in operation.get("updateComponents", {}).get("components", []):
                component_kind = component.get("component")
                if kind == "results" and component_kind in RESULT_COMPONENTS and isinstance(part.get("surface_id"), str):
                    key = part["surface_id"]
                    entries[key] = dict(key=key, kind=component_kind, label=str(component.get("name", ""))[:300])
                if kind == "resources" and component_kind == "TransferDraftSnapshot":
                    try:
                        parameters = json.loads(component.get("parameters", ""))
                        config = parameters["config"]
                        locators = (config.get("source", {}).get("locator"), config.get("target", {}).get("parent_locator"))
                    except (ValueError, TypeError, KeyError):
                        continue
                    for locator in locators:
                        if isinstance(locator, str) and locator.startswith("addp://") and len(locator) <= 2048:
                            entries[locator] = dict(key=locator, kind="resource", locator=locator)
    return list(entries.values())


async def workspace_context(db, request, session_id):
    first = (await db.execute(select(Message).where(Message.session_id == session_id, Message.role == "user")
                             .order_by(Message.created_at.asc(), Message.id.asc()).limit(1))).scalar_one_or_none()
    goal = ""
    if first:
        text = [part["text"] for part in first.parts or [] if part.get("type") == "text" and isinstance(part.get("text"), str)]
        goal = "\n\n".join(text) if text else first.content
    counts = (await db.execute(select(Interaction.kind, func.count(), func.min(cast(Interaction.id, String))).where(
        Interaction.session_id == session_id, Interaction.user_id == int(request.state.principal_id),
        Interaction.tenant_id == int(request.state.tenant_id), Interaction.status == "pending"
    ).group_by(Interaction.kind))).all()
    counts = {kind: (count, identity) for kind, count, identity in counts}
    clarifications, single = counts.get("clarification", (0, None))
    return dict(goal=goal, pending_clarifications=clarifications,
                pending_approvals=counts.get("owner_approval", (0, None))[0],
                clarification_id=str(UUID(single)) if clarifications == 1 else None)
