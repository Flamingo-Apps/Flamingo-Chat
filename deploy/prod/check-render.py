"""Refuses a production render that still carries dev settings (DEC-0099).

Reads the rendered service manifests on stdin (helm template output) and
exits non-zero, listing every problem, if any of these slipped through:
DEV_FAKE_GOOGLE present at all (any value but "1" is off since DEC-0099,
but production must not carry it), a secret in a plain env var instead of
a Secret, an env var that is present but empty (it would override the same
name coming from a Secret), the CHANGE-ME placeholder, or an image that
isn't a GHCR image with a real tag.
"""
import sys

import yaml

SECRET_KEYS = ("SESSION_SIGNING_KEY", "GOOGLE_CLIENT_SECRET", "TURNSTILE_SECRET", "POSTGRES_URL")


def containers(doc):
    kind = doc.get("kind")
    if kind == "Deployment":
        return doc["spec"]["template"]["spec"]["containers"]
    if kind == "CronJob":
        return doc["spec"]["jobTemplate"]["spec"]["template"]["spec"]["containers"]
    return []


def main():
    problems = []
    for doc in yaml.safe_load_all(sys.stdin):
        if not doc:
            continue
        for c in containers(doc):
            name = f"{doc['kind']}/{doc['metadata']['name']}"
            env = {e["name"]: e.get("value") for e in c.get("env", [])}
            for key, value in env.items():
                if value in (None, ""):
                    problems.append(f"{name}: {key} is present but empty")
                if "CHANGE-ME" in str(value):
                    problems.append(f"{name}: {key} still contains the CHANGE-ME placeholder")
            if "DEV_FAKE_GOOGLE" in env:
                problems.append(f"{name}: DEV_FAKE_GOOGLE must not be set in production")
            for key in SECRET_KEYS:
                if key in env:
                    problems.append(f"{name}: {key} must come from a Secret, not a plain env var")
            image = c["image"]
            if not image.startswith("ghcr.io/flamingo-apps/") or image.endswith((":dev", ":latest")):
                problems.append(f"{name}: unexpected image {image}")
    if problems:
        print("Refusing to deploy - the production render has problems:", file=sys.stderr)
        for p in problems:
            print(f"  - {p}", file=sys.stderr)
        sys.exit(1)
    print("Production render check passed.")


if __name__ == "__main__":
    main()
