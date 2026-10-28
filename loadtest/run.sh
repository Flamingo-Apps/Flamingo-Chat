#!/usr/bin/env bash
# Runs the k6 chat load test (loadtest/k6/chat.js) as a Job INSIDE the
# current cluster, so it talks straight to the gateway: no internet, no
# Cloudflare, and a result that is about the backend itself. The generator
# shares the node with the app, so its CPU is counted against the app and
# the numbers are on the cautious side; it is reported separately.
#
# One run per level of simultaneous users, so the results line up in a table
# and the point where it stops coping is visible. Results are written to
# loadtest/results/ and are meant to be committed with the method (DEC-0025).
#
#   LEVELS="25 50 100" loadtest/run.sh                      # local kind cluster
#   CONFIRM_PRODUCTION=yes LEVELS="50 100 200" loadtest/run.sh   # the GKE cluster
#
# Settings: LEVELS, HOLD_S (default 120), RAMP_S (30), SESSION_S (60),
# SEND_EVERY_S (3). The gateway's signup limits are raised for the duration
# and put back afterwards, even if the run fails.
set -euo pipefail
cd "$(dirname "$0")/.."

ctx=$(kubectl config current-context)
case "$ctx" in
	kind-*) env_name=local ;;
	gke_*)
		[ "${CONFIRM_PRODUCTION:-}" = yes ] || { echo "Refusing: '$ctx' is the production cluster. Set CONFIRM_PRODUCTION=yes to load-test it." >&2; exit 1; }
		env_name=gke ;;
	*) echo "Refusing: '$ctx' is neither a local kind cluster nor a GKE cluster." >&2; exit 1 ;;
esac

levels=${LEVELS:-25}
hold=${HOLD_S:-120}; ramp=${RAMP_S:-30}; session=${SESSION_S:-60}; every=${SEND_EVERY_S:-3}
k6_image=grafana/k6:2.3.0
stamp=$(date -u +%Y%m%dT%H%MZ)
results=loadtest/results; mkdir -p "$results"

node_type=$(kubectl get nodes -o jsonpath='{.items[0].metadata.labels.node\.kubernetes\.io/instance-type}')
node_cpu=$(kubectl get nodes -o jsonpath='{.items[0].status.capacity.cpu}')
node_mem_ki=$(kubectl get nodes -o jsonpath='{.items[0].status.capacity.memory}')
nodes=$(kubectl get nodes --no-headers | wc -l)
gateway_image=$(kubectl get deploy gateway -o jsonpath='{.spec.template.spec.containers[0].image}')
echo "== $env_name cluster '$ctx': $nodes node(s), type '${node_type:-unknown}', $node_cpu CPU, $node_mem_ki memory; gateway $gateway_image"

# --- raise the signup limits for the test, and always put them back -------
limit_vars=(LIMIT_GUEST_SIGNUPS_PER_IP_PER_HOUR LIMIT_GUEST_SIGNUPS_GLOBAL_PER_HOUR)
declare -A original
for v in "${limit_vars[@]}"; do
	original[$v]=$(kubectl get deploy gateway -o "jsonpath={.spec.template.spec.containers[0].env[?(@.name==\"$v\")].value}")
done
restore() {
	echo "== putting the gateway's limits back"
	for v in "${limit_vars[@]}"; do
		if [ -n "${original[$v]}" ]; then kubectl set env deploy/gateway "$v=${original[$v]}" >/dev/null; else kubectl set env deploy/gateway "$v-" >/dev/null; fi
	done
	kubectl rollout status deploy/gateway --timeout=3m >/dev/null || true
	[ -n "${pf_pid:-}" ] && kill "$pf_pid" 2>/dev/null || true
	kubectl delete configmap loadtest-script --ignore-not-found >/dev/null
}
trap restore EXIT
kubectl set env deploy/gateway "${limit_vars[0]}=1000000" "${limit_vars[1]}=1000000" >/dev/null
kubectl rollout status deploy/gateway --timeout=3m

kubectl create configmap loadtest-script --from-file=chat.js=loadtest/k6/chat.js --dry-run=client -o yaml | kubectl apply -f - >/dev/null

prom_svc=$(kubectl get svc -o name | grep -E 'kube-prometheus-stack-prometheus$' | head -1)
kubectl port-forward "$prom_svc" 19099:9090 >/dev/null 2>&1 &
pf_pid=$!
sleep 3

printf '\n%6s  %s\n' users "p50ms  p95ms  p99ms  recv/s  matched  errors  peak resources"
for level in $levels; do
	job="loadtest-$level-$(date -u +%H%M%S)"
	started=$(date -u +%s)
	kubectl apply -f - >/dev/null <<YAML
apiVersion: batch/v1
kind: Job
metadata:
  name: $job
spec:
  backoffLimit: 0
  activeDeadlineSeconds: $((ramp + hold + 200))
  ttlSecondsAfterFinished: 600
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: k6
          image: $k6_image
          args: ["run", "/scripts/chat.js"]
          env:
            - { name: TARGET, value: "$level" }
            - { name: HOLD_S, value: "$hold" }
            - { name: RAMP_S, value: "$ramp" }
            - { name: SESSION_S, value: "$session" }
            - { name: SEND_EVERY_S, value: "$every" }
          resources:
            requests: { cpu: 250m, memory: 256Mi }
            limits: { memory: 3Gi }
          volumeMounts:
            - { name: script, mountPath: /scripts }
      volumes:
        - name: script
          configMap: { name: loadtest-script }
YAML
	# k6 exits non-zero when one of its own thresholds is crossed, which makes
	# the Job "Failed" rather than "Complete"; waiting only for Complete would
	# sit out the whole timeout. So stop waiting at either outcome.
	deadline=$(($(date +%s) + ramp + hold + 240))
	until [ -n "$(kubectl get "job/$job" -o jsonpath='{.status.succeeded}{.status.failed}')" ] || [ "$(date +%s)" -ge "$deadline" ]; do sleep 5; done
	[ -n "$(kubectl get "job/$job" -o jsonpath='{.status.failed}')" ] && echo "  (level $level crossed one of k6's thresholds; the result is still read)"
	ended=$(date -u +%s)
	kubectl logs "job/$job" > "$results/$stamp-$env_name-$level.log" 2>&1 || true
	sed -n '/===SUMMARY-JSON===/{n;p}' "$results/$stamp-$env_name-$level.log" > "/tmp/$job.json"
	if [ ! -s "/tmp/$job.json" ]; then echo "  level $level: no summary in the log (see $results/$stamp-$env_name-$level.log)"; continue; fi
	meta=$(printf '{"environment":"%s","cluster":"%s","nodes":%s,"node_type":"%s","node_cpu":"%s","node_memory":"%s","gateway_image":"%s","k6_image":"%s","started_utc":"%s","ended_utc":"%s","generator":"k6 Job inside the cluster, talking to the gateway service directly"}' \
		"$env_name" "$ctx" "$nodes" "${node_type:-}" "$node_cpu" "$node_mem_ki" "$gateway_image" "$k6_image" "$(date -u -d @$started +%FT%TZ)" "$(date -u -d @$ended +%FT%TZ)")
	python3 loadtest/collect.py "/tmp/$job.json" http://localhost:19099 "$started" "$ended" "$results/$stamp-$env_name-$level.json" "$meta"
	rm -f "$results/$stamp-$env_name-$level.log"
done
echo; echo "Results: $results/$stamp-$env_name-*.json"
