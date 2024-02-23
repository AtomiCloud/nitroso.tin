#!/usr/bin/env bash

echo "$GOPATH"

set -eou pipefail

go mod tidy
pre-commit run --all
