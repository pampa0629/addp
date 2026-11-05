#!/usr/bin/env bash
set -euo pipefail

fail() {
    printf '%s\n' "$*" >&2
    exit 1
}

require_primary_main() {
    local primary current
    primary=$(git worktree list --porcelain | sed -n '1s/^worktree //p')
    current=$(git rev-parse --show-toplevel)
    [[ "$current" == "$primary" && "$(git symbolic-ref -q HEAD)" == refs/heads/main ]] ||
        fail '请从共享工作区的 main 执行发布。'
}

require_release_lock() {
    local owner
    require_primary_main
    owner=$(cat "$lock/owner" 2>/dev/null || true)
    [[ -n "$owner" && "$owner" == "${ADDP_MAIN_PUBLISH_OWNER:-}" ]] ||
        fail 'main 由统一发布入口保护。请先与当前发布会话协调，再执行 scripts/ci/publish-main.sh。'
}

common=$(git rev-parse --path-format=absolute --git-common-dir)
lock="$common/addp-main-publish.lock"
case "${0##*/}" in
    reference-transaction)
        [[ "${1:-}" == prepared ]] || exit 0
        while read -r old new ref; do
            if [[ "$ref" == refs/heads/main ]]; then
                require_release_lock
            fi
        done
        exit 0
        ;;
    pre-push)
        while read -r local_ref local_oid remote_ref remote_oid; do
            if [[ "$remote_ref" == refs/heads/main ]]; then
                require_release_lock
            fi
        done
        exit 0
        ;;
esac

require_primary_main
root=$(git rev-parse --show-toplevel)
cd "$root"
hook_source="$root/scripts/ci/publish-main.sh"
hooks="$common/hooks"
mode=${1:-}
case "$mode" in
    --install|--sync|--push-existing)
        [[ $# == 1 ]] || fail "用法：$0 $mode"
        ;;
    --)
        shift
        [[ $# -gt 0 ]] || fail "用法：$0 -- 命令 [参数...]"
        ;;
    *)
        fail "用法：$0 --install | --sync | --push-existing | -- 命令 [参数...]"
        ;;
esac

if [[ "$mode" == --install ]]; then
    if git config --get core.hooksPath >/dev/null; then
        fail '仓库已有自定义 Git hooks 路径，请先核对后统一安装。'
    fi
    mkdir -p "$hooks"
    for name in pre-push reference-transaction; do
        target="$hooks/$name"
        if [[ -e "$target" || -L "$target" ]]; then
            [[ -L "$target" && "$(readlink "$target")" == "$hook_source" ]] ||
                fail "已有 Git hook，请先核对：$target"
        fi
    done
    for name in pre-push reference-transaction; do
        [[ -L "$hooks/$name" ]] || ln -s "$hook_source" "$hooks/$name"
    done
    git config --local pull.ff only
    exit 0
fi

if ! mkdir "$lock" 2>/dev/null; then
    fail "main 正在发布，请等待当前持有者结束：$lock"
fi
trap 'rm -f "$lock/owner"; rmdir "$lock"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
export ADDP_MAIN_PUBLISH_OWNER=$$
printf '%s\n' "$ADDP_MAIN_PUBLISH_OWNER" > "$lock/owner"

fetch_main() {
    git fetch --no-tags origin refs/heads/main
    git rev-parse FETCH_HEAD
}

remote_before=$(fetch_main)
if git merge-base --is-ancestor HEAD "$remote_before"; then
    if [[ "$(git rev-parse HEAD)" != "$remote_before" ]]; then
        git merge --ff-only "$remote_before"
    fi
elif ! git merge-base --is-ancestor "$remote_before" HEAD; then
    fail '本地和远端 main 已分叉，请保全在制文件并由单一发布会话整合。'
fi

if [[ "$mode" != --push-existing && "$(git rev-parse HEAD)" != "$remote_before" ]]; then
    fail '本地已有待发布提交，请先由发布者核对后使用 --push-existing。'
fi
if [[ "$mode" == --sync ]]; then
    exit 0
fi
if [[ "$mode" == -- ]]; then
    "$@"
fi

require_primary_main
candidate=$(git rev-parse HEAD)
if [[ "$candidate" == "$remote_before" ]]; then
    exit 0
fi
[[ "$(fetch_main)" == "$remote_before" ]] ||
    fail '远端 main 在本轮验证期间已更新，请重新核对后发布。'
git merge-base --is-ancestor "$remote_before" "$candidate" ||
    fail '待发布的 main 没有包含已核对的远端提交。'
git push origin "$candidate:refs/heads/main"
[[ "$(fetch_main)" == "$candidate" && "$(git rev-parse HEAD)" == "$candidate" ]] ||
    fail '推送后本地与远端 main 已再次变化，请核对。'
