PROJECT_NAME              = auto-ssh

# build output
BINARY           ?= ${PROJECT_NAME}
BUILD_NUMBER     = $(shell git rev-list --count HEAD)
TARGET           ?= auto-ssh

VERSION = $(shell git describe --tags --abbrev=0)
COMMIT  = $(shell git rev-parse --short=7 HEAD)
BRANCH  = $(shell git rev-parse --abbrev-ref HEAD)

# Symlink into GOPATH
GITHUB_PATH = https://github.com/jfigge
CURRENT_DIR = $(shell pwd)
BUILD_DIR_LINK = $(shell readlink ${BUILD_DIR})

export PATH := ${BIN}:$(PATH)
export GOPRIVATE:=*.teradata.com

# Setup the -ldflags option for go build here, interpolate the variable values
FLAGS_PKG=main
LDFLAGS = --ldflags "-X ${FLAGS_PKG}.Version=${VERSION} -X ${FLAGS_PKG}.Commit=${COMMIT} -X ${FLAGS_PKG}.Branch=${BRANCH} -X ${FLAGS_PKG}.BuildNumber=${BUILD_NUMBER}"

all: lint darwin linux windows

lint:
	golangci-lint run
linux:
	@echo Building Linux for amd
	@GOOS=linux GOARCH=amd64 go build ${LDFLAGS} -o ${BINARY}-linux-amd64 ${TARGET};
	@echo Building Linux for arm
	@GOOS=linux GOARCH=arm64 go build ${LDFLAGS} -o ${BINARY}-linux-arm64} ${TARGET};

darwin:
	@echo Building Darwin for amd
	@GOOS=darwin GOARCH=amd64 go build ${LDFLAGS} -o ${BINARY}-darwin-amd64 ${TARGET};
	@echo Building Darwin for arm
	@GOOS=darwin GOARCH=arm64 go build ${LDFLAGS} -o ${BINARY}-darwin-arm64 ${TARGET};

windows:
	@echo Building Windows for amd
	@GOOS=windows GOARCH=amd64 go build ${LDFLAGS} -o ${BINARY}-windows-amd64.exe ${TARGET};
