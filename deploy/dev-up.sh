#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

CLUSTER_NAME="boltrunner"

if ! kind get clusters | grep -q "^${CLUSTER_NAME}$"; then
  kind create cluster --name "${CLUSTER_NAME}" --config deploy/kind-config.yaml
fi

docker build -f deploy/Dockerfile.server -t boltrunner/server:local .
docker build -f deploy/Dockerfile.sidecar -t boltrunner/sidecar:local .
docker build -f deploy/Dockerfile.jmeter -t boltrunner/jmeter:local .

kind load docker-image boltrunner/server:local --name "${CLUSTER_NAME}"
kind load docker-image boltrunner/sidecar:local --name "${CLUSTER_NAME}"
kind load docker-image boltrunner/jmeter:local --name "${CLUSTER_NAME}"

kubectl apply -f deploy/k8s/namespace.yaml
kubectl apply -f deploy/k8s/rbac.yaml
kubectl apply -f deploy/k8s/postgres.yaml
kubectl apply -f deploy/k8s/backend.yaml

kubectl -n boltrunner rollout status deployment/boltrunner-postgres --timeout=120s
kubectl -n boltrunner rollout status deployment/boltrunner-backend --timeout=120s

echo "Port-forwarding boltrunner-backend to localhost:8080 (Ctrl+C to stop, or run deploy/dev-down.sh)"
kubectl -n boltrunner port-forward svc/boltrunner-backend 8080:8080
