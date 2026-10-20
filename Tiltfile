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
# Dashboards aren't port-forwarded here automatically (Tilt only tracks
# k8s objects it applied itself via k8s_yaml, not ones a local_resource's
# shell command created) - forward them manually when wanted:
#   kubectl port-forward svc/kube-prometheus-stack-grafana 3000:80
# ---------------------------------------------------------------------------

local_resource(
    'observability-repos',
    cmd='helm repo add prometheus-community https://prometheus-community.github.io/helm-charts >/dev/null 2>&1; ' +
        'helm repo add grafana https://grafana.github.io/helm-charts >/dev/null 2>&1; ' +
        'helm repo update prometheus-community grafana >/dev/null',
    labels=['observability'],
)

local_resource(
    'kube-prometheus-stack',
    cmd='helm upgrade --install kube-prometheus-stack prometheus-community/kube-prometheus-stack ' +
        '--version 92.10.1 -f deploy/observability/kube-prometheus-stack-values.yaml --wait --timeout 8m',
    resource_deps=['observability-repos'],
    labels=['observability'],
)

local_resource(
    'loki',
    cmd='helm upgrade --install loki grafana/loki --version 7.3.0 ' +
        '-f deploy/observability/loki-values.yaml --wait --timeout 8m',
    resource_deps=['observability-repos'],
    labels=['observability'],
)

local_resource(
    'alloy',
    cmd='helm upgrade --install alloy grafana/alloy --version 1.14.0 ' +
        '-f deploy/observability/alloy-values.yaml --wait --timeout 8m',
    resource_deps=['observability-repos', 'loki'],
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
    labels=['web'],
)
