#!/bin/sh
set -eu

# Shared adapter for Codex and Claude lifecycle hooks. The Python helper keeps
# one entry per provider/session so concurrent agents never clear each other.
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
if command -v python3 >/dev/null 2>&1; then
  exec python3 "$script_dir/agent-status-hook.py"
fi
if command -v python >/dev/null 2>&1; then
  exec python "$script_dir/agent-status-hook.py"
fi
# A missing optional runtime must not make the host agent tool fail.
exit 0
