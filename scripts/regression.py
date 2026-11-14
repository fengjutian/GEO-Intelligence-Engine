#!/usr/bin/env python3
"""Core P0 regression: project -> questions -> protocol -> audit -> judge -> metrics -> report.

Prerequisites:
  - API and MySQL are running (python scripts/dev.py start).
  - A model worker is running with a reachable provider; for local regression use
    .run/fake-openai plus `go run ./cmd/model-worker` on GEO_MODEL_WORKER_ADDRESS=:8085.

Usage:
  python scripts/regression.py --base-url http://127.0.0.1:8080 --timeout 180
"""

from __future__ import annotations

import argparse
import json
import sys
import time
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timezone
from pathlib import Path


class RegressionError(RuntimeError):
    pass


def request(base_url: str, method: str, path: str, body: dict | None = None, headers: dict | None = None):
    payload = None if body is None else json.dumps(body).encode("utf-8")
    all_headers = {"accept": "application/json"}
    if body is not None:
        all_headers["content-type"] = "application/json"
    if headers:
        all_headers.update(headers)
    req = urllib.request.Request(base_url.rstrip("/") + path, data=payload, method=method, headers=all_headers)
    try:
        with urllib.request.urlopen(req, timeout=30) as response:
            raw = response.read()
            content_type = response.headers.get("content-type", "")
            if "application/json" in content_type:
                return response.status, json.loads(raw.decode("utf-8"))
            return response.status, raw
    except urllib.error.HTTPError as error:
        detail = error.read().decode("utf-8", errors="replace")
        raise RegressionError(f"{method} {path} -> {error.code}: {detail}") from error


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--base-url", default="http://127.0.0.1:8080")
    parser.add_argument("--timeout", type=int, default=180)
    parser.add_argument("--provider", help="only use an active engine config for this provider")
    parser.add_argument("--model", help="model selector for the protocol (defaults per provider)")
    parser.add_argument("--evidence", default=".run/regression-verification.json")
    args = parser.parse_args()

    started = datetime.now(timezone.utc)
    stamp = started.strftime("%Y%m%d%H%M%S")
    summary: dict = {"baseUrl": args.base_url, "startedAt": started.isoformat()}

    _, version = request(args.base_url, "GET", "/api/v1/version")
    summary["apiVersion"] = version.get("apiVersion", "unknown")

    _, configs = request(args.base_url, "GET", "/api/v1/engine-configs")
    engine = next((
        item for item in configs.get("items", [])
        if item.get("status") == "active" and (not args.provider or item.get("provider") == args.provider)
    ), None)
    if not engine:
        raise RegressionError("no active engine config; seed one before running regression")
    model = args.model or ("MiniMax-M2.5" if engine.get("provider") == "minimax" else "deepseek-flash")
    summary["engineConfigId"] = engine["id"]
    summary["provider"] = engine.get("provider")
    summary["model"] = model

    _, project = request(args.base_url, "POST", "/api/v1/projects", {
        "name": f"Regression {stamp}",
        "brandName": "Regression Brand",
        "officialUrl": "https://example.com",
        "locale": "zh-CN",
        "region": "CN",
        "timezone": "Asia/Shanghai",
    })
    summary["projectId"] = project["id"]

    questions = ["GEO 分析工具推荐哪些？", "如何提升品牌在 AI 回答中的可见度？"]
    for text in questions:
        request(args.base_url, "POST", f"/api/v1/projects/{project['id']}/questions", {
            "text": text, "language": "zh-CN", "questionType": "comparison", "weight": 1,
        })

    _, sets = request(args.base_url, "GET", f"/api/v1/projects/{project['id']}/question-sets")
    question_set = sets["items"][0]
    _, published_set = request(args.base_url, "POST", f"/api/v1/question-sets/{question_set['id']}/versions")
    summary["questionVersionId"] = published_set["id"]
    summary["questionCount"] = published_set["questionCount"]

    _, protocol = request(args.base_url, "POST", f"/api/v1/projects/{project['id']}/measurement-protocols", {
        "questionVersionId": published_set["id"],
        "engines": [{
            "engineConfigId": engine["id"],
            "modelSelector": model,
            "channel": engine.get("channel") or "model_api",
            "parameters": {},
        }],
        "region": "CN",
        "language": "zh-CN",
        "samplesPerQuestion": 1,
        "budgetMicrounits": 100000,
        "currency": "USD",
        "judgeVersion": "judge-rules-v1.0",
        "metricVersion": "metrics-v1",
        "promptTemplateVersion": "audit-prompt-v1",
        "estimatedCostPerSample": 1000,
    })
    _, protocol = request(args.base_url, "POST", f"/api/v1/measurement-protocols/{protocol['id']}:publish")
    summary["protocolId"] = protocol["id"]

    _, audit = request(args.base_url, "POST", "/api/v1/audits", {
        "projectId": project["id"], "protocolId": protocol["id"],
    }, {"Idempotency-Key": f"regression-{uuid.uuid4()}"})
    summary["auditId"] = audit["id"]

    deadline = time.time() + args.timeout
    status = audit.get("status")
    while time.time() < deadline:
        _, audit = request(args.base_url, "GET", f"/api/v1/audits/{audit['id']}")
        status = audit.get("status")
        if status in {"succeeded", "partial_success", "failed", "cancelled"}:
            break
        time.sleep(2)
    if status not in {"succeeded", "partial_success"}:
        raise RegressionError(f"audit finished with status {status}")
    summary["auditStatus"] = status
    summary["plannedCount"] = audit.get("plannedCount")
    summary["completedCount"] = audit.get("completedCount")
    summary["eligibleCount"] = audit.get("eligibleCount")

    _, metrics = request(args.base_url, "GET", f"/api/v1/audits/{audit['id']}/metrics")
    values = metrics if isinstance(metrics, list) else metrics.get("items", [])
    if not values:
        raise RegressionError("audit produced no metric values")
    summary["metricValues"] = len(values)
    summary["metricCodes"] = sorted({item.get("metricCode") for item in values if item.get("metricCode")})

    _, report = request(args.base_url, "POST", f"/api/v1/audits/{audit['id']}/reports", {"metricSnapshotId": "", "templateVersion": "report-v1"})
    summary["reportId"] = report["id"]
    summary["reportSections"] = len(report.get("sections", []))
    if summary["reportSections"] < 6:
        raise RegressionError("report is missing required sections")

    _, artifact = request(args.base_url, "POST", f"/api/v1/reports/{report['id']}/exports", {"format": "csv"})
    status_code, content = request(args.base_url, "GET", artifact["downloadUrl"])
    if status_code != 200 or not isinstance(content, bytes) or len(content) < 100:
        raise RegressionError("CSV artifact is empty or unavailable")
    summary["csvBytes"] = len(content)
    summary["finishedAt"] = datetime.now(timezone.utc).isoformat()
    summary["status"] = "passed"

    evidence = Path(args.evidence)
    evidence.parent.mkdir(parents=True, exist_ok=True)
    evidence.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except RegressionError as error:
        print(f"REGRESSION FAILED: {error}", file=sys.stderr)
        sys.exit(1)
