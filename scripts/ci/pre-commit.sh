#!/usr/bin/env bash
set -eou pipefail

go mod tidy
SKIP=a-golang-ci-lint pre-commit run --all
