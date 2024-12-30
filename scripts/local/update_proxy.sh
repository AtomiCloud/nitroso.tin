#!/bin/bash

set -eou pipefail

echo "⬇️ Retrieving token from webshare..."
token="$(infisical secrets --plain --env=raichu get ATOMI_WEBPROXY__API__TOKEN)"
echo "✅ Token retrieved"

echo "⬇️ Retrieving proxy config from webshare..."
config="$(curl "https://proxy.webshare.io/api/v2/proxy/config/" -H "Authorization: Token ${token}")"
echo "✅ Proxy config retrieved"

echo "⬇️ Processing proxy config..."
proxy_token="$(echo "${config}" | jq -r '.proxy_list_download_token')"
echo "✅ Proxy token retrieved"

echo "⬇️ Downloading proxy list..."
./scripts/local/process_proxy.sh "https://proxy.webshare.io/api/v2/proxy/list/download/${proxy_token}/-/any/username/direct/-/"
echo "✅ Proxy list downloaded"

echo "⬆️ Uploading proxy list for tin..."
tin_project="--projectId=df53bb81-dee0-4479-b515-3cab9af7386f"
infisical secrets set --env=lapras "${tin_project}" "ATOMI_KTMB__PROXY=$(cat ./tin.proxy.txt)"
infisical secrets set --env=pichu "${tin_project}" "ATOMI_KTMB__PROXY=$(cat ./tin.proxy.txt)"
infisical secrets set --env=pikachu "${tin_project}" "ATOMI_KTMB__PROXY=$(cat ./tin.proxy.txt)"
infisical secrets set --env=raichu "${tin_project}" "ATOMI_KTMB__PROXY=$(cat ./tin.proxy.txt)"
echo "✅ Proxy list uploaded"

echo "⬆️ Uploading proxy list for helium..."
helium_project="--projectId=cc897910-0fe7-4784-ac3d-be9847fca2d9"
infisical secrets set --env=lapras "${helium_project}" "ATOMI_APP__SEARCHER__PROXY=$(cat ./helium.proxy.txt)"
infisical secrets set --env=pichu "${helium_project}" "ATOMI_APP__SEARCHER__PROXY=$(cat ./helium.proxy.txt)"
infisical secrets set --env=pikachu "${helium_project}" "ATOMI_APP__SEARCHER__PROXY=$(cat ./helium.proxy.txt)"
infisical secrets set --env=raichu "${helium_project}" "ATOMI_APP__SEARCHER__PROXY=$(cat ./helium.proxy.txt)"
echo "✅ Proxy list uploaded"
