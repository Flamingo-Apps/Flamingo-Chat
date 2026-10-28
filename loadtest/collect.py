"""Assembles one load-test result: k6's summary plus what the cluster did.

Called by run.sh after each level with the k6 summary JSON, the time window,
and a Prometheus address. Prints the result JSON path and one table row.
Reads only; stdlib only.
"""
import json
import sys
import urllib.parse
import urllib.request

summary_path, prom, start, end, out_path, meta_json = sys.argv[1:7]
summary = json.load(open(summary_path))
meta = json.loads(meta_json)


def q(expr, step=15):
    url = f"{prom}/api/v1/query_range?" + urllib.parse.urlencode({"query": expr, "start": start, "end": end, "step": step})
    with urllib.request.urlopen(url, timeout=60) as r:
        return json.load(r)["data"]["result"]


def peak(expr):
    best = None
    for series in q(expr):
        for _, v in series["values"]:
            v = float(v)
            best = v if best is None or v > best else best
    return best


APP = 'container!="",container!="POD",pod!~"loadtest.*"'
GEN = 'container!="",container!="POD",pod=~"loadtest.*"'
cluster = {
    "app_cpu_cores_peak": peak(f"sum(rate(container_cpu_usage_seconds_total{{{APP}}}[1m]))"),
    "app_memory_gib_peak": (peak(f"sum(container_memory_working_set_bytes{{{APP}}})") or 0) / 2**30,
    "generator_cpu_cores_peak": peak(f"sum(rate(container_cpu_usage_seconds_total{{{GEN}}}[1m]))"),
    "generator_memory_gib_peak": (peak(f"sum(container_memory_working_set_bytes{{{GEN}}})") or 0) / 2**30,
    "busiest_services_cpu_cores_peak": {},
}
for svc in ("gateway", "chat", "delivery-worker", "persistence-worker", "matching", "identity", "presence", "kafka", "postgres", "redis"):
    v = peak(f'sum(rate(container_cpu_usage_seconds_total{{container="{svc}"}}[1m]))')
    if v is not None:
        cluster["busiest_services_cpu_cores_peak"][svc] = round(v, 3)

result = {"meta": meta, "k6": summary, "cluster": cluster}
json.dump(result, open(out_path, "w"), indent=2)

lat = summary.get("msg_latency_ms", {})
users = summary["users"]
secs = users["hold_s"] or 1
f = lambda x, n=0: "-" if x is None else f"{x:.{n}f}"
print(
    f"{users['target']:>6}  {f(lat.get('med'))}  {f(lat.get('p95'))}  {f(lat.get('p99'))}  "
    f"{f((summary['messages'].get('received') or 0) / (users['ramp_s'] + users['hold_s']), 1)}/s  "
    f"{f((summary.get('sessions_matched_rate') or 0) * 100, 1)}%  "
    f"err={sum((summary['errors'].get(k) or 0) for k in ('frame_errors', 'signup_failed', 'ws_connect_failed'))}  "
    f"cpu={f(cluster['app_cpu_cores_peak'], 2)}  mem={f(cluster['app_memory_gib_peak'], 2)}GiB  gen_cpu={f(cluster['generator_cpu_cores_peak'], 2)}"
)
