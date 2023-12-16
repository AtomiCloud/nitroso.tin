#!/usr/bin/env bash

dev_config="$1"

set -eou pipefail

[ "$dev_config" = '' ] && dev_config="./config/dev.yaml"

# check if dev config exists
if [ ! -f "$dev_config" ]; then
  echo "❌ Dev config '$dev_config' does not exist!"
  exit 1
fi

input="$(yq '.landscape' "$dev_config")"
config="./infra/k3d.$input.yaml"
echo "🧬 Attempting to start cluster '$input' using '$config'..."

# obtain existing cluster
current="$(k3d cluster ls -o json | jq -r --arg input "${input}" '.[] | select(.name == $input) | .name')"
if [ "$current" = "$input" ]; then
  echo "✅ Cluster already exist!"
else
  # ask if to create cluster
  echo "🥟 Cluster does not exist, creating..."
  k3d cluster create "$input" --config "$config" --wait
  echo "🚀 Cluster created!"
fi

echo "🛠 Generating kubeconfig"
mkdir -p "$HOME/.kube/configs"
mkdir -p "$HOME/.kube/k3dconfigs"

echo "📝 Writing to '$HOME/.kube/k3dconfigs/k3d-$input'"
k3d kubeconfig get "$input" >"$HOME/.kube/k3dconfigs/k3d-$input"
KUBECONFIG=$(cd ~/.kube/configs && find "$(pwd)"/* | awk 'ORS=":"')$(cd ~/.kube/k3dconfigs && find "$(pwd)"/* | awk 'ORS=":"') kubectl config view --flatten >~/.kube/config
chmod 600 ~/.kube/config
echo "✅ Generated kube config file"
# wait for cluster to be ready
echo "🕑 Waiting for cluster to be ready..."
kubectl --context "k3d-$input" -n kube-system wait --for=jsonpath=.status.readyReplicas=1 --timeout=300s deployment metrics-server
kubectl --context "k3d-$input" -n kube-system wait --for=jsonpath=.status.readyReplicas=1 --timeout=300s deployment coredns
kubectl --context "k3d-$input" -n kube-system wait --for=jsonpath=.status.readyReplicas=1 --timeout=300s deployment local-path-provisioner
kubectl --context "k3d-$input" -n kube-system wait --for=jsonpath=.status.succeeded=1 --timeout=300s job helm-install-traefik-crd
kubectl --context "k3d-$input" -n kube-system wait --for=jsonpath=.status.succeeded=1 --timeout=300s job helm-install-traefik
kubectl --context "k3d-$input" -n kube-system wait --for=jsonpath=.status.readyReplicas=1 --timeout=300s deployment traefik
echo "✅ Cluster is ready!"

# install external-secrets operator
echo "🛠 Installing external-secrets operator..."
helm repo add external-secrets https://charts.external-secrets.io
helm upgrade --install --kube-context "k3d-$input" external-secrets external-secrets/external-secrets -n external-secrets --create-namespace
kubectl --context "k3d-$input" -n external-secrets wait --for=jsonpath=.status.readyReplicas=1 --timeout=300s deployment external-secrets-webhook
kubectl --context "k3d-$input" -n external-secrets wait --for=jsonpath=.status.readyReplicas=1 --timeout=300s deployment external-secrets-cert-controller
kubectl --context "k3d-$input" -n external-secrets wait --for=jsonpath=.status.readyReplicas=1 --timeout=300s deployment external-secrets
echo "✅ Installed external-secrets operator!"

# create doppler secret
echo "🛠 Creating doppler secret..."
root_token="$(doppler secrets get SULFOXIDE_SOS -p "sulfoxide-sos" -c "$input" --plain | base64)"

kubectl --context "k3d-$input" -n external-secrets apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: root-token
type: Opaque
data:
  "ROOT_TOKEN": "$root_token"
EOF
echo "✅ Created doppler secret!"

# create doppler cluster secret store
echo "🛠 Creating doppler cluster secret store..."
kubectl --context "k3d-$input" -n external-secrets apply -f - <<EOF
apiVersion: external-secrets.io/v1beta1
kind: ClusterSecretStore
metadata:
  name: doppler
spec:
  provider:
    doppler:
      auth:
        secretRef:
          dopplerToken:
            name: root-token
            key: ROOT_TOKEN
            namespace: external-secrets
EOF
echo "✅ Created doppler cluster secret store!"
