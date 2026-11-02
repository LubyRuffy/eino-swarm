"""Model-catalog snapshots and immutable identities for opt-in paired evals.

Resources are configured by the human. Tasks and roles are left to the model;
an executor choice is a prompt preference, never a role-to-model routing rule.
"""

from __future__ import annotations

import hashlib
import json
from pathlib import Path

import agent_eval_grade as grade


def parse_ref(ref: str) -> dict:
    parts = ref.split("\t")
    if len(parts) != 2 or not all(parts) or any(p != p.strip() for p in parts):
        raise ValueError("CLI choice must be the exact provider<TAB>model ChoiceID")
    return {"provider_id": parts[0], "model": parts[1], "ref": ref}


def prepare_models(config_text: str, provider_id: str, model: str = "", *,
                   planner_ref: str = "", executor_ref: str = "") -> dict:
    providers = [p for p in grade.extract_providers(config_text)
                 if p.get("base_url") and p.get("model") and p.get("enabled", "true").lower() != "false"]
    catalog = []
    for provider in providers:
        names = list(dict.fromkeys([provider["model"], *provider.get("catalog", [])]))
        provider["catalog"] = names
        catalog.extend(parse_ref(provider["id"] + "\t" + name) for name in names)
    choices = {c["ref"]: c for c in catalog}
    if not planner_ref:
        selected = grade.select_model(grade.extract_provider(config_text, provider_id), model)
        planner_ref = selected["id"] + "\t" + selected["model"]
    planner = parse_ref(planner_ref)
    executor = parse_ref(executor_ref) if executor_ref else None
    for choice in (planner, executor):
        if choice and choice["ref"] not in choices:
            raise ValueError("selected model is not in the enabled configured catalog")
    return {"providers": providers, "catalog": catalog, "planner": planner, "executor": executor,
            "catalog_source": "configured_snapshot"}


def model_summary(models: dict) -> dict:
    return {**{k: models[k] for k in ("planner", "executor", "catalog", "catalog_source")},
            "catalog_ref_kind": "ui_cli_choice_id_not_agent_tool_ref",
            "provider_settings": [{k: p[k] for k in ("id", "api_mode", "timeout_seconds", "first_byte_timeout_seconds",
                                                       "context_window", "model_context") if k in p}
                                  for p in models["providers"]]}


def render_model_config(models: dict, cap: int = 0) -> str:
    q = json.dumps
    lines = ["models:", "    default: " + q(models["planner"]["provider_id"]), "    providers:"]
    for provider in models["providers"]:
        grade.require_copyable_provider(provider)
        lines.append("        - id: " + q(provider["id"]))
        for key in ("label", "base_url", "api_key", "api_mode"):
            if key in provider:
                lines.append(f"          {key}: {q(provider[key])}")
        model = models["planner"]["model"] if provider["id"] == models["planner"]["provider_id"] else provider["model"]
        lines.append("          model: " + q(model))
        for key in ("timeout_seconds", "first_byte_timeout_seconds", "context_window"):
            if str(provider.get(key, "")).isdigit():
                lines.append(f"          {key}: {int(provider[key])}")
        lines.append("          catalog:")
        lines.extend("            - " + q(name) for name in provider["catalog"])
        contexts = {name: size for name, size in provider.get("model_context", {}).items() if isinstance(size, int)}
        if contexts:
            lines.append("          model_context:")
            lines.extend(f"            {q(name)}: {size}" for name, size in contexts.items())
    if cap:
        lines.extend(["swarm:", f"    max_completion_tokens: {cap}"])
    return "\n".join(lines) + "\n"


def project_prompt(task: dict, models: dict | None) -> str:
    prompt = task.get("project_prompt", "")
    if models and models.get("executor"):
        identity = json.dumps({key: models["executor"][key] for key in ("provider_id", "model")}, ensure_ascii=False)
        prompt += ("\n\nFor this evaluation, prefer the configured model identity " + identity + " for delegated execution when useful. "
                   "First call list_models and copy its exact returned ref for that provider_id and model into model_ref. "
                   "Do not construct, decode or escape a tool ref from a model name. "
                   "You may choose the roles, division of work, worker count and other available models from evidence. "
                   "Do not delegate only to satisfy a quota. Report actual limitations.")
    return prompt.strip()


def digest(value) -> str:
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()


def file_digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def task_fingerprint(task: dict) -> str:
    return digest({"files": grade.hash_tree(task["dir"]), "prompt": task["prompt"],
                   "timeout_s": task["timeout_s"], "project_prompt": task.get("project_prompt", "")})


def budget(args, task: dict) -> dict:
    return {"timeout_s": task["timeout_s"], "max_completion_tokens": getattr(args, "max_completion_tokens", 0),
            "review_wait_s": args.review_wait}


def private_write(path: Path, text: str) -> None:
    """Evidence is private without changing compiler/test subprocess umasks."""
    import os
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    try:
        os.fchmod(fd, 0o600)
        with os.fdopen(fd, "w") as output:
            output.write(text)
    except BaseException:
        try:
            os.close(fd)
        except OSError:
            pass
        raise
