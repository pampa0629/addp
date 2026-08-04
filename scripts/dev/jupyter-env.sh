#!/bin/bash

JUPYTER_ENV_FINGERPRINT_VERSION="jupyter-env-v1"

jupyter_dependency_fingerprint() {
  local project_root="$1"
  local file
  local files=(
    "engines/jupyter/requirements.txt"
    "common-python/pyproject.toml"
  )

  {
    printf '%s\n' "$JUPYTER_ENV_FINGERPRINT_VERSION"
    for file in "${files[@]}"; do
      printf '%s %s\n' "$file" "$(git -C "$project_root" hash-object "$project_root/$file")"
    done
  } | git -C "$project_root" hash-object --stdin
}

jupyter_runtime_imports_available() {
  local python_bin="$1"

  "$python_bin" - <<'PY' >/dev/null 2>&1
import sys

if sys.version_info < (3, 11):
    raise SystemExit(1)

import dotenv
import flask
import folium
import geopandas
import gunicorn
import ipykernel
import jupyterlab
import matplotlib
import minio
import pandas
import papermill
import plotly
import psycopg2
import pyarrow
import pymysql
import sqlalchemy
from addp_common.notebook import engines
PY
}

select_jupyter_python() {
  local candidate
  local candidates=(
    "/opt/homebrew/bin/python3.12"
    "python3.12"
    "/opt/homebrew/bin/python3.11"
    "python3.11"
    "/opt/homebrew/bin/python3.13"
    "python3.13"
    "/opt/homebrew/bin/python3"
    "python3"
  )

  for candidate in "${candidates[@]}"; do
    if command -v "$candidate" >/dev/null 2>&1 &&
       "$candidate" -c 'import sys; raise SystemExit(0 if sys.version_info >= (3, 11) else 1)' >/dev/null 2>&1; then
      command -v "$candidate"
      return 0
    fi
  done

  echo "✗ 未找到可用的 Python 3.11+，请先安装 Python 3.12" >&2
  return 1
}

ensure_jupyter_python_env() {
  local project_root="$1"
  local engine_dir="$project_root/engines/jupyter"
  local venv_dir="$engine_dir/venv"
  local python_bin="$venv_dir/bin/python"
  local fingerprint_file="$venv_dir/.addp-dependency-fingerprint"
  local expected_fingerprint
  local installed_fingerprint=""
  local install_reason=""
  local bootstrap_python

  expected_fingerprint="$(jupyter_dependency_fingerprint "$project_root")"

  if [ ! -x "$python_bin" ]; then
    bootstrap_python="$(select_jupyter_python)"
    if [ -d "$venv_dir" ]; then
      echo "Jupyter 虚拟环境不可用，重新创建..."
      rm -rf "$venv_dir"
    else
      echo "首次启动，创建 Jupyter Python 虚拟环境..."
    fi
    echo "  使用 $($bootstrap_python --version 2>&1)"
    "$bootstrap_python" -m venv "$venv_dir"
    install_reason="新建虚拟环境"
  else
    installed_fingerprint="$(cat "$fingerprint_file" 2>/dev/null || true)"
    if [ "$installed_fingerprint" != "$expected_fingerprint" ]; then
      install_reason="依赖声明已变化"
    elif ! jupyter_runtime_imports_available "$python_bin"; then
      install_reason="运行时依赖缺失或损坏"
    elif ! "$python_bin" -m pip check >/dev/null 2>&1; then
      install_reason="Python 包依赖不一致"
    fi
  fi

  if [ -z "$install_reason" ]; then
    echo "Jupyter 虚拟环境依赖完整，跳过安装"
    return 0
  fi

  echo "同步 Jupyter Python 依赖：$install_reason"
  if ! "$python_bin" -m pip install --upgrade pip ||
     ! "$python_bin" -m pip install -r "$engine_dir/requirements.txt" ||
     ! "$python_bin" -m pip install -e "$project_root/common-python" ||
     ! "$python_bin" -m pip check ||
     ! jupyter_runtime_imports_available "$python_bin"; then
    echo "✗ Jupyter Python 依赖同步失败" >&2
    return 1
  fi

  printf '%s\n' "$expected_fingerprint" > "$fingerprint_file"
  echo "✓ Jupyter Python 依赖同步完成"
}
