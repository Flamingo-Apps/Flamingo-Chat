#!/usr/bin/env bash
# Creates the Secrets the production cluster needs (DEC-0099). Values are
# generated here or typed in, and go straight to the cluster - never to a
# file in the repository (AGENTS.md rule 9).
#
# A Secret that already exists is left alone, never regenerated: Postgres
# only reads POSTGRES_PASSWORD when it first initialises its data directory,
# so replacing that password later would lock every service out of the
# existing database. To rotate a value on purpose, delete that Secret first.
#
# Usage (against the production cluster's kubectl context):
#   GOOGLE_CLIENT_SECRET='...' deploy/prod/create-secrets.sh
set -euo pipefail

# Twice the requested length of random bytes, so enough letters and digits
# are left after base64's +, / and = are stripped.
random() { head -c "$(($1 * 2))" /dev/urandom | base64 -w0 | tr -dc 'A-Za-z0-9' | head -c "$1"; }
exists() { kubectl get secret "$1" >/dev/null 2>&1; }

echo "Creating Secrets in context: $(kubectl config current-context)"

if exists postgres-credentials; then
	echo "postgres-credentials: already exists - kept."
else
	pg_password=$(random 40)
	kubectl create secret generic postgres-credentials \
		--from-literal=POSTGRES_USER=flamingo \
		--from-literal=POSTGRES_PASSWORD="$pg_password" \
		--from-literal=POSTGRES_DB=flamingo >/dev/null
	echo "postgres-credentials: created (random password)."
fi

# Built from postgres-credentials, so the two can never disagree.
if exists flamingo-postgres; then
	echo "flamingo-postgres: already exists - kept."
else
	pg_password=$(kubectl get secret postgres-credentials -o jsonpath='{.data.POSTGRES_PASSWORD}' | base64 -d)
	kubectl create secret generic flamingo-postgres \
		--from-literal=POSTGRES_URL="postgres://flamingo:${pg_password}@postgres:5432/flamingo?sslmode=disable" >/dev/null
	echo "flamingo-postgres: created."
fi

if exists gateway-secrets; then
	echo "gateway-secrets: already exists - kept."
else
	if [ -z "${GOOGLE_CLIENT_SECRET:-}" ]; then
		read -rsp "Google OAuth client secret (from the Google Cloud console): " GOOGLE_CLIENT_SECRET
		echo
	fi
	[ -n "$GOOGLE_CLIENT_SECRET" ] || { echo "GOOGLE_CLIENT_SECRET is required" >&2; exit 1; }
	kubectl create secret generic gateway-secrets \
		--from-literal=SESSION_SIGNING_KEY="$(random 64)" \
		--from-literal=GOOGLE_CLIENT_SECRET="$GOOGLE_CLIENT_SECRET" >/dev/null
	echo "gateway-secrets: created (random session signing key)."
fi
