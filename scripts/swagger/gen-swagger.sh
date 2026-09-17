#!/bin/bash
# 用途：为指定模块（或全部模块）重新生成 Swagger 文档
# 使用：bash scripts/swagger/gen-swagger.sh [module1 module2 ...] 或 all
# 示例：
#   bash scripts/swagger/gen-swagger.sh              # 生成所有模块
#   bash scripts/swagger/gen-swagger.sh all          # 同上
#   bash scripts/swagger/gen-swagger.sh system       # 只生成 system
#   bash scripts/swagger/gen-swagger.sh system standard model

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

# 所有支持发布检查 API 文档的后端模块
GO_MODULES=(system manager meta transfer orchestrator develop service monitor standard model quality security portal graph asset inference catalog workbench)
FASTAPI_MODULES=(agent copilot)
ALL_MODULES=("${GO_MODULES[@]}" "${FASTAPI_MODULES[@]}")

# 查找 swag 可执行文件
find_swag() {
    if command -v swag &>/dev/null; then
        command -v swag
    elif [ -f "${HOME}/go/bin/swag" ]; then
        echo "${HOME}/go/bin/swag"
    else
        echo ""
    fi
}

# 安装 swag（如果未安装）
ensure_swag() {
    local swag_bin
    swag_bin=$(find_swag)
    if [ -z "$swag_bin" ]; then
        echo "⚙️  swag 未安装，正在安装..." >&2
        go install github.com/swaggo/swag/cmd/swag@v1.16.4
        swag_bin="${HOME}/go/bin/swag"
        echo "✅ swag 安装完成: $swag_bin" >&2
    fi
    echo "$swag_bin"
}

is_fastapi_module() {
    local target=$1
    local module
    for module in "${FASTAPI_MODULES[@]}"; do
        [ "$target" = "$module" ] && return 0
    done
    return 1
}

# 解析参数
TARGETS=()
for arg in "$@"; do
    if [ "$arg" = "all" ]; then
        TARGETS=("${ALL_MODULES[@]}")
        break
    else
        TARGETS+=("$arg")
    fi
done

# 无参数时默认生成所有
if [ ${#TARGETS[@]} -eq 0 ]; then
    TARGETS=("${ALL_MODULES[@]}")
fi

# 验证模块名
for t in "${TARGETS[@]}"; do
    valid=false
    for m in "${ALL_MODULES[@]}"; do
        [ "$t" = "$m" ] && valid=true && break
    done
    if [ "$valid" = false ]; then
        echo "❌ 未知模块: $t"
        echo "支持的模块: ${ALL_MODULES[*]}"
        exit 1
    fi
done

echo "🔧 生成 Swagger 文档: ${TARGETS[*]}"
echo ""

SWAG_BIN=""
for module in "${TARGETS[@]}"; do
    if ! is_fastapi_module "$module"; then
        SWAG_BIN=$(ensure_swag)
        break
    fi
done

# 工作区锁覆盖指纹检查、并行生成和缓存发布；进程退出自动释放。
GENERATED_GO_MODULES="${GO_MODULES[*]}" FASTAPI_MODULE_NAMES="${FASTAPI_MODULES[*]}" python3 - "$ROOT_DIR" "$SWAG_BIN" "${TARGETS[@]}" <<'PY'
import concurrent.futures
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

root = Path(sys.argv[1]).resolve()
swag = sys.argv[2]
targets = list(dict.fromkeys(sys.argv[3:]))
fastapi = set(os.environ["FASTAPI_MODULE_NAMES"].split())
state = root / ".dev-state/swagger"
state.mkdir(parents=True, exist_ok=True)
manifest_path = state / "manifest.json"
generated_go = {root / module / "backend/docs/docs.go" for module in os.environ["GENERATED_GO_MODULES"].split()}


def digest_files(paths):
    digest = hashlib.sha256()
    for path in sorted(set(paths)):
        digest.update(str(path).encode() + b"\0")
        digest.update(hashlib.sha256(path.read_bytes()).digest())
    return digest.hexdigest()


def go_inputs():
    # swag 会递归扫描模块源码（含没有被 main 引用的 handler 注解）。
    # 保守覆盖全部本地模块，避免用编译依赖图遗漏注解或共享类型。
    environment = subprocess.check_output(["go", "env", "-json",
        "GOVERSION", "GOOS", "GOARCH", "GOARM", "GOARM64", "GOAMD64",
        "CGO_ENABLED", "GOEXPERIMENT", "GOFLAGS", "GOWORK", "GOROOT",
        "GOPATH", "GOMODCACHE", "GOTOOLCHAIN", "GO386", "GOMIPS", "GOMIPS64",
        "GOPPC64", "GORISCV64", "GOWASM", "CC", "CXX", "CGO_CFLAGS",
        "CGO_CPPFLAGS", "CGO_CXXFLAGS", "CGO_FFLAGS", "CGO_LDFLAGS"], cwd=root)
    raw = subprocess.check_output(["go", "list", "-m", "-json", "all"], cwd=root).decode()
    decoder = json.JSONDecoder()
    modules = []
    while raw.strip():
        item, end = decoder.raw_decode(raw.lstrip())
        modules.append(item)
        raw = raw.lstrip()[end:]
    files = {root / "scripts/swagger/gen-swagger.sh", Path(swag).resolve()}
    dependencies = []
    for module in modules:
        replacement = module.get("Replace", {})
        dependencies.append({
            "path": module["Path"], "version": module.get("Version"),
            "replace": {key: replacement.get(key) for key in ("Path", "Version")},
        })
        if not module.get("Main") and not (replacement and not replacement.get("Version")):
            continue
        directory = Path(replacement.get("Dir") or module["Dir"]).resolve()
        for parent, dirs, names in os.walk(directory):
            dirs[:] = sorted(d for d in dirs if d not in {".git", ".gomodcache", ".gopath", ".dev-state", ".dev-bins", ".cache", "node_modules", "venv", "__pycache__"})
            for name in names:
                path = Path(parent) / name
                if path in generated_go:
                    continue
                if name.endswith(".go") or name in {"go.mod", "go.sum", ".swaggo"}:
                    files.add(path)
    goenv = json.loads(environment)
    workspace = Path(goenv["GOWORK"])
    for path in (workspace, Path(str(workspace) + ".sum")):
        if path.is_file():
            files.add(path)
    payload = {"version": 1, "environment": goenv, "modules": dependencies, "files": digest_files(files)}
    return hashlib.sha256(json.dumps(payload, sort_keys=True).encode()).hexdigest()


def output_digest(module):
    directory = root / module / "backend/docs"
    paths = [directory / name for name in ("docs.go", "swagger.json", "swagger.yaml")]
    if not all(path.is_file() for path in paths):
        return None
    return digest_files(paths)


def save_manifest(manifest):
    fd, temporary = tempfile.mkstemp(prefix="manifest-", dir=state)
    try:
        with os.fdopen(fd, "w") as stream:
            json.dump(manifest, stream, sort_keys=True)
        os.replace(temporary, manifest_path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def generate(module):
    directory = root / module / "backend"
    print(f"  📄 [{module}] 生成中...", flush=True)
    if module in fastapi:
        python = directory / "venv/bin/python"
        executable = str(python) if os.access(python, os.X_OK) else shutil.which("python3")
        command = [executable, "-c", 'import json; from pathlib import Path; from main import app; Path("openapi.json").write_text(json.dumps(app.openapi(), ensure_ascii=False, indent=2, sort_keys=True) + "\\n", encoding="utf-8")']
    else:
        command = [swag, "init", "-g", "cmd/server/main.go", "-o", "docs", "--parseDependency", "--parseInternal", "-q"]
    subprocess.run(command, cwd=directory, check=True, pass_fds=(lock.fileno(),))
    if module not in fastapi and output_digest(module) is None:
        raise RuntimeError(f"{module}: 生成产物不完整")
    print(f"  ✅ [{module}] 完成", flush=True)
    return output_digest(module) if module not in fastapi else None


with (state / "generation.lock").open("a") as lock:
    fcntl.flock(lock, fcntl.LOCK_EX)
    try:
        manifest = json.loads(manifest_path.read_text())
        if not isinstance(manifest, dict):
            manifest = {}
    except (OSError, ValueError):
        manifest = {}
    go_targets = [module for module in targets if module not in fastapi]
    before = go_inputs() if go_targets else None
    pending = []
    outputs = {}
    for module in targets:
        output = output_digest(module) if module in go_targets else None
        if output and manifest.get(module) == {"input": before, "output": output}:
            outputs[module] = output
            print(f"  ♻️  [{module}] 输入和产物未变化，复用 Swagger", flush=True)
        else:
            pending.append(module)
            manifest.pop(module, None)
    # 先清除待生成模块的记录，失败或中断后不能使用旧记录。
    save_manifest(manifest)
    failed = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=max(1, len(pending))) as pool:
        futures = {pool.submit(generate, module): module for module in pending}
        for future in concurrent.futures.as_completed(futures):
            try:
                outputs[futures[future]] = future.result()
            except Exception as error:
                module = futures[future]
                failed.append(module)
                print(f"  ❌ [{module}] 生成失败: {error}", file=sys.stderr, flush=True)
    if failed:
        sys.exit(1)
    if go_targets and go_inputs() != before:
        print("❌ 生成期间 Go Swagger 输入发生变化，请重新执行", file=sys.stderr)
        sys.exit(1)
    for module in go_targets:
        if output_digest(module) != outputs[module]:
            print(f"❌ [{module}] 校验期间 Swagger 产物发生变化，请重新执行", file=sys.stderr)
            sys.exit(1)
        manifest[module] = {"input": before, "output": outputs[module]}
    save_manifest(manifest)
    print(f"\n✅ 全部完成（{len(targets)} 个模块，{len(targets) - len(pending)} 个复用）", flush=True)
PY
