#!/usr/bin/env bash
# Installs or upgrades one observability Helm release for the dev Tiltfile,
# but only when something actually changed: the chart version or the values
# file (DEC-0097). Running `helm upgrade --install <repo/chart>` on every
# `tilt up` re-downloaded each chart from GitHub every time, and failed
# whenever that download was slow, even with the release installed and
# healthy (INC-0017).
#
# Usage: ensure-release.sh <release> <repo/chart> <version> <values-file>
set -euo pipefail

release=$1
chart_ref=$2
version=$3
values=$4
chart_name=${chart_ref#*/}

# Fingerprint of everything that decides what gets installed. Stored per
# release in a ConfigMap after a successful install, and compared next time.
want=$({ echo "$chart_ref@$version"; cat "$values"; } | sha256sum | cut -c1-16)
key=${release//-/_}
have=$(kubectl get configmap tilt-helm-releases -o "jsonpath={.data.$key}" 2>/dev/null || true)

if helm list -f "^${release}\$" --deployed -o yaml 2>/dev/null | grep -q "chart: ${chart_name}-${version}\$" \
	&& [ "$have" = "$want" ]; then
	echo "$release: already deployed as ${chart_name}-${version} with these values - nothing to do."
	exit 0
fi

# Install from Helm's local chart cache when that exact version is already
# there, so a re-install doesn't need the network either.
tgz="$(helm env HELM_REPOSITORY_CACHE)/${chart_name}-${version}.tgz"
if [ ! -f "$tgz" ]; then
	echo "$release: downloading ${chart_ref} ${version}..."
	helm pull "$chart_ref" --version "$version" -d "$(dirname "$tgz")"
fi

echo "$release: installing ${chart_name}-${version} (chart or values changed since the last install)..."
helm upgrade --install "$release" "$tgz" -f "$values" --wait --timeout 8m

kubectl create configmap tilt-helm-releases >/dev/null 2>&1 || true
kubectl patch configmap tilt-helm-releases --type merge -p "{\"data\":{\"$key\":\"$want\"}}" >/dev/null
echo "$release: done."
