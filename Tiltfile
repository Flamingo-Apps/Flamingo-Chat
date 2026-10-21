# Local development for Flamingo Chat.
#
# `tilt up` builds every service's image, deploys Postgres, Redis and
# Kafka, creates the Kafka topics, deploys every service through the
# shared Helm chart, and forwards each one's port to localhost - see
# docs/ARCHITECTURE.md §9 (DEC-0039).
#
# This intentionally targets a local kind cluster only. The real
# deployment target is Google Cloud (docs/ARCHITECTURE.md §12), reached
# through its own pipeline, not through this file.
allow_k8s_contexts('kind-flamingo-dev')

# ---------------------------------------------------------------------------
# Dependencies: Postgres, Redis, Kafka, and the one-shot topic-creation Job.
# ---------------------------------------------------------------------------

k8s_yaml([
    'deploy/k8s/dev/postgres.yaml',
    'deploy/k8s/dev/redis.yaml',
    'deploy/k8s/dev/kafka.yaml',
    'deploy/k8s/dev/kafka-topics.yaml',
])

k8s_resource('postgres', port_forwards='5432:5432', labels=['dependencies'])
k8s_resource('redis', port_forwards='6379:6379', labels=['dependencies'])
k8s_resource('kafka', port_forwards='9092:9092', labels=['dependencies'])

# A Job is considered ready by Tilt once it completes, not once a pod is
# "Ready" in the usual sense - no special configuration needed for that,
# just the dependency order so it doesn't start racing Kafka's own
# startup (it also retries internally, so this is a courtesy, not the
# only thing making it correct).
k8s_resource('kafka-create-topics', resource_deps=['kafka'], labels=['dependencies'])

# ---------------------------------------------------------------------------
# Observability: Prometheus + Grafana (metrics), Loki + Alloy (logs) -
# dev-only, see DEC-0095 and deploy/observability/. Plain Helm releases
# via local_resource, not Tilt's own helm() function - that one expects a
# local chart directory (like deploy/helm/service below), not a
# repo-hosted chart such as these. kube-prometheus-stack is what
# services below actually depend on: it installs the PodMonitor CRD
# their own Helm release needs to exist first.
#
# Their UIs get links through the grafana/prometheus/alloy-ui resources
# further down: Tilt can't port-forward Services a local_resource's shell
# command created (it only tracks objects it applied via k8s_yaml), so
# those run `kubectl port-forward` themselves.
# ---------------------------------------------------------------------------

# A failed repo refresh (offline, slow network) is only a warning:
# ensure-release.sh needs the network only when a chart version isn't in
# Helm's local cache yet.
local_resource(
    'observability-repos',
    cmd='helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1; ' +
        'helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1; ' +
        '(helm repo update prometheus-community grafana >/dev/null 2>&1 && echo "chart repos refreshed") ' +
        '|| echo "warning: could not refresh chart repos - using Helm\'s cached index"',
    labels=['observability'],
)

# Each release is (re)installed only when its chart version or values file
# changed since the last install - otherwise it's a few seconds of "nothing
# to do" (DEC-0097). deps makes an edit to a values file re-apply it while
# tilt up is running.
def helm_release(release, chart, version, values, deps=[]):
    local_resource(
        release,
        cmd='deploy/observability/ensure-release.sh %s %s %s %s' % (release, chart, version, values),
        deps=[values, 'deploy/observability/ensure-release.sh'],
        resource_deps=['observability-repos'] + deps,
        labels=['observability'],
    )

helm_release('kube-prometheus-stack', 'prometheus-community/kube-prometheus-stack', '92.10.1',
             'deploy/observability/kube-prometheus-stack-values.yaml')
helm_release('loki', 'grafana/loki', '7.3.0', 'deploy/observability/loki-values.yaml')
helm_release('alloy', 'grafana/alloy', '1.14.0', 'deploy/observability/alloy-values.yaml', deps=['loki'])

# A plain `kubectl port-forward` dies whenever its pod restarts and never
# comes back, so each one runs in a retry loop.
def port_forward(name, svc, ports, links, deps, preamble=''):
    local_resource(
        name,
        serve_cmd=preamble + 'while true; do kubectl port-forward svc/%s %s; sleep 2; done' % (svc, ports),
        links=links,
        resource_deps=deps,
        labels=['observability'],
    )

port_forward(
    'grafana', 'kube-prometheus-stack-grafana', '3000:80',
    [link('http://localhost:3000/d/flamingo-overview', 'Flamingo dashboard'),
     link('http://localhost:3000/explore', 'Explore metrics and logs')],
    ['kube-prometheus-stack'],
    # The chart generates the admin password into a Secret; printed here
    # so it shows in this resource's Tilt log instead of living in the repo.
    preamble="echo \"Grafana login: admin / $(kubectl get secret kube-prometheus-stack-grafana -o jsonpath='{.data.admin-password}' | base64 -d)\"; ",
)
port_forward(
    'prometheus', 'kube-prometheus-stack-prometheus', '9090:9090',
    [link('http://localhost:9090/targets', 'Prometheus scrape targets')],
    ['kube-prometheus-stack'],
)
# No resource_deps on 'alloy': the retry loop already waits for the Service,
# and depending on it would hide this UI whenever that Helm step is red.
port_forward(
    'alloy-ui', 'alloy', '12345:12345',
    [link('http://localhost:12345', 'Alloy pipeline graph')],
    [],
)

# Dashboards as code: every JSON file in deploy/observability/dashboards/
# becomes one ConfigMap, which Grafana's sidecar loads automatically because
# of the grafana_dashboard=1 label (it watches every namespace for it).
watch_file('deploy/observability/dashboards')
k8s_yaml(local(
    'kubectl create configmap grafana-dashboards-flamingo --from-file=deploy/observability/dashboards/ ' +
    '--dry-run=client -o yaml | kubectl label --local -f - grafana_dashboard=1 -o yaml',
    quiet=True,
))
k8s_resource(
    new_name='grafana-dashboards',
    objects=['grafana-dashboards-flamingo:configmap'],
    resource_deps=['kube-prometheus-stack'],
    labels=['observability'],
)

# ---------------------------------------------------------------------------
# The Go services, deployed through deploy/helm/service.
# ---------------------------------------------------------------------------

# Every Dockerfile builds from the repository root, not its own service
# directory, because go.work ties every module together: a service's
# go.work lists every other service too, so all of them have to be
# present in the build context for workspace mode to resolve at all (see
# services/identity/Dockerfile). That rules out docker_build's `only`
# parameter here - it would trim exactly the directories go.work needs.
# The cost is that Tilt watches the whole repo for every service's image,
# not just that service's own directory; Go's own build cache (mounted in
# the Dockerfile) keeps a rebuild fast regardless.
services = [
    {'name': 'gateway', 'port': 8080, 'deps': []},
    {'name': 'identity', 'port': 50051, 'deps': ['postgres']},
    {'name': 'chat', 'port': 50053, 'deps': ['postgres', 'redis', 'kafka']},
    {'name': 'matching', 'port': 50052, 'deps': ['redis', 'kafka']},
    {'name': 'presence', 'port': 50054, 'deps': ['redis']},
    {'name': 'moderation', 'port': 50055, 'deps': ['postgres', 'kafka', 'redis']},
    # 'port': None marks a worker service (protocol: worker in its own
    # values file) - nothing to port-forward, since it has no server of
    # its own (DEC-0066).
    {'name': 'delivery-worker', 'port': None, 'deps': ['redis', 'kafka']},
    {'name': 'persistence-worker', 'port': None, 'deps': ['postgres', 'kafka']},
]

for svc in services:
    name = svc['name']
    docker_build(
        'flamingo/' + name,
        '.',
        dockerfile='services/%s/Dockerfile' % name,
    )
    k8s_yaml(helm(
        'deploy/helm/service',
        name=name,
        values=['deploy/helm/service/values/%s.yaml' % name],
        # Needs the PodMonitor CRD kube-prometheus-stack installs -
        # unconditional in every other environment (deploy/helm/service's
        # own default is off, see its values.yaml) since only the dev
        # Tiltfile path has that CRD installed at all.
        set=['podMonitor.enabled=true'],
    ))
    k8s_resource(
        name,
        port_forwards='%d:%d' % (svc['port'], svc['port']) if svc['port'] else [],
        resource_deps=svc['deps'] + ['kube-prometheus-stack'],
        # Without this Tilt files every PodMonitor under "uncategorized",
        # since a PodMonitor has no pod of its own to group it by.
        objects=['%s:podmonitor' % name],
        labels=['services'],
    )

# ---------------------------------------------------------------------------
# The web frontend: not containerized, just the Vite dev server running
# directly on the host (DEC-0040 chose a plain local process over a dev
# container for this, to keep edit/reload fast). It needs the gateway's
# port already forwarded to localhost:8080, hence resource_deps.
# ---------------------------------------------------------------------------

local_resource(
    'web',
    cmd='npm install',
    serve_cmd='npm run dev',
    dir='web',
    serve_dir='web',
    deps=['web/package.json'],
    resource_deps=['gateway'],
    links=[link('http://localhost:8000', 'Flamingo web app')],
    labels=['web'],
)
