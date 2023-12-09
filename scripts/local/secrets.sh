#!/usr/bin/env bash

set -eou pipefail

echo "🔏 Setting up secrets for local development..."
while ! (doppler me --json | jq -r '.workplace.name' | grep 'AtomiCloud') &>/dev/null; do

  doppler login
done
