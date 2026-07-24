#!/bin/bash
# 用途：静态校验真实 Gin 路由与 Swagger paths 的覆盖一致性
# 使用：
#   bash scripts/swagger/check-route-coverage.sh <module>
#   bash scripts/swagger/check-route-coverage.sh all

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"
cd "${ROOT_DIR}"

GO_MODULES=(system manager meta transfer orchestrator develop service monitor standard model quality portal graph asset)
FASTAPI_MODULES=(agent copilot)
ALL_MODULES=("${GO_MODULES[@]}" "${FASTAPI_MODULES[@]}")

TARGETS=()
for arg in "$@"; do
  if [ "$arg" = "all" ]; then
    TARGETS=("${ALL_MODULES[@]}")
    break
  else
    TARGETS+=("$arg")
  fi
done

if [ ${#TARGETS[@]} -eq 0 ]; then
  TARGETS=("${ALL_MODULES[@]}")
fi

is_known_module() {
  local target="$1"
  local module
  for module in "${ALL_MODULES[@]}"; do
    [ "$target" = "$module" ] && return 0
  done
  return 1
}

for target in "${TARGETS[@]}"; do
  if ! is_known_module "$target"; then
    echo "❌ 未知模块: $target"
    echo "支持的模块: ${ALL_MODULES[*]}"
    exit 1
  fi
done

if ! command -v python3 >/dev/null 2>&1; then
  echo "❌ 未找到 python3，无法执行 Swagger 覆盖校验"
  exit 1
fi

FAILED=()

check_module() {
  local module="$1"

  if [[ " ${FASTAPI_MODULES[*]} " == *" $module "* ]]; then
    local module_dir="${module}/backend"
    local openapi_file="${module_dir}/openapi.json"
    local python_bin="${ROOT_DIR}/${module_dir}/venv/bin/python"
    if [ ! -x "$python_bin" ]; then
      python_bin="$(command -v python3 || true)"
    fi
    if [ -z "$python_bin" ]; then
      echo "  ❌ [$module] 未找到可用 Python"
      return 1
    fi
    if [ ! -f "$openapi_file" ]; then
      echo "  ❌ [$module] 未找到 $openapi_file，请先运行 gen-swagger.sh"
      return 1
    fi
    if (cd "$module_dir" && "$python_bin" - "$ROOT_DIR/$openapi_file" <<'PY'
import json
import sys
from pathlib import Path

from main import app

document_path = Path(sys.argv[1])
expected = json.dumps(app.openapi(), ensure_ascii=False, indent=2, sort_keys=True) + "\n"
actual = document_path.read_text(encoding="utf-8")
if actual != expected:
    print("  ❌ FastAPI OpenAPI 投影与运行时路由不一致，请重新运行 gen-swagger.sh")
    raise SystemExit(1)
operation_count = sum(
    1
    for path_item in app.openapi().get("paths", {}).values()
    for method in path_item
    if method.lower() in {"get", "post", "put", "delete", "patch", "head", "options"}
)
print(f"  ✅ FastAPI OpenAPI 投影一致（{operation_count} 个公开路由方法）")
PY
    ); then
      return 0
    fi
    return 1
  fi

  local router_file="${module}/backend/internal/api/router.go"
  local main_file="${module}/backend/cmd/server/main.go"
  local swagger_file="${module}/backend/docs/swagger.json"

  if [ ! -f "$router_file" ]; then
    echo "  ⚠️  [$module] 未找到 $router_file，跳过"
    return 0
  fi
  if [ ! -f "$main_file" ]; then
    echo "  ⚠️  [$module] 未找到 $main_file，跳过"
    return 0
  fi
  if [ ! -f "$swagger_file" ]; then
    echo "  ❌ [$module] 未找到 $swagger_file，请先运行 gen-swagger.sh"
    return 1
  fi

  python3 - "$module" "$router_file" "$main_file" "$swagger_file" <<'PY'
import json
import re
import sys
from pathlib import Path

module, router_file, main_file, swagger_file = sys.argv[1:5]

def clean_path(path: str) -> str:
    if not path:
        return "/"
    path = re.sub(r"/+", "/", path.strip())
    if not path.startswith("/"):
        path = "/" + path
    if len(path) > 1:
        path = path.rstrip("/")
    return path

def join_path(prefix: str, suffix: str) -> str:
    if suffix == "":
        return clean_path(prefix or "/")
    return clean_path((prefix.rstrip("/") if prefix else "") + "/" + suffix.lstrip("/"))

def swaggerize(path: str) -> str:
    path = clean_path(path)
    if path.endswith("/*yformat"):
        path = path.removesuffix("/*yformat") + "/{y}.{format}"
    path = re.sub(r"/\*([A-Za-z_][A-Za-z0-9_]*)", r"/{\1}", path)
    return re.sub(r":([A-Za-z_][A-Za-z0-9_]*)", r"{\1}", path)

def should_exclude(path: str) -> bool:
    path = clean_path(path)
    lowered = path.lower()
    excluded_prefixes = (
        "/",
        "/health",
        "/swagger",
        "/docs",
        "/redoc",
        "/openapi.json",
        "/metrics",
        "/debug",
    )
    if lowered in excluded_prefixes:
        return True
    if any(lowered.startswith(prefix + "/") for prefix in excluded_prefixes if prefix != "/"):
        return True
    excluded_segments = ("/internal/", "/debug/")
    return any(segment in lowered + "/" for segment in excluded_segments)

main_text = Path(main_file).read_text(encoding="utf-8")
base_match = re.search(r"^\s*//\s*@BasePath\s+(\S+)", main_text, re.MULTILINE)
base_path = clean_path(base_match.group(1)) if base_match else ""
extra_base_paths_by_module = {
    "service": ["/tiles", "/wmts", "/ogc/tiles", "/ogc/features", "/api/query", "/api/gquery"],
}
documented_base_paths = [base_path] + [clean_path(p) for p in extra_base_paths_by_module.get(module, [])]

router_text = Path(router_file).read_text(encoding="utf-8")
group_prefix = {"router": ""}
api_groups = set()
routes = {}

group_re = re.compile(r'^\s*(\w+)\s*:=\s*(\w+)\.Group\("([^"]*)"\)')
route_re = re.compile(r'^\s*(\w+)\.(GET|POST|PUT|DELETE|PATCH|HEAD|OPTIONS)\("([^"]*)"')

for raw_line in router_text.splitlines():
    line = raw_line.split("//", 1)[0]
    group_match = group_re.search(line)
    if group_match:
        var_name, parent, suffix = group_match.groups()
        if parent in group_prefix:
            full = join_path(group_prefix[parent], suffix)
            group_prefix[var_name] = full
            if full.startswith("/api/v1/"):
                api_groups.add(full)
        continue

    route_match = route_re.search(line)
    if route_match:
        var_name, method, suffix = route_match.groups()
        if var_name not in group_prefix:
            continue
        full = swaggerize(join_path(group_prefix[var_name], suffix))
        if should_exclude(full):
            continue
        if documented_base_paths and not any(full == p or full.startswith(p + "/") for p in documented_base_paths):
            continue
        routes.setdefault(full, set()).add(method.lower())

if not base_path:
    print(f"  ❌ [{module}] main.go 缺少 @BasePath")
    sys.exit(1)

if base_path not in api_groups and not any(group.startswith(base_path + "/") for group in api_groups):
    groups = ", ".join(sorted(api_groups)) or "未发现 /api/v1/* 路由组"
    print(f"  ❌ [{module}] BasePath 不匹配: @BasePath {base_path}, router groups: {groups}")
    sys.exit(1)

try:
    swagger = json.loads(Path(swagger_file).read_text(encoding="utf-8"))
except Exception as exc:
    print(f"  ❌ [{module}] swagger.json 解析失败: {exc}")
    sys.exit(1)

swagger_base = clean_path(swagger.get("basePath") or base_path)
if swagger_base != base_path:
    print(f"  ❌ [{module}] swagger.json basePath={swagger_base} 与 main.go @BasePath={base_path} 不一致")
    sys.exit(1)

swagger_routes = {}
for raw_path, operations in (swagger.get("paths") or {}).items():
    if not isinstance(operations, dict):
        continue
    normalized = swaggerize(clean_path(raw_path))
    if any(normalized == p or normalized.startswith(p + "/") for p in documented_base_paths):
        full = normalized
    else:
        full = join_path(base_path, normalized)
    if should_exclude(full):
        continue
    methods = {
        method.lower()
        for method in operations.keys()
        if method.lower() in {"get", "post", "put", "delete", "patch", "head", "options"}
    }
    if methods:
        swagger_routes.setdefault(full, set()).update(methods)

missing = []
method_mismatch = []
for path, methods in sorted(routes.items()):
    doc_methods = swagger_routes.get(path)
    if doc_methods is None:
        for method in sorted(methods):
            missing.append((method, path))
        continue
    for method in sorted(methods - doc_methods):
        method_mismatch.append((method, path, sorted(doc_methods)))

stale = []
for path, methods in sorted(swagger_routes.items()):
    real_methods = routes.get(path)
    if real_methods is None:
        for method in sorted(methods):
            stale.append((method, path))
        continue
    for method in sorted(methods - real_methods):
        stale.append((method, path))

if not missing and not stale and not method_mismatch:
    print(f"  ✅ [{module}] Swagger 覆盖一致（{sum(len(v) for v in routes.values())} 个公开路由方法）")
    sys.exit(0)

print(f"  ❌ [{module}] Swagger 覆盖不一致")
if missing:
    print("    missing in swagger:")
    for method, path in missing:
        print(f"      {method.upper():6s} {path}")
if stale:
    print("    stale in swagger:")
    for method, path in stale:
        print(f"      {method.upper():6s} {path}")
if method_mismatch:
    print("    method mismatch:")
    for method, path, doc_methods in method_mismatch:
        print(f"      router:  {method.upper():6s} {path}")
        print(f"      swagger: {','.join(m.upper() for m in doc_methods)} {path}")
sys.exit(1)
PY
}

echo "🔎 校验 Swagger 路由覆盖: ${TARGETS[*]}"
echo ""

for module in "${TARGETS[@]}"; do
  if ! check_module "$module"; then
    FAILED+=("$module")
  fi
done

echo ""
if [ ${#FAILED[@]} -eq 0 ]; then
  echo "✅ Swagger 路由覆盖校验通过"
  exit 0
fi

echo "⚠️  Swagger 路由覆盖校验发现问题: ${FAILED[*]}"
if [ "${SWAGGER_COVERAGE_WARN_ONLY:-0}" = "1" ]; then
  echo "ℹ️  SWAGGER_COVERAGE_WARN_ONLY=1，本次仅告警"
  exit 0
fi
exit 1
