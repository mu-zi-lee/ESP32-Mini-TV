---
name: mini-tv-agent-status
description: Publish Codex or Claude lifecycle events to the ESP32 mirror traffic lights.
---

# MINI TV Agent Status

Use `scripts/agent-status-hook.sh` as the hook command for Codex or Claude.
The hook accepts JSON on stdin and writes a small status file read by Control.
It is intentionally best-effort: a read-only home directory, missing Python,
or malformed event payload never makes the host agent tool fail.

```sh
MINI_TV_AGENT_SOURCE=codex \
MINI_TV_AGENT_STATE=working \
companion/control/scripts/agent-status-hook.sh
```

Supported states are `idle`, `working`, `done`, `error`, and `question`.
The traffic-light semantics are: working = steady green (the agent is in
progress), done/idle = steady red (the agent has stopped), and question/error
= steady yellow (attention is needed).

The status file keeps separate `codex` and `claude` entries plus a bounded
event history. Control shows each provider for debugging; the ESP32 receives
the aggregate priority `error/question > working > done/idle`.

Set `MINI_TV_AGENT_STATUS_FILE` when the hook and Control run with different
home directories. The default is `~/.config/mini-tv/agent-status.json`.

## Install

From a checkout of this repository:

```sh
mkdir -p ~/.codex/skills/mini-tv-agent-status
cp companion/control/skills/agent-status/SKILL.md ~/.codex/skills/mini-tv-agent-status/
chmod +x companion/control/scripts/agent-status-hook.sh
```

Add the hook command to the host agent's lifecycle configuration. For Codex,
the command can be used for `SessionStart`, `UserPromptSubmit`, `PreToolUse`,
`PostToolUse`, `PermissionRequest`, `Stop`, `Interrupt`, and `SessionEnd` in
`~/.codex/hooks.json`:

```json
{
  "type": "command",
  "command": "MINI_TV_AGENT_SOURCE=codex /absolute/path/to/companion/control/scripts/agent-status-hook.sh",
  "timeout": 3,
  "async": true
}
```

Set `MINI_TV_AGENT_STATE=working` for pre-tool/start events,
`MINI_TV_AGENT_STATE=question` for permission prompts,
`MINI_TV_AGENT_STATE=done` for stop events, and leave it unset for
`PostToolUse` so the JSON result can distinguish success from a structured
failure. Run `/hooks` in Codex to review and trust the command, then restart
the session. Claude can use the same command in its hook settings with
`MINI_TV_AGENT_SOURCE=claude`.

When Control is running, the lower-left mirror overlay shows three dots:
green means an agent is working, red means it is stopped or finished, and
yellow means a question or error needs attention. If a tool call itself shows
an error, inspect the Control Agent 状态 panel and the hook event history;
ordinary text containing the word "error" is not treated as a failure.
