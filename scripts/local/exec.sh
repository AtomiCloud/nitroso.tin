#!/usr/bin/env bash

file="$1"

# shellcheck disable=SC2124
dev=${@:2}

set -eou pipefail

[ "$file" = '' ] && file="./config/dev.yaml"

landscape="$(yq -r '.landscape' "$file")"
platform="$(yq -r '.platform' "$file")"
service="$(yq -r '.service' "$file")"

name="$platform-$service-dev-proxy"
target="pod/$name"

# shellcheck disable=SC2086
mirrord exec --context "k3d-$landscape" --target "$target" --fs-mode local -e -n "$platform" -- $dev
