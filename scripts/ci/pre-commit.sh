#!/usr/bin/env bash

set -eou pipefail
go mod tidy
pre-commit run --all
