#!/usr/bin/env bash
# DEPRECATED: use `wxctl` instead.
# This script only validated the isolated-HOME approach for multi-instance WeChat.
# See README.md for migration: wxctl migrate

set -euo pipefail

echo "wechat-profile.sh is deprecated; use: wxctl create|start <name>" >&2
echo "Migration from ~/.local/share/wechat-profiles: wxctl migrate" >&2

if [[ $# -lt 1 ]]; then
    echo "用法（已废弃）：wechat-profile <实例名称>" >&2
    exit 1
fi

PROFILE="$1"
shift

if [[ ! "$PROFILE" =~ ^[a-zA-Z0-9_-]+$ ]]; then
    echo "实例名称只能包含字母、数字、下划线和短横线" >&2
    exit 1
fi

if command -v wxctl >/dev/null 2>&1; then
    exec wxctl start "$PROFILE" "$@"
fi

REAL_HOME="$(getent passwd "$(id -un)" | cut -d: -f6)"
PROFILE_HOME="$REAL_HOME/.local/share/wechat-profiles/$PROFILE"
SHARED_DIR="$REAL_HOME/Documents/WeChat-Shared"

mkdir -p "$PROFILE_HOME"
mkdir -p "$SHARED_DIR"

if [[ ! -e "$PROFILE_HOME/Shared" ]]; then
    ln -s "$SHARED_DIR" "$PROFILE_HOME/Shared"
fi

export HOME="$PROFILE_HOME"
export GTK_IM_MODULE=fcitx
export QT_IM_MODULE=fcitx
export XMODIFIERS='@im=fcitx'

exec /usr/bin/wechat "$@"
