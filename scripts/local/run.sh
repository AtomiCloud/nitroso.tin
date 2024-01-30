#!/usr/bin/env bash

file="$1"

# shellcheck disable=SC2124
dev=${@:2}
set -eou pipefail
[ "$file" = '' ] && file="./config/dev.yaml"

landscape="$(yq -r '.landscape' "$file")"
platform="$(yq -r '.platform' "$file")"
service="$(yq -r '.service' "$file")"

export LANDSCAPE="$landscape"
# shellcheck disable=SC2086
doppler run -p "$platform-$service" -c "$landscape" -- $dev
