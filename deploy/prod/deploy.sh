#!/usr/bin/env bash
# Deploys the pilot to the production cluster (DEC-0099), in dependency
# order: data stores, monitoring, then the services, then the tunnel that
# makes gateway reachable. Safe to re-run; every step is an apply/upgrade.
#
# Usage, from the repository root, with kubectl pointed at the GKE cluster:
#   deploy/prod/deploy.sh <image-tag>      # the git SHA the images were built from
set -euo pipefail
cd "$(dirname "$0")/../.."

tag=${1:?usage: deploy/prod/deploy.sh <image-tag>}
ctx=$(kubectl config current-context)
case "$ctx" in
	gke_*) ;;
	*) echo "Refusing: kubectl context '$ctx' is not a GKE cluster - this would deploy production settings somewhere else." >&2; exit 1 ;;
esac
echo "Deploying image tag $tag to $ctx"

for s in postgres-credentials flamingo-postgres gateway-secrets ghcr-pull tunnel-token; do
	kubectl get secret "$s" >/dev/null 2>&1 || { echo "Missing Secret $s - see deploy/prod/create-secrets.sh and deploy/k8s/prod/cloudflared.yaml" >&2; exit 1; }
done

services=(identity chat matching presence moderation delivery-worker persistence-worker gateway)
helm_values() { echo "-f deploy/helm/service/values/$1.yaml -f deploy/helm/service/values/prod/$1.yaml"; }

# Checked before anything is changed, so a bad render never half-deploys.
echo "== Checking the production render"
for s in "${services[@]}"; do
	# shellcheck disable=SC2046
	helm template "$s" deploy/helm/service $(helm_values "$s") --set image.tag="$tag"
done | python3 deploy/prod/check-render.py

echo "== Data stores"
kubectl apply -f deploy/k8s/prod/postgres.yaml -f deploy/k8s/prod/redis.yaml -f deploy/k8s/prod/kafka.yaml
kubectl rollout status statefulset/postgres --timeout=5m
kubectl rollout status statefulset/kafka --timeout=5m
kubectl rollout status deployment/redis --timeout=5m
# A Job's pod template is immutable, so it is only created once; it exits
# after making the topics, which then live on Kafka's disk.
kubectl get job kafka-create-topics >/dev/null 2>&1 || kubectl apply -f deploy/k8s/prod/kafka-topics.yaml
kubectl wait --for=condition=complete job/kafka-create-topics --timeout=5m

echo "== Monitoring"
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1 || true
helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1 || true
E=deploy/observability/ensure-release.sh
O=deploy/observability
$E kube-prometheus-stack prometheus-community/kube-prometheus-stack 92.10.1 "$O/kube-prometheus-stack-values.yaml" "$O/prod/kube-prometheus-stack-values.yaml"
$E loki grafana/loki 7.3.0 "$O/loki-values.yaml" "$O/prod/loki-values.yaml"
$E alloy grafana/alloy 1.14.0 "$O/alloy-values.yaml" "$O/prod/alloy-values.yaml"
kubectl create configmap grafana-dashboards-flamingo --from-file="$O/dashboards/" --dry-run=client -o yaml \
	| kubectl label --local -f - grafana_dashboard=1 -o yaml | kubectl apply -f -

echo "== Services"
for s in "${services[@]}"; do
	# shellcheck disable=SC2046
	helm upgrade --install "$s" deploy/helm/service $(helm_values "$s") --set image.tag="$tag" --wait --timeout 5m
done

echo "== Tunnel"
kubectl apply -f deploy/k8s/prod/cloudflared.yaml
kubectl rollout status deployment/cloudflared --timeout=3m

echo "Done. Grafana: kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80"
