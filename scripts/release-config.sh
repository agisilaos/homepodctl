#!/usr/bin/env bash
# Repository-owned settings; common helpers are pinned to this reviewed bundle.
CLI_NAME="homepodctl"
FORMULA_NAME="homepodctl"
ARTIFACT_NAME="homepodctl"
DEFAULT_BRANCH="main"
DEFAULT_HOMEBREW_DESC="macOS CLI for Apple Music + HomePod control"
DEFAULT_HOMEBREW_LICENSE="MIT"
DEFAULT_HOMEBREW_TEST_ARG="version"
DEFAULT_FORMULA_PATH="Formula/homepodctl.rb"
DEFAULT_BUILD_PKG="./cmd/homepodctl"
RELEASE_LDFLAGS_TEMPLATE='-s -w -X main.version={{VERSION}} -X main.commit={{COMMIT}} -X main.date={{DATE}}'
RELEASE_VERSION_TEMPLATE='homepodctl {{VERSION}} ({{COMMIT}}) {{DATE}}'
RELEASE_GO_TOOLCHAIN="go1.27.1"
RELEASE_CGO_ENABLED=0
RELEASE_INCLUDE_LICENSE=0
CLI_TEMPLATE_FINGERPRINT="816f217b3a5c95477b24e3fb8ba1f3db5470654441b71b16e5385c9cb5b73290"
