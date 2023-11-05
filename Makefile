PROJECT_NAME              = auto-ssh
GOARCH                    ?= amd64
GO                        = go
BUILD_TYPE                = prod

# build output
BINARY           ?= ${PROJECT_NAME}
BUILD_IMAGE_NAME ?= ${PROJECT_GROUP}/$PROJECT_NAME}
BUILD_NUMBER     = $(shell git rev-list --count HEAD)
TARGET           ?= gitlab.teracloud.ninja/teracloud/saas-services/applications/tdsh

# workspace
WORKSPACE ?= $(realpath $(dir $(realpath $(firstword $(MAKEFILE_LIST)))))
GOPATH  ?= ${WORKSPACE}/vendor
BIN     = ${GOPATH}/bin
BUILD_DIR = ${WORKSPACE}/build
OUTPUT_DIR ?= ${BUILD_DIR}/bin

VERSION = $(shell git describe --tags --abbrev=0)
COMMIT  = $(shell git rev-parse --short=7 HEAD)
BRANCH  = $(shell git rev-parse --abbrev-ref HEAD)

# Symlink into GOPATH
GITHUB_PATH = gitlab.teracloud.ninja/teracloud/saas-services/applications
CURRENT_DIR = $(shell pwd)
BUILD_DIR_LINK = $(shell readlink ${BUILD_DIR})

export PATH := ${BIN}:$(PATH)
export GOPRIVATE:=*.teradata.com

# Setup the -ldflags option for go build here, interpolate the variable values
FLAGS_PKG=auto-ssh/main
LDFLAGS = --ldflags "-X ${FLAGS_PKG}.Version=${VERSION} -X ${FLAGS_PKG}.Commit=${COMMIT} -X ${FLAGS_PKG}.Branch=${BRANCH} -X ${FLAGS_PKG}.BuildNumber=${BUILD_NUMBER}"

all: lint darwin

lint:
	golangci-lint run

fmt:
	go fmt $$(go list ./... | grep -v /internal_vendor/);
	go fmt $$(go list ./... | grep -v /vendor/);
	goimports -local github.com/golangci/golangci-lint -w $$(find . -type f -iname \*.go)

vet:
	go vet $$(go list ./... | grep -v /internal_vendor/);
	go vet $$(go list ./... | grep -v /vendor/)


test-with-coverage:
	mkdir -p coverage
	go test ./... -coverpkg=./... -covermode=count -coverprofile coverage/coverage.txt
	go tool cover -func=coverage/coverage.txt -o coverage/profile.out
	echo `tail -1 coverage/profile.out`
	gocover-cobertura < coverage/coverage.txt > coverage/cobertura-coverage.xml

linux:
	GOOS=linux GOARCH=${GOARCH} go build ${LDFLAGS} -o ${BINARY}-linux-${GOARCH} ${TARGET};

darwin: OS="Darwin amd64"
darwin:
	GOOS=darwin GOARCH=${GOARCH} go build ${LDFLAGS} -o ${BINARY}-darwin-${GOARCH} ${TARGET};

windows:
	GOOS=windows GOARCH=${GOARCH} go build ${LDFLAGS} -o ${BINARY}-windows-${GOARCH}.exe ${TARGET};


