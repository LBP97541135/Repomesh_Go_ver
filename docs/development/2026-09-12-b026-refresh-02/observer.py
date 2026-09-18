#!/usr/bin/env python3
import argparse
import json
import os
import subprocess
import sys
import time
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path

ACTOR = "16198e14-7249-4cc2-a15e-57cd8b568874"
FIXTURE_ID = "repo_00000000001367444901"
PSQL = "psql"
PG_SOCKET = "/home/xubohan/.local/state/repomesh-b026"
PG_PORT = "55432"
PG_USER = "xubohan"
PG_DATABASE = "repomesh_b026"
WINDOWS_NODE = "/mnt/c/Program Files/nodejs/node.exe"
RUN_DIR_UNC = r"\\wsl.localhost\Ubuntu-22.04\home\xubohan\projects\Repomesh_Go_ver\docs\development\2026-09-12-b026-refresh-02"
POST_REFRESH_UNC = RUN_DIR_UNC + r"\post-refresh.mjs"
RUN_DIR = Path(__file__).resolve().parent
SNAPSHOT_SQL = RUN_DIR / "snapshot.sql"
BASELINE_PATH = RUN_DIR / "baseline.json"
OBSERVATIONS_PATH = RUN_DIR / "observations.jsonl"
OUTCOME_PATH = RUN_DIR / "outcome.json"
CONFIRMED_PATH = RUN_DIR / "confirmed-refresh.json"
STOP_PATH = RUN_DIR / "STOP"


class ControlledFailure(Exception):
    def __init__(self, code):
        super().__init__(code)
        self.code = code


def parse_time(value):
    if not isinstance(value, str):
        raise ControlledFailure("INVALID_TIMESTAMP")
    parsed = None
    error = None
    for timestamp_format in ("%Y-%m-%dT%H:%M:%S.%f%z", "%Y-%m-%dT%H:%M:%S%z"):
        try:
            parsed = datetime.strptime(value, timestamp_format)
            break
        except ValueError as exc:
            error = exc
    if parsed is None:
        raise ControlledFailure("INVALID_TIMESTAMP") from error
    if parsed.tzinfo is None:
        raise ControlledFailure("INVALID_TIMESTAMP")
    return parsed.astimezone(timezone.utc)


def exclusive_json(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(value, stream, separators=(",", ":"), sort_keys=True)
            stream.write("\n")
    except BaseException:
        try:
            os.close(descriptor)
        except OSError:
            pass
        raise


def snapshot():
    command = [
        PSQL, "-X", "-A", "-t", "-q", "-v", "ON_ERROR_STOP=1",
        "-h", PG_SOCKET, "-p", PG_PORT, "-U", PG_USER, "-d", PG_DATABASE,
        "-v", f"actor={ACTOR}", "-f", str(SNAPSHOT_SQL),
    ]
    try:
        completed = subprocess.run(
            command,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=15,
            check=False,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise ControlledFailure("DATABASE_UNAVAILABLE") from exc
    if completed.returncode != 0:
        raise ControlledFailure("DATABASE_UNAVAILABLE")
    lines = [line for line in completed.stdout.splitlines() if line.strip()]
    if len(lines) != 1:
        raise ControlledFailure("INVALID_DATABASE_RESULT")
    try:
        value = json.loads(lines[0])
    except json.JSONDecodeError as exc:
        raise ControlledFailure("INVALID_DATABASE_RESULT") from exc
    validate_snapshot_shape(value)
    return value


def validate_snapshot_shape(value):
    if not isinstance(value, dict):
        raise ControlledFailure("INVALID_DATABASE_RESULT")
    connection = value.get("connection")
    sessions = value.get("sessions")
    attempts = value.get("attempts")
    if not isinstance(connection, dict) or not isinstance(sessions, list) or not isinstance(attempts, list):
        raise ControlledFailure("INVALID_DATABASE_STATE")
    if connection.get("actor") != ACTOR:
        raise ControlledFailure("ACTOR_CHANGED")
    required_connection = {
        "revision", "access_epoch", "status", "refresh_state",
        "credential_committed_at", "observed_at", "access_expires_at",
        "refresh_expires_at", "has_refresh_token", "refresh_due_at",
    }
    if not required_connection.issubset(connection):
        raise ControlledFailure("INVALID_DATABASE_STATE")
    if not isinstance(connection.get("revision"), str) or not isinstance(connection.get("access_epoch"), int):
        raise ControlledFailure("INVALID_DATABASE_STATE")
    for row in sessions:
        if not isinstance(row, dict) or row.get("actor") != ACTOR or not isinstance(row.get("generation"), int):
            raise ControlledFailure("INVALID_DATABASE_STATE")
    for row in attempts:
        if not isinstance(row, dict) or not isinstance(row.get("id"), str):
            raise ControlledFailure("INVALID_DATABASE_STATE")
    parse_time(value.get("sampledAt"))


def active_baseline_session(value):
    sampled_at = parse_time(value["sampledAt"])
    active = [
        row for row in value["sessions"]
        if row.get("revoked") is False
        and parse_time(row.get("expires_at")) > sampled_at
        and parse_time(row.get("last_active_at")) > sampled_at - timedelta(minutes=30)
    ]
    if len(active) != 1:
        raise ControlledFailure("ORIGINAL_SESSION_NOT_UNIQUE")
    return active[0]


def original_session_valid(value, generation):
    sampled_at = parse_time(value["sampledAt"])
    matching = [row for row in value["sessions"] if row.get("generation") == generation]
    return len(matching) == 1 and matching[0].get("revoked") is False \
        and parse_time(matching[0].get("expires_at")) > sampled_at \
        and parse_time(matching[0].get("last_active_at")) > sampled_at - timedelta(minutes=30)


def load_baseline():
    try:
        with BASELINE_PATH.open("r", encoding="utf-8") as stream:
            value = json.load(stream)
    except (OSError, json.JSONDecodeError) as exc:
        raise ControlledFailure("BASELINE_UNAVAILABLE") from exc
    validate_snapshot_shape(value)
    if not isinstance(value.get("baselineAttemptId"), str) or value.get("fixtureId") != FIXTURE_ID:
        raise ControlledFailure("INVALID_BASELINE")
    return value


def prepare(attempt_id):
    try:
        canonical = str(uuid.UUID(attempt_id))
    except ValueError as exc:
        raise ControlledFailure("INVALID_ATTEMPT_ID") from exc
    if canonical != attempt_id.lower():
        raise ControlledFailure("INVALID_ATTEMPT_ID")

    value = snapshot()
    connection = value["connection"]
    matching = [row for row in value["attempts"] if row.get("id") == attempt_id]
    if len(matching) != 1:
        raise ControlledFailure("BASELINE_ATTEMPT_NOT_FOUND")
    attempt = matching[0]
    if attempt.get("purpose") != "reconnect" or attempt.get("state") != "confirmed" \
            or attempt.get("actor") != ACTOR:
        raise ControlledFailure("BASELINE_ATTEMPT_INVALID")
    if connection.get("status") != "connected" or connection.get("refresh_state") != "idle" \
            or attempt.get("connection_revision") != connection.get("revision"):
        raise ControlledFailure("BASELINE_CONNECTION_INVALID")
    if connection.get("has_refresh_token") is not True \
            or connection.get("access_expires_at") is None \
            or connection.get("refresh_expires_at") is None:
        raise ControlledFailure("BASELINE_CREDENTIAL_INVALID")
    if parse_time(connection["access_expires_at"]) <= parse_time(value["sampledAt"]) \
            or parse_time(connection["refresh_expires_at"]) <= parse_time(value["sampledAt"]):
        raise ControlledFailure("BASELINE_CREDENTIAL_EXPIRED")
    if parse_time(connection["refresh_due_at"]) <= parse_time(value["sampledAt"]):
        raise ControlledFailure("BASELINE_REFRESH_ALREADY_DUE")
    active_baseline_session(value)
    value["baselineAttemptId"] = attempt_id
    value["fixtureId"] = FIXTURE_ID
    exclusive_json(BASELINE_PATH, value)


def wait_until(seconds):
    end = time.monotonic() + seconds
    while True:
        remaining = end - time.monotonic()
        if remaining <= 0:
            return True
        if STOP_PATH.exists():
            return False
        time.sleep(min(1, remaining))


def append_observation(stream, value):
    stream.write(json.dumps(value, separators=(",", ":"), sort_keys=True) + "\n")
    stream.flush()
    os.fsync(stream.fileno())


def write_outcome(status, before, after, error_code=None, browser_return_code=None):
    value = {"status": status, "before": before, "after": after}
    if error_code is not None:
        value["errorCode"] = error_code
    if browser_return_code is not None:
        value["browserReturnCode"] = browser_return_code
    exclusive_json(OUTCOME_PATH, value)


def run_post_refresh():
    try:
        completed = subprocess.run(
            [WINDOWS_NODE, POST_REFRESH_UNC, RUN_DIR_UNC],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=300,
            check=False,
        )
        return completed.returncode
    except subprocess.TimeoutExpired:
        return 124
    except OSError:
        return 127


def classify(baseline, current, generation, baseline_attempt_ids):
    connection_before = baseline["connection"]
    connection_now = current["connection"]
    sampled_at = parse_time(current["sampledAt"])
    due_at = parse_time(connection_before["refresh_due_at"])

    current_attempt_ids = {row["id"] for row in current["attempts"]}
    if not current_attempt_ids.issubset(baseline_attempt_ids):
        return "fail", "NEW_ATTEMPT_DETECTED"
    if not original_session_valid(current, generation):
        return "fail", "ORIGINAL_SESSION_INVALID"
    if connection_now.get("status") == "unknown" or connection_now.get("refresh_state") == "unknown":
        return "fail", "UNKNOWN_DATABASE_STATE"

    epoch_delta = connection_now["access_epoch"] - connection_before["access_epoch"]
    revision_changed = connection_now["revision"] != connection_before["revision"]
    if epoch_delta < 0 or epoch_delta > 1:
        return "fail", "INVALID_EPOCH_ADVANCE"
    if (epoch_delta != 0 or revision_changed) and sampled_at < due_at:
        return "fail", "PREMATURE_CONNECTION_CHANGE"
    if epoch_delta == 0 and revision_changed:
        return "fail", "REVISION_WITHOUT_EPOCH"
    if epoch_delta == 1 and not revision_changed:
        return "fail", "EPOCH_WITHOUT_REVISION"

    if epoch_delta == 1:
        committed_now = parse_time(connection_now["credential_committed_at"])
        if committed_now < due_at:
            return "fail", "PREMATURE_CREDENTIAL_COMMIT"
        success = connection_now.get("status") == "connected" \
            and connection_now.get("refresh_state") == "idle" \
            and connection_now.get("has_refresh_token") is True \
            and committed_now > parse_time(connection_before["credential_committed_at"]) \
            and parse_time(connection_now["access_expires_at"]) > parse_time(connection_before["access_expires_at"]) \
            and parse_time(connection_now["refresh_expires_at"]) > parse_time(connection_before["refresh_expires_at"])
        return ("success", None) if success else ("fail", "REFRESH_INVARIANT_FAILED")

    if connection_now.get("revision") != connection_before.get("revision"):
        return "fail", "REVISION_CHANGED"
    if connection_now.get("status") != "connected":
        return "fail", "CONNECTION_NOT_CONNECTED"
    refresh_state = connection_now.get("refresh_state")
    if refresh_state == "claimed" and sampled_at >= due_at:
        return "continue", None
    if refresh_state != "idle":
        return "fail", "UNEXPECTED_REFRESH_STATE"
    return "continue", None


def run():
    baseline = load_baseline()
    before = {key: value for key, value in baseline.items() if key not in {"baselineAttemptId", "fixtureId"}}
    generation = active_baseline_session(baseline)["generation"]
    baseline_attempt_ids = {row["id"] for row in baseline["attempts"]}
    due_at = parse_time(baseline["connection"]["refresh_due_at"])
    deadline = parse_time(baseline["connection"]["access_expires_at"]) + timedelta(minutes=15)
    descriptor = os.open(OBSERVATIONS_PATH, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    latest = before
    outcome_written = False
    try:
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            while True:
                if STOP_PATH.exists():
                    write_outcome("STOPPED", before, latest)
                    outcome_written = True
                    return
                try:
                    current = snapshot()
                except ControlledFailure as exc:
                    write_outcome("FAILED", before, latest, exc.code)
                    outcome_written = True
                    return
                latest = current
                append_observation(stream, current)
                state, error_code = classify(baseline, current, generation, baseline_attempt_ids)
                if state == "fail":
                    write_outcome("FAILED", before, current, error_code)
                    outcome_written = True
                    return
                if state == "success":
                    confirmed = {"phase": "natural_refresh_confirmed", **current}
                    exclusive_json(CONFIRMED_PATH, confirmed)
                    browser_return_code = run_post_refresh()
                    write_outcome("REFRESH_CONFIRMED", before, current, browser_return_code=browser_return_code)
                    outcome_written = True
                    return
                now = datetime.now(timezone.utc)
                if now >= deadline:
                    write_outcome("FAILED", before, current, "REFRESH_DEADLINE_EXCEEDED")
                    outcome_written = True
                    return
                interval = 10 if now >= due_at - timedelta(minutes=2) else 60
                if not wait_until(min(interval, (deadline - now).total_seconds())):
                    write_outcome("STOPPED", before, current)
                    outcome_written = True
                    return
    finally:
        if not outcome_written and not OUTCOME_PATH.exists():
            write_outcome("FAILED", before, latest, "OBSERVER_INTERRUPTED")


def main():
    parser = argparse.ArgumentParser()
    subparsers = parser.add_subparsers(dest="command", required=True)
    prepare_parser = subparsers.add_parser("prepare")
    prepare_parser.add_argument("--attempt-id", required=True)
    subparsers.add_parser("run")
    args = parser.parse_args()
    try:
        if args.command == "prepare":
            prepare(args.attempt_id)
        else:
            run()
    except ControlledFailure as exc:
        sys.stderr.write(exc.code + "\n")
        return 1
    except FileExistsError:
        sys.stderr.write("EVIDENCE_ALREADY_EXISTS\n")
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
