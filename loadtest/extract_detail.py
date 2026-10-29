"""Pulls the detailed measurements for each load-test window out of Prometheus.

Reads only. Prometheus keeps 15 days, so this has to run before 2026-11-11 or
the data is gone. Output: loadtest/results/20261028-gke-detail.json
"""
import glob
import json
import sys
import urllib.parse
import urllib.request
from datetime import datetime, timedelta, timezone

PROM = "http://localhost:19099"
RESULTS = "/home/aro/Flamingo/Flamingo-Chat/loadtest/results"
TEST_SECONDS = 200  # 30 s ramp + 120 s hold + 15 s ramp-down + grace, from the start of each Job


def ts(s):
    return datetime.strptime(s, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)


def rng(expr, start, end, step=15):
    url = f"{PROM}/api/v1/query_range?" + urllib.parse.urlencode(
        {"query": expr, "start": start.timestamp(), "end": end.timestamp(), "step": step}
    )
    with urllib.request.urlopen(url, timeout=60) as r:
        return json.load(r)["data"]["result"]


def inst(expr, at):
    url = f"{PROM}/api/v1/query?" + urllib.parse.urlencode({"query": expr, "time": at.timestamp()})
    with urllib.request.urlopen(url, timeout=60) as r:
        return json.load(r)["data"]["result"]


def peak_avg(series):
    vals = [float(v) for s in series for _, v in s["values"] if v != "NaN"]
    return (max(vals), sum(vals) / len(vals)) if vals else (None, None)


def by(series, label):
    out = {}
    for s in series:
        k = s["metric"].get(label, "")
        vals = [float(v) for _, v in s["values"] if v != "NaN"]
        if vals:
            out[k] = (max(vals), sum(vals) / len(vals))
    return out


def r(x, n=3):
    return None if x is None else round(x, n)


CONTAINERS = ["gateway", "chat", "matching", "identity", "presence", "moderation", "delivery-worker",
              "persistence-worker", "kafka", "postgres", "redis", "prometheus", "grafana", "loki", "alloy", "node-exporter"]


def collect(meta):
    start = ts(meta["started_utc"])
    end = min(ts(meta["ended_utc"]), start + timedelta(seconds=TEST_SECONDS))
    secs = int((end - start).total_seconds())
    out = {"window_utc": [meta["started_utc"], end.strftime("%Y-%m-%dT%H:%M:%SZ")]}

    # CPU time on the whole node, by what the CPU was doing, in cores (2 available).
    modes = by(rng('sum by (mode) (rate(node_cpu_seconds_total[1m]))', start, end), "mode")
    busy = rng('sum(rate(node_cpu_seconds_total{mode!="idle"}[1m]))', start, end)
    b_peak, b_avg = peak_avg(busy)
    out["node_cpu_cores"] = {
        "busy_peak": r(b_peak), "busy_avg": r(b_avg),
        "by_mode_peak": {m: r(v[0]) for m, v in modes.items() if m != "idle"},
        "by_mode_avg": {m: r(v[1]) for m, v in modes.items() if m != "idle"},
    }
    # Kernel pressure stall: share of time that some task was waiting for the resource.
    for name, metric in (("cpu", "node_pressure_cpu_waiting_seconds_total"), ("io", "node_pressure_io_waiting_seconds_total"),
                         ("memory", "node_pressure_memory_waiting_seconds_total")):
        p, a = peak_avg(rng(f"rate({metric}[1m])", start, end))
        out.setdefault("pressure_waiting_share", {})[name] = {"peak": r(p), "avg": r(a)}
    p, _ = peak_avg(rng("(node_memory_MemTotal_bytes - node_memory_MemAvailable_bytes) / 2^30", start, end))
    out["node_memory_used_gib_peak"] = r(p, 2)

    # Disks: which device is which is recorded in the environment section.
    disks = {}
    for key, expr in (("write_mib_s", 'sum by (device) (rate(node_disk_written_bytes_total{device=~"sd.*|nvme.*"}[1m])) / 2^20'),
                      ("read_mib_s", 'sum by (device) (rate(node_disk_read_bytes_total{device=~"sd.*|nvme.*"}[1m])) / 2^20'),
                      ("busy_share", 'sum by (device) (rate(node_disk_io_time_seconds_total{device=~"sd.*|nvme.*"}[1m]))')):
        for dev, (pk, _a) in by(rng(expr, start, end), "device").items():
            disks.setdefault(dev, {})[key + "_peak"] = r(pk)
    out["disks_peak"] = disks

    # Per container: CPU cores and memory (working set), against what it is allowed.
    cpu = by(rng('sum by (container) (rate(container_cpu_usage_seconds_total{container!="",container!="POD",pod!~"loadtest.*"}[1m]))', start, end), "container")
    mem = by(rng('max by (container) (container_memory_working_set_bytes{container!="",container!="POD",pod!~"loadtest.*"}) / 2^20', start, end), "container")
    out["containers"] = {c: {"cpu_cores_peak": r(cpu.get(c, (None,))[0]), "memory_mib_peak": r(mem.get(c, (None,))[0], 1)} for c in CONTAINERS if c in cpu or c in mem}
    gcpu = by(rng('sum by (container) (rate(container_cpu_usage_seconds_total{container="k6"}[1m]))', start, end), "container")
    gmem = by(rng('max by (container) (container_memory_working_set_bytes{container="k6"}) / 2^20', start, end), "container")
    out["load_generator_k6"] = {"cpu_cores_peak": r(gcpu.get("k6", (None,))[0]), "memory_mib_peak": r(gmem.get("k6", (None,))[0], 1)}

    # The services themselves.
    p, _ = peak_avg(rng('max(kafka_consumer_lag)', start, end))
    out["kafka_consumer_lag_peak_messages"] = r(p, 0)
    p, _ = peak_avg(rng('max(go_goroutines{container="gateway"})', start, end))
    out["gateway_goroutines_peak"] = r(p, 0)
    p, _ = peak_avg(rng('sum(rate(container_network_receive_bytes_total{pod=~"gateway.*"}[1m])) / 1024', start, end))
    q, _ = peak_avg(rng('sum(rate(container_network_transmit_bytes_total{pod=~"gateway.*"}[1m])) / 1024', start, end))
    out["gateway_pod_network_kib_s_peak"] = {"receive": r(p, 1), "transmit": r(q, 1)}

    def total(expr):
        res = inst(expr, end)
        return round(sum(float(s["value"][1]) for s in res)) if res else None

    out["requests_in_window"] = {
        "guest_signups": total(f'sum(increase(http_server_request_duration_seconds_count{{route="POST /v1/auth/guest"}}[{secs}s]))'),
        "chat_SendMessage_calls": total(f'sum(increase(grpc_server_request_duration_seconds_count{{container="chat",method="/chat.v1.ChatService/SendMessage"}}[{secs}s]))'),
        "matching_RequestMatch_calls": total(f'sum(increase(grpc_server_request_duration_seconds_count{{container="matching",method="/matching.v1.MatchingService/RequestMatch"}}[{secs}s]))'),
    }
    return out


def main():
    levels = []
    for f in sorted(glob.glob(f"{RESULTS}/*Z-gke-*.json")):
        d = json.load(open(f))
        name = f.split("/")[-1]
        entry = {"result_file": name, "target_users": d["k6"]["users"]["target"]}
        entry.update(collect(d["meta"]))
        levels.append(entry)
        print("collected", name, file=sys.stderr)
    json.dump(levels, open(f"{RESULTS}/20261028-gke-detail.json", "w"), indent=2)
    print("written")


main()
