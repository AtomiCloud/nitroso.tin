#!/bin/bash

# URL of the endpoint
endpoint_url="$1"

rm helium.proxy.txt || true
rm tin.proxy.txt || true
rm proxy.tmp || true

curl -s "$endpoint_url" | while IFS= read -r line; do
  # Extract each part of the proxy string
  ip=$(echo "$line" | cut -d':' -f1 | tr -d '\n' | tr -d '\r')
  port=$(echo "$line" | cut -d':' -f2 | tr -d '\n' | tr -d '\r')
  user=$(echo "$line" | cut -d':' -f3 | tr -d '\n' | tr -d '\r')
  password=$(echo "$line" | cut -d':' -f4 | tr -d '\n' | tr -d '\r')

  url="http://${user}:${password}@${ip}:${port};"
  # Concatenate into the new format
  printf "%s" "${url}" >>proxy.tmp
done

# Read the content of the file into a variable
input_string=$(cat ./proxy.tmp)

# Set the Internal Field Separator to ';'
IFS=';' read -r -a entries <<<"$input_string"

# Join the first 200 entries into a variable `a`
a=$(
  IFS=';'
  echo "${entries[*]:0:300}"
)

# Join the remaining 300 entries into a variable `b`
b=$(
  IFS=';'
  echo "${entries[*]:300:700}"
)

# Print the variables if you want to check
printf "%s" "${a}" >helium.proxy.txt
printf "%s" "${b}" >tin.proxy.txt
