#!/usr/bin/env bash
# Creates the pilot's Google Cloud project and GKE cluster (DEC-0023,
# DEC-0099). Every step is skipped when its resource already exists, so it
# is safe to re-run.
#
# Every flag is chosen against gcloud's own `container clusters create
# --help`, mostly to override a default that would cost more or duplicate
# what the cluster already runs itself:
#   --num-nodes 1              default is 3
#   --machine-type e2-standard-2 default is e2-medium (4 GiB, too small:
#                              the full stack measured ~3 GiB before GKE's
#                              own system pods)
#   --disk-type/--disk-size    default is a 100 GB balanced boot disk
#   --no-enable-managed-prometheus  on by default from GKE 1.27; Prometheus
#                              already runs in the cluster (DEC-0095)
#   --logging/--monitoring SYSTEM   only GKE's own components; application
#                              logs go to Loki
#   --maintenance-window 21:30 UTC = 03:00-07:00 IST: with one node, a node
#                              upgrade means a few minutes of downtime
# Zonal (not regional): GKE's free tier covers one zonal cluster's fee.
#
# Usage:
#   PROJECT_ID=<globally unique id> BILLING_ACCOUNT=<id from `gcloud billing accounts list`> \
#     deploy/gke/create-cluster.sh
set -euo pipefail

project=${PROJECT_ID:?set PROJECT_ID}
billing=${BILLING_ACCOUNT:?set BILLING_ACCOUNT}
zone=asia-south1-a
cluster=flamingo

if gcloud projects describe "$project" >/dev/null 2>&1; then
	echo "project $project: exists"
else
	gcloud projects create "$project" --name="Flamingo pilot"
fi
# Re-linking a project that is already on this account is not a no-op: it
# failed with "Cloud billing quota exceeded" for a project that was already
# linked (INC-0022).
if [ "$(gcloud billing projects describe "$project" --format='value(billingAccountName)')" = "billingAccounts/$billing" ]; then
	echo "project $project: billed to $billing"
else
	gcloud billing projects link "$project" --billing-account="$billing" >/dev/null
fi
gcloud services enable container.googleapis.com --project="$project"

if gcloud container clusters describe "$cluster" --project="$project" --zone="$zone" >/dev/null 2>&1; then
	echo "cluster $cluster: exists"
else
	gcloud container clusters create "$cluster" \
		--project="$project" \
		--zone="$zone" \
		--release-channel=regular \
		--num-nodes=1 \
		--machine-type=e2-standard-2 \
		--disk-type=pd-standard \
		--disk-size=50 \
		--no-enable-managed-prometheus \
		--logging=SYSTEM \
		--monitoring=SYSTEM \
		--maintenance-window=21:30
fi

# Writes the gke_<project>_<zone>_flamingo context deploy/prod/deploy.sh expects.
gcloud container clusters get-credentials "$cluster" --project="$project" --zone="$zone"
kubectl get nodes
