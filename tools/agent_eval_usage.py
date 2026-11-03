"""Inspectable per-model usage without treating missing provider data as zero.

Older Trace rows flatten absent and explicit-zero usage into the same integer.
Their positive values remain evidence; their zeros are unknown. When a Trace
supplies raw_usage (or usage_presence), explicit zero and absence stay distinct.
No prompt, output body, endpoint or credential is copied into these observations.
"""

from __future__ import annotations

import json
from collections import defaultdict
from typing import Any

FIELDS = ("prompt_tokens", "completion_tokens", "total_tokens", "cached_tokens", "reasoning_tokens")
AUX_AGENT_IDS = ("title-namer", "memory-reviewer", "schedule-answer-policy", "thread-wake-policy",
                 "session-memory", "compact-summarizer", "composer-prediction")


def _raw(call: dict) -> dict | None:
    value = call.get("raw_usage", call.get("usage"))
    if isinstance(value, str):
        try:
            value = json.loads(value)
        except (ValueError, TypeError):
            return None
    return value if isinstance(value, dict) else None


def _value(raw: dict, field: str) -> Any:
    aliases = {"prompt_tokens": "input_tokens", "completion_tokens": "output_tokens"}
    if field in raw:
        return raw[field]
    if field in aliases and aliases[field] in raw:
        return raw[aliases[field]]
    details = {
        "cached_tokens": ("prompt_tokens_details", "input_tokens_details"),
        "reasoning_tokens": ("completion_tokens_details", "output_tokens_details"),
    }
    for key in details.get(field, ()):
        if isinstance(raw.get(key), dict) and field in raw[key]:
            return raw[key][field]
    return None


def field_fact(call: dict, field: str) -> dict:
    raw = _raw(call)
    presence = call.get("usage_presence")
    if raw is not None:
        value, source = _value(raw, field), "raw_usage"
        if value is None:
            return {"state": "absent", "value": None, "source": source}
    elif isinstance(presence, dict) and field in presence:
        source = "usage_presence"
        if presence[field] is not True:
            return {"state": "absent", "value": None, "source": source}
        value = call.get(field)
    else:
        value, source = call.get(field), "legacy_trace"
        if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
            return {"state": "unknown", "value": None, "source": source}
    if not isinstance(value, int) or isinstance(value, bool) or value < 0:
        return {"state": "unknown", "value": None, "source": source}
    return {"state": "zero" if value == 0 else "value", "value": value, "source": source}


def _totals(observations: list[dict]) -> dict:
    out = {"calls": len(observations)}
    for field in FIELDS:
        facts = [o[field] for o in observations]
        known = [f["value"] for f in facts if f["state"] in ("zero", "value")]
        complete = bool(facts) and len(known) == len(facts)
        out[field] = sum(known) if complete else None
        out["known_" + field] = sum(known)
        out[field + "_states"] = {s: sum(f["state"] == s for f in facts)
                                   for s in ("absent", "unknown", "zero", "value")}
    return out


def extract(calls: list[dict]) -> dict:
    observations = []
    grouped = defaultdict(list)
    for call in calls:
        row = {k: call.get(k) for k in ("id", "turn_id", "agent_id", "provider_id", "model", "duration_ms")}
        row.update({field: field_fact(call, field) for field in FIELDS})
        raw = _raw(call)
        if raw is not None:
            # Store only token measurements, never arbitrary extension values.
            row["raw_usage"] = {field: _value(raw, field) for field in FIELDS if _value(raw, field) is not None}
        observations.append(row)
        grouped[(call.get("provider_id") or "", call.get("model") or "")].append(row)
    return {**_totals(observations), "calls": observations,
            "by_model": [{"provider_id": key[0], "model": key[1], **_totals(rows)}
                         for key, rows in sorted(grouped.items())]}


def routes(calls: list[dict], planner: dict | None, executor: dict | None) -> dict:
    """A preferred model is observed only from real call identities, not prose."""
    def matches(call: dict, choice: dict | None) -> bool:
        return bool(choice and call.get("provider_id") == choice["provider_id"]
                    and call.get("model") == choice["model"])
    managers = [c for c in calls if c.get("agent_id") == "manager"]
    workers = [c for c in calls if c.get("agent_id") not in (None, "", "manager", *AUX_AGENT_IDS)]
    return {"manager_calls": len(managers), "worker_calls": len(workers),
            "planner_matches": sum(matches(c, planner) for c in managers),
            "executor_matches": sum(matches(c, executor) for c in workers),
            "executor_preference_realized": bool(executor and workers and any(matches(c, executor) for c in workers)),
            "worker_models": sorted({(c.get("provider_id", ""), c.get("model", "")) for c in workers})}
