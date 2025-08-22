#!/usr/bin/env bash

file="$1"

# shellcheck disable=SC2124
dev=${@:2}
set -eou pipefail
[ "$file" = '' ] && file="./config/dev.yaml"

landscape="$(yq -r '.landscape' "$file")"

export LANDSCAPE="$landscape"
# shellcheck disable=SC2086
infisical run --env="$landscape" -- $dev
