#!/bin/bash

# URL of the endpoint
endpoint_url="$1"

rm proxy.txt

curl -s "$endpoint_url" | while IFS= read -r line; do
  # Extract each part of the proxy string
  ip=$(echo "$line" | cut -d':' -f1 | tr -d '\n' | tr -d '\r')
  port=$(echo "$line" | cut -d':' -f2 | tr -d '\n' | tr -d '\r')
  user=$(echo "$line" | cut -d':' -f3 | tr -d '\n' | tr -d '\r')
  password=$(echo "$line" | cut -d':' -f4 | tr -d '\n' | tr -d '\r')

  url="http://${user}:${password}@${ip}:${port};"
  # Concatenate into the new format
  printf "%s" "${url}" >>proxy.txt

done
