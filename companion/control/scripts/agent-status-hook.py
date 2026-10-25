#!/usr/bin/env python3
import json
import os
import re
import sys
import tempfile
import time
from pathlib import Path


def text(value):
    return str(value).strip() if value is not None else ""


def first(payload, *keys):
    for key in keys:
        value = payload.get(key)
        if value not in (None, ""):
            return text(value)
    return ""


def is_false(value):
    return value is False or (isinstance(value, str) and value.strip().lower() in ("false", "no", "0"))


def is_nonzero(value):
    if isinstance(value, bool):
        return False
    try:
        return int(value) != 0
    except (TypeError, ValueError):
        return False


def has_error_value(value):
    if value in (None, "", False, 0, [], {}):
        return False
    if isinstance(value, str) and value.strip().lower() in ("false", "none", "null", "0"):
        return False
    return True


def explicit_failure(value):
    """Recognize failure metadata without scanning arbitrary tool text."""
    if isinstance(value, str):
        stripped = value.strip()
        if not stripped:
            return False
        try:
            return explicit_failure(json.loads(stripped))
        except (TypeError, ValueError, json.JSONDecodeError):
            # A plain-text tool result is intentionally not searched for words
            # such as "error": successful output can mention them normally.
            lowered = stripped.lower()
            return lowered.startswith(("error:", "failed:", "failure:", "command failed"))
    if isinstance(value, list):
        return any(explicit_failure(item) for item in value)
    if not isinstance(value, dict):
        return False

    # Hook payloads differ slightly between Codex and Claude. These fields
    # are explicit error channels, unlike ordinary output or stderr text.
    for key in ("error", "errors", "exception"):
        candidate = value.get(key)
        if has_error_value(candidate):
            return True

    for key in ("ok", "success", "successful"):
        if key in value and is_false(value[key]):
            return True
    for key in ("is_error", "isError", "failed", "failure"):
        if key in value and (value[key] is True or is_false(value[key]) is False and str(value[key]).strip().lower() in ("error", "failed", "failure")):
            return True
    for key in ("exit_code", "exitCode", "return_code", "returnCode"):
        if key in value and is_nonzero(value[key]):
            return True
    for key in ("status", "state", "result"):
        candidate = value.get(key)
        if isinstance(candidate, str) and candidate.strip().lower() in ("error", "failed", "failure", "failure_status"):
            return True
    return False


def state_for(event, payload):
    forced = text(os.environ.get("MINI_TV_AGENT_STATE"))
    if forced:
        return forced.lower()
    event = event.lower()
    if any(token in event for token in ("permission", "approval", "question", "input")):
        return "question"
    if any(token in event for token in ("error", "fail", "exception")):
        return "error"
    if any(token in event for token in ("stop", "complete", "finish", "success")):
        return "done"
    if "posttooluse" in event or "post_tool_use" in event or "aftertooluse" in event:
        tool_result = payload.get("tool_response")
        if tool_result is None:
            tool_result = payload.get("tool_result")
        if tool_result is None:
            tool_result = payload.get("result")
        return "error" if explicit_failure(tool_result) else "working"
    if "pretooluse" in event or "pre_tool_use" in event:
        return "working"
    if any(token in event for token in ("start", "begin", "submit", "prompt", "message", "tool")):
        return "working"
    return "idle"


def main():
    try:
        payload = json.load(sys.stdin)
    except Exception:
        payload = {}
    source = text(os.environ.get("MINI_TV_AGENT_SOURCE")) or text(payload.get("source")) or "agent"
    source = re.sub(r"[^A-Za-z0-9_.-]", "_", source.lower())
    event = first(payload, "hook_event_name", "event", "type") or "unknown"
    state = state_for(event, payload)
    session_id = first(payload, "session_id", "sessionId", "conversation_id", "conversationId")
    message = first(payload, "last_assistant_message", "message", "reason")
    now = int(time.time())
    path = Path(os.environ.get("MINI_TV_AGENT_STATUS_FILE", Path.home() / ".config/mini-tv/agent-status.json"))
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
    except OSError:
        # A status hook must never turn a normal tool call into a failed call.
        return
    document = {"agents": {}, "events": [], "updatedAt": now}
    try:
        with path.open("r", encoding="utf-8") as handle:
            existing = json.load(handle)
        if isinstance(existing, dict):
            document.update(existing)
    except (OSError, ValueError):
        pass
    agents = document.get("agents")
    if not isinstance(agents, dict):
        agents = {}
    if document.get("state") or document.get("source"):
        legacy_source = text(document.get("source")) or "agent"
        agents[legacy_source] = {"state": text(document.get("state")), "source": legacy_source,
                                 "sessionId": text(document.get("sessionId")),
                                 "updatedAt": int(document.get("updatedAt") or now)}
    entry = {"state": state, "source": source, "updatedAt": now}
    if session_id:
        entry["sessionId"] = session_id
    if event:
        entry["event"] = event
    if message:
        entry["message"] = message[:280]
    agents[source] = entry
    events = document.get("events")
    if not isinstance(events, list):
        events = []
    events.append(entry)
    document = {"agents": agents, "events": events[-24:], "updatedAt": now}
    try:
        with tempfile.NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, prefix=path.name + ".", delete=False) as handle:
            json.dump(document, handle, ensure_ascii=True, separators=(",", ":"))
            handle.write("\n")
            temporary = Path(handle.name)
        os.replace(temporary, path)
    except OSError:
        # Hooks are observability only; a read-only home directory should not
        # break the command that emitted the lifecycle event.
        return


if __name__ == "__main__":
    main()
