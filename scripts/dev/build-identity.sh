#!/bin/bash
# build-identity.sh - ADDP Go 构建身份和原子发布辅助函数

addp_hash_files() {
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256
  else
    sha256sum
  fi
}

addp_source_fingerprint() {
  local source_dir="$1"
  shift

  local project_root_logical project_root_physical
  project_root_logical=$(cd "$PROJECT_ROOT" && pwd -L)
  project_root_physical=$(cd "$PROJECT_ROOT" && pwd -P)

  local file_list raw_file_list
  file_list=$(mktemp "${TMPDIR:-/tmp}/addp-build-files.XXXXXX")
  raw_file_list=$(mktemp "${TMPDIR:-/tmp}/addp-build-inputs.XXXXXX")

  local go_list_command="${ADDP_GO_LIST_COMMAND:-go}"
  local list_template='{{ $dir := .Dir }}{{ range .GoFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .CgoFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .CFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .CXXFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .MFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .HFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .FFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .SFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .SwigFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .SwigCXXFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .SysoFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ range .EmbedFiles }}{{ printf "%s/%s\n" $dir . }}{{ end }}{{ with .Module }}{{ .GoMod }}{{ "\n" }}{{ end }}'

  if ! (cd "$source_dir" && "$go_list_command" list -deps -f "$list_template" "$@") > "$raw_file_list"; then
    rm -f "$file_list" "$raw_file_list"
    return 1
  fi

  (
    cd "$project_root_physical"
    while IFS= read -r file; do
      [ -f "$file" ] || continue
      local relative_file=""
      case "$file" in
        "$project_root_logical"/*)
          relative_file="${file#"$project_root_logical/"}"
          ;;
        "$project_root_physical"/*)
          relative_file="${file#"$project_root_physical/"}"
          ;;
      esac
      case "$relative_file" in
        ''|.git/*|.gomodcache/*|.gopath/*|.dev-bins/*|.dev-state/*) ;;
        *) printf '%s\n' "$relative_file" ;;
      esac
    done < "$raw_file_list"
    [ -f "go.work" ] && printf '%s\n' "go.work"
    [ -f "go.work.sum" ] && printf '%s\n' "go.work.sum"
  ) | LC_ALL=C sort -u > "$file_list"

  local module_sum_list
  module_sum_list=$(mktemp "${TMPDIR:-/tmp}/addp-build-sums.XXXXXX")
  sed -n '/\/go\.mod$/p' "$file_list" | sed 's#/go\.mod$#/go.sum#' > "$module_sum_list"
  while IFS= read -r go_sum; do
    [ -f "${project_root_physical}/${go_sum}" ] && printf '%s\n' "$go_sum"
  done < "$module_sum_list" >> "$file_list"
  LC_ALL=C sort -u -o "$file_list" "$file_list"

  local fingerprint
  if command -v shasum >/dev/null 2>&1; then
    fingerprint=$(cd "$project_root_physical" && tr '\n' '\0' < "$file_list" | xargs -0 shasum -a 256 | addp_hash_files | awk '{print $1}')
  else
    fingerprint=$(cd "$project_root_physical" && tr '\n' '\0' < "$file_list" | xargs -0 sha256sum | addp_hash_files | awk '{print $1}')
  fi
  rm -f "$file_list" "$raw_file_list" "$module_sum_list"
  printf 'sha256:%s\n' "$fingerprint"
}

addp_build_fingerprint_path() {
  local output_path="$1"
  printf '%s.fingerprint\n' "${PROJECT_ROOT}/${output_path}"
}

addp_go_build_is_current() {
  local source_dir="$1"
  local output_path="$2"
  shift 2

  local fingerprint_path
  fingerprint_path=$(addp_build_fingerprint_path "$output_path")
  [ -x "${PROJECT_ROOT}/${output_path}" ] || return 1
  [ -f "$fingerprint_path" ] || return 1

  local current_fingerprint recorded_fingerprint
  current_fingerprint=$(addp_source_fingerprint "$source_dir" "$@") || return 1
  recorded_fingerprint=$(sed -n '1p' "$fingerprint_path")
  [ "$current_fingerprint" = "$recorded_fingerprint" ]
}

addp_atomic_go_build() (
  local module_name="$1"
  local source_dir="$2"
  local output_path="$3"
  shift 3

  case "$output_path" in
    /*|../*|*/../*|*/..)
      echo "  ✗ ${module_name} 构建输出必须是工作区内的规范相对路径: ${output_path}" >&2
      return 1
      ;;
  esac

  local tmp_dir="${PROJECT_ROOT}/.dev-bins/.tmp"
  local build_pid="${BASHPID:-$$}"
  local go_command="${ADDP_GO_COMMAND:-go}"
  mkdir -p "$tmp_dir" "$(dirname "${PROJECT_ROOT}/${output_path}")"
  local tmp_binary
  tmp_binary=$(mktemp "${tmp_dir}/$(basename "$output_path").${build_pid}.XXXXXX")
  trap 'rm -f "$tmp_binary"' EXIT
  trap 'exit 130' INT
  trap 'exit 143' TERM

  local fingerprint_before fingerprint_after git_commit built_at build_id ldflags
  if ! fingerprint_before=$(addp_source_fingerprint "$source_dir" "$@"); then
    echo "  ✗ ${module_name} 无法解析 Go 构建输入" >&2
    rm -f "$tmp_binary"
    return 1
  fi
  git_commit=$(git rev-parse HEAD 2>/dev/null || printf 'unknown')
  built_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
  build_id="$(date -u '+%Y%m%dT%H%M%SZ')-${module_name}-${build_pid}-${tmp_binary##*.}"
  ldflags="-X github.com/addp/common/buildinfo.BuildID=${build_id} -X github.com/addp/common/buildinfo.GitCommit=${git_commit} -X github.com/addp/common/buildinfo.SourceFingerprint=${fingerprint_before} -X github.com/addp/common/buildinfo.BuiltAt=${built_at}"

  if ! (cd "$source_dir" && "$go_command" build -ldflags "$ldflags" -o "$tmp_binary" "$@"); then
    rm -f "$tmp_binary"
    return 1
  fi

  if ! fingerprint_after=$(addp_source_fingerprint "$source_dir" "$@"); then
    echo "  ✗ ${module_name} 无法重新解析 Go 构建输入" >&2
    rm -f "$tmp_binary"
    return 1
  fi
  if [ "$fingerprint_after" != "$fingerprint_before" ]; then
    echo "  ✗ ${module_name} 构建期间源码发生变化，已拒绝发布产物" >&2
    rm -f "$tmp_binary"
    return 1
  fi

  chmod +x "$tmp_binary"
  mv -f "$tmp_binary" "${PROJECT_ROOT}/${output_path}"
  printf '%s\n' "$fingerprint_after" > "$(addp_build_fingerprint_path "$output_path")"
)
