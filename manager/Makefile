.PHONY: help dev build clean

help:
	@echo "可用命令:"
	@echo "  make dev          - 开发模式运行（后端）"
	@echo "  make build        - 编译后端到 ../dist/{debug|release}"
	@echo "  make clean        - 清理本服务在 dist 下的产物"

OUT_DIR ?= ../dist
BUILD_TYPE ?= release
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
BIN_SUFFIX := $(if $(filter windows,$(GOOS)),.exe,)
GOFLAGS_DEBUG := -gcflags "all=-N -l"
GOFLAGS_RELEASE := -ldflags "-s -w"

# 本地 Go 构建缓存与工具链设置
LOCAL_GOMODCACHE := $(abspath ../.gomodcache)
LOCAL_GOPATH := $(abspath ../.gopath)
GOTOOLCHAIN ?= local

dev:
	cd backend && go run cmd/server/main.go

build:
	@echo "编译 manager ($(BUILD_TYPE)) 到 $(OUT_DIR)"
	@mkdir -p $(OUT_DIR)/$(BUILD_TYPE)/backend/manager/$(GOOS)-$(GOARCH)
	@if [ "$(BUILD_TYPE)" = "debug" ]; then \
	  (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOTOOLCHAIN=$(GOTOOLCHAIN) \
	   cd backend && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_DEBUG) -o ../../dist/$(BUILD_TYPE)/backend/manager/$(GOOS)-$(GOARCH)/manager$(BIN_SUFFIX) cmd/server/main.go); \
	else \
	  (GOMODCACHE=$(LOCAL_GOMODCACHE) GOPATH=$(LOCAL_GOPATH) GOTOOLCHAIN=$(GOTOOLCHAIN) \
	   cd backend && GOOS=$(GOOS) GOARCH=$(GOARCH) go build $(GOFLAGS_RELEASE) -o ../../dist/$(BUILD_TYPE)/backend/manager/$(GOOS)-$(GOARCH)/manager$(BIN_SUFFIX) cmd/server/main.go); \
	fi

clean:
	rm -rf ../dist/debug/backend/manager
	rm -rf ../dist/release/backend/manager
