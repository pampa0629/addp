#!/usr/bin/env bash
# Canonical GaussDB developer media facts. Never substitute openGauss media.
GAUSSDB_OFFICIAL_VERSION=V2.0-10.0.0
# Public candidate is verified only in the standalone certification module.
GAUSSDB_GO_MODULE=github.com/HuaweiCloudDeveloper/gaussdb-go
GAUSSDB_GO_VERSION=v1.0.0-rc1
GAUSSDB_GO_SUM=h1:OIZ83SgbK0ImF/vKFSfNoCAQWQySx5sYhj0RgsWbMbA=
GAUSSDB_GO_MOD_SUM=h1:Xf3AtRet+/ygewEMfzt0FbIFydPkv47Hx5Xz3w07+yo=
GAUSSDB_OFFICIAL_BASE_IMAGE=ubuntu:22.04@sha256:829f6df217bcbae2b371026e81711d1a787c61b2967ad09d015063663ebafbf7

gaussdb_select_official_media() {
    case "${1:-$(uname -m)}" in
        x86_64|amd64)
            GAUSSDB_OFFICIAL_MEDIA_URL=https://dbs-download.obs.cn-north-1.myhuaweicloud.com/GaussDB/1789441453338/GaussDB_V2.0-10.0.0_trial_Ubuntu_X86_20260915102549.zip
            GAUSSDB_OFFICIAL_MEDIA_SHA256=c53cf2021d86c4820e8c6fddf13a098e1b2b1f4a2ca142c6dcf1ff2fa9ab54a8
            ;;
        aarch64|arm64)
            GAUSSDB_OFFICIAL_MEDIA_URL=https://dbs-download.obs.cn-north-1.myhuaweicloud.com/GaussDB/1789441439039/GaussDB_V2.0-10.0.0_trial_Ubuntu_ARM_20260915102549.zip
            GAUSSDB_OFFICIAL_MEDIA_SHA256=a6442f7c12c396aae2e96e3dde8b4eca2eac45ab26dce1735efc530f447990e5
            ;;
        *) echo 'unsupported GaussDB media architecture' >&2; return 1 ;;
    esac
}
