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
    {'name': 'moderation', 'port': 50055, 'deps': ['postgres', 'kafka']},
    # persistence-worker is deliberately not deployed here yet - it has no
    # consume loop until docs/ARCHITECTURE.md's Chat/worker PRs land, so
    # today it would just log one line and exit, which Kubernetes would
    # restart in a pointless loop. It joins this list in that PR.
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
    ))
    k8s_resource(
        name,
        port_forwards='%d:%d' % (svc['port'], svc['port']),
        resource_deps=svc['deps'],
        labels=['services'],
    )
