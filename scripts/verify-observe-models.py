#!/usr/bin/env python3
"""Explicit, bounded live acceptance of the local workbench's model adapters.

Creates synthetic HTTP trials and frozen samples; makes at most five model calls.
Keys stay in the running server. Does not start product agents or cloud resources.
"""
import argparse
import json
from pathlib import Path
import urllib.request
import urllib.parse


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--url", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--allow-paid-model-calls", action="store_true")
    args = parser.parse_args()
    parsed = urllib.parse.urlparse(args.url)
    if parsed.scheme not in ("http", "https") or parsed.hostname not in ("127.0.0.1", "localhost", "::1") or parsed.username or parsed.query or parsed.fragment:
        parser.error("use an explicit credential-free loopback workbench URL")
    if not args.allow_paid_model_calls:
        parser.error("this acceptance makes up to five paid API calls; pass --allow-paid-model-calls explicitly")
    output = Path(args.output)
    output.mkdir(mode=0o700, parents=True, exist_ok=False)

    def save(name, value):
        path = output / name
        path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n")
        path.chmod(0o600)

    def api(path, body=None):
        request = urllib.request.Request(args.url.rstrip("/") + path,
            data=json.dumps(body).encode() if body is not None else None,
            headers={"Content-Type": "application/json", "X-RepoMesh-Local": "1"})
        with urllib.request.urlopen(request, timeout=100) as response:
            return json.load(response)

    settings = api("/api/settings")
    assistant = api("/api/assistant")
    assert settings["provider"] == "typesafe" and settings["model_configured"]
    assert assistant["configured"] and not assistant["cloud_workspace_required"]
    created = api("/api/trials", {"variant": "suite"})
    extra = api("/api/trials", {"variant": "baseline"})
    archive_id = created["archive"]
    ids = [t["trial_id"] for t in created["trials"] + extra["trials"]]
    catalog = api("/api/catalog")
    archive = next(a for a in catalog["archives"] if a["id"] == archive_id)
    rows = [t for t in archive["trials"] if t["trial_id"] in ids]
    grades = []
    for trial in created["trials"]:
        row = next(t for t in rows if t["trial_id"] == trial["trial_id"])
        payload = {"archive": archive_id, "trial_id": row["trial_id"], "expected_revision": row["evidence_refs"][1]}
        grade = api("/api/judge", payload)
        save("jev-" + row["variant_id"] + ".json", grade)
        assert grade["status"] == "complete", grade["status"]
        assert grade["provider"] == "typesafe" and grade["claim_id"]
        repeat = api("/api/judge", payload)
        assert repeat["id"] == grade["id"], "replay produced another paid grading result"
        grades.append({"variant": row["variant_id"], "business_verdict": row["verdict"],
            "advisory_verdict": grade["verdict"], "model": grade["model"], "usage": grade["usage"],
            "dimensions": grade["dimensions"]})
    samples = []
    for row in rows:
        sample = api("/api/samples", {"archive": archive_id, "trial_id": row["trial_id"],
            "expected_revision": row["evidence_refs"][1], "reason": "verification_failed" if row["verdict"] == "fail" else "representative_sample"})
        samples.append(sample)
    baseline = next(s for s in samples if s["trial_id"] == next(t["trial_id"] for t in rows if t["variant_id"] == "baseline"))
    analysis = api("/api/analyses/attribution", {"sample_id": baseline["id"], "expected_revision": baseline["subject_revision"]})
    save("deepseek-attribution.json", analysis)
    assert analysis["status"] == "complete"
    replay = api("/api/analyses/attribution", {"sample_id": baseline["id"], "expected_revision": baseline["subject_revision"]})
    assert replay["id"] == analysis["id"]
    clusters = api("/api/analyses/clustering", {"sample_ids": [s["id"] for s in samples]})
    save("deepseek-clustering.json", clusters)
    assert clusters["status"] == "complete"
    after = api("/api/catalog")
    current = next(a for a in after["archives"] if a["id"] == archive_id)
    assert {t["trial_id"]: t["verdict"] for t in current["trials"] if t["trial_id"] in ids} == {t["trial_id"]: t["verdict"] for t in rows}
    annotations = api("/api/samples")["annotations"]
    assert all(not a["confirmed"] for a in annotations if a["id"] == analysis["id"])
    summary = {"integration_passed": True, "scope": "synthetic_fixed_product; no real Agent capability claim",
        "new_model_calls": 5, "jev": grades, "deepseek_model": analysis["model"],
        "attribution": analysis["result"], "cluster_membership": clusters["result"],
        "human_confirmation_performed": False, "business_verdicts_unchanged": True}
    save("summary.json", summary)
    print(json.dumps({"integration_passed": True, "new_model_calls": 5, "real_agent_trials": 0}))


if __name__ == "__main__":
    main()
