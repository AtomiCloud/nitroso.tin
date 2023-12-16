#!/bin/bash

set -eou pipefail

# This script generates the SDK for the current platform.
# It is used by the CI to generate the SDKs for all platforms.

curl -o swagger.json "http://api.zinc.nitroso.lapras.lvh.me:20010/swagger/v1/swagger.json"
yq eval swagger.json -P -oy >swagger.yaml

rm swagger.json

oapi-codegen -package zinc swagger.yaml >lib/zinc/main.go

rm swagger.yaml
