#!/usr/bin/env bash

set -eou pipefail

echo "$GOPATH"

go mod tidy
pre-commit run --all
