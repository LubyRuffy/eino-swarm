"""Retained actual HTTP inputs for one private native acceptance case."""
from email.message import Message
import hashlib
import json
import os
from pathlib import Path
import re
import struct
import sys
import urllib.parse
import urllib.request
import zlib


def _component(value):
    if not isinstance(value, str) or not re.fullmatch(r"[A-Za-z0-9_-]{1,128}", value):
        raise RuntimeError("input evidence label or identity is not a safe path component")
    return value


def _path(root, relative):
    relative = Path(relative)
    if relative.is_absolute() or not relative.parts or any(part in (".", "..") for part in relative.parts):
        raise RuntimeError("input body evidence path escapes its case")
    path = root
    for part in relative.parts:
        path = path / part
        if path.is_symlink():
            raise RuntimeError("input body evidence path is a symbolic link")
    return path


def _mime(value):
    if not isinstance(value, str) or not value.strip():
        raise RuntimeError("input HTTP Content-Type is missing")
    message = Message()
    message["Content-Type"] = value
    return message.get_content_type()


def image_dimensions(data, fixture):
    # This QA accepts one known decodable PNG, not arbitrary image formats.
    # Exact actual-body equality permits decoding its simple, unfiltered IDAT.
    if data != fixture:
        raise RuntimeError("input image body differs from the decodable PNG fixture")
    width, height = struct.unpack(">II", data[16:24])
    pixels = zlib.decompress(data[41:-16])
    stride = 1 + width * 4
    if (width, height) != (3, 2) or len(pixels) != height * stride or any(pixels[offset] for offset in range(0, len(pixels), stride)):
        raise RuntimeError("input PNG image scanlines or dimensions differ")
    return dict(format="png", width=width, height=height)


def download(context, path, mime=None, capture=None):
    request = urllib.request.Request(context["origin"] + path)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=3) as response:
        raw_type = response.headers.get("Content-Type") if mime or capture is not None else None
        if capture is not None:
            capture.update(request_path=path, request_url=request.full_url,
                response_url=response.url, status=response.status, content_type=raw_type,
                no_sniff=response.headers.get("X-Content-Type-Options"))
        if response.url != request.full_url or response.headers.get("X-Content-Type-Options") != "nosniff":
            raise RuntimeError("input download identity differs")
        data = response.read()
        if capture is not None:
            capture["data"] = data
        if mime and _mime(raw_type) != mime:
            raise RuntimeError("input HTTP MIME differs from image metadata")
        if capture is not None:
            capture["http_mime"] = _mime(raw_type)
        return data


def _exclusive(path, data):
    with path.open("xb") as stream:
        os.chmod(path, 0o600)
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())


def retain(context, label, snapshot, captures, fixture, primary=None, draft=None, reuse=False):
    label, case = _component(label), _component(context["case_id"])
    source = Path(context["evidence"])
    if source.is_symlink():
        raise RuntimeError("input evidence case directory is a symbolic link")
    root = source.resolve(strict=True)
    directory = _path(root, "http-inputs/" + label)
    records, bodies, failure = [], [], primary
    for index, capture in enumerate(captures):
        if "data" not in capture:
            continue
        data = capture["data"]
        record = {key: value for key, value in capture.items() if key != "data"}
        relative = "http-inputs/" + label + "/" + str(index) + ".body"
        record.update(body=relative, size=len(data), sha256=hashlib.sha256(data).hexdigest())
        if record["metadata"]["kind"] == "image":
            try:
                record["image"] = image_dimensions(data, fixture)
            except (RuntimeError, ValueError, zlib.error, struct.error) as error:
                failure = failure or error
                record["image_error"] = type(error).__name__
        records.append(record)
        bodies.append((relative, data))
    row = dict(schema=1, case_id=case, label=label, thread_id=context["thread_id"], origin=context["origin"],
               status="failed" if failure is not None else "complete", snapshot=snapshot, files=records)
    if draft is not None:
        row.update(draft_key=draft.get("key"), draft_revision=draft.get("revision"))
    if reuse and directory.exists():
        if json.loads(_path(root, "http-inputs/" + label + "/receipt.json").read_text()) != row or any(
                _path(root, name).read_bytes() != data for name, data in bodies):
            raise RuntimeError("input evidence from an earlier observation differs; overwrite refused")
    else:
        directory.parent.mkdir(mode=0o700, exist_ok=True)
        directory.mkdir(mode=0o700)
        for name, data in bodies:
            _exclusive(_path(root, name), data)
        _exclusive(directory / "receipt.json", (json.dumps(row, ensure_ascii=False, indent=2) + "\n").encode())
    if primary is None and failure is not None:
        raise failure


def _finish(context, label, snapshot, captures, fixture, primary, **kwargs):
    if label is None:
        return
    try:
        retain(context, label, snapshot, captures, fixture, primary, **kwargs)
    except BaseException as error:
        if primary is None:
            raise
        try:
            print("WARN input_evidence_write_failed " + json.dumps(dict(error_type=type(error).__name__)), file=sys.stderr)
        except (OSError, ValueError):
            pass


def draft_snapshot(context, harness, label=None):
    if label is not None:
        _component(label)
    key = urllib.parse.quote("thread:" + context["thread_id"], safe="")
    path = "/api/composer/drafts/" + key
    draft, snapshot, captures, primary = None, None, [], None
    try:
        draft = harness.api(context["origin"], path)["draft"]
        harness.fault_phases.observe_restore_api(context, draft)
        files = []
        for item in draft.get("files", []):
            file_path = path + "/files/" + item["id"]
            if label is None:
                # Default restore observation keeps the original download and
                # error order; strict evidence capture is explicitly requested.
                request = urllib.request.Request(context["origin"] + file_path)
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                with opener.open(request, timeout=3) as response:
                    if response.url != request.full_url or response.headers.get("X-Content-Type-Options") != "nosniff":
                        raise RuntimeError("draft byte download identity differs")
                    data = response.read()
            else:
                capture = dict(metadata=dict(item))
                captures.append(capture)
                data = harness.download_input(context, file_path,
                    item["mime"] if item["kind"] == "image" else None, capture=capture)
            if len(data) != item["size"]:
                raise RuntimeError("draft attachment was truncated")
            files.append(dict(item, sha256=hashlib.sha256(data).hexdigest()))
        snapshot = dict(text=draft["text"], quotes=draft.get("quotes", []), files=files, state=draft["state"])
        return snapshot
    except BaseException as error:
        primary = error
        raise
    finally:
        if label is not None:
            _finish(context, label, snapshot, captures, harness.fixture_png(), primary, draft=draft)


def validate_sent_input(context, text, draft, row, trace, harness):
    prefix = "/api/threads/" + urllib.parse.quote(row["thread_id"], safe="")
    events = [event for event in trace.get("events", []) if event.get("kind") == "user_message"]
    if len(events) != 1:
        raise RuntimeError("sent input has no unique actual user-message event")
    event = events[0]
    listing = harness.api(context["origin"], prefix + "/files")["files"]
    paths, evidence, captures, primary, snapshot = [], [], [], None, None
    label = "sent-" + _component(row["id"])
    images = list(event.get("images", []))
    try:
        for item in draft["files"]:
            capture = dict(metadata=dict(item))
            captures.append(capture)
            if item["kind"] == "file":
                matches = [entry for entry in listing if entry.get("uploaded") and not entry.get("dir")
                    and entry.get("name") == item["name"] and entry.get("size") == item["size"]]
                if len(matches) != 1:
                    raise RuntimeError("sent attachment has no unique uploaded file")
                path = matches[0]["path"]
                paths.append(path)
                url = prefix + "/download/" + urllib.parse.quote(path, safe="/")
                data = harness.download_input(context, url, capture=capture)
            else:
                if not images:
                    raise RuntimeError("sent image is absent from the actual visual input")
                image = images.pop(0)
                if image.get("mime") != item["mime"]:
                    raise RuntimeError("sent image MIME differs")
                path = image["id"]
                data = harness.download_input(context, prefix + "/input-images/" + urllib.parse.quote(path, safe=""),
                                               item["mime"], capture=capture)
                image_dimensions(data, harness.fixture_png())
            if len(data) != item["size"] or hashlib.sha256(data).hexdigest() != item["sha256"]:
                raise RuntimeError("sent attachment bytes differ from the exact saved draft")
            evidence.append(dict(item, input_path=path))
        expected = text.strip()
        if paths:
            expected += "\n\nAttached files:\n" + "\n".join("- " + path for path in paths)
        if images or row.get("user_text") != expected or event.get("text") != expected:
            raise RuntimeError("sent input differs from the exact hydrated attachment notice")
        snapshot = dict(text=expected, files=evidence)
    except BaseException as error:
        primary = error
        raise
    finally:
        _finish(context, label, snapshot, captures, harness.fixture_png(), primary, reuse=True)
    destination = Path(context["evidence"]) / ("sent-input-" + row["id"] + ".json")
    if destination.exists() or destination.is_symlink():
        if destination.is_symlink() or json.loads(destination.read_text()) != snapshot:
            raise RuntimeError("sent input evidence already exists; overwrite refused")
    else:
        _exclusive(destination, (json.dumps(snapshot, ensure_ascii=False, indent=2) + "\n").encode())


def _metadata(item):
    return {key: value for key, value in item.items() if key not in ("sha256", "input_path")}


def _read_receipt(root, label, result, snapshot, fixture):
    receipt = json.loads(_path(root, "http-inputs/" + label + "/receipt.json").read_text())
    if (receipt.get("schema") != 1 or receipt.get("case_id") != result["case_id"] or receipt.get("label") != label
            or receipt.get("status") != "complete" or receipt.get("snapshot") != snapshot
            or len(receipt["files"]) != len(snapshot["files"]) or not receipt.get("thread_id")):
        raise RuntimeError("input evidence receipt belongs to another case or snapshot")
    bodies = []
    thread = urllib.parse.quote(receipt["thread_id"], safe="")
    for index, (record, item) in enumerate(zip(receipt["files"], snapshot["files"])):
        expected_path = "http-inputs/" + label + "/" + str(index) + ".body"
        if record["body"] != expected_path or _metadata(record["metadata"]) != _metadata(item):
            raise RuntimeError("input body evidence identity or path differs")
        data = _path(root, record["body"]).read_bytes()
        mime = _mime(record.get("content_type"))
        request_path = ("/api/composer/drafts/" + urllib.parse.quote("thread:" + receipt["thread_id"], safe="")
            + "/files/" + urllib.parse.quote(item["id"], safe="")) if label in ("before", "after") else (
            "/api/threads/" + thread + ("/download/" + urllib.parse.quote(item["input_path"], safe="/")
            if item["kind"] == "file" else "/input-images/" + urllib.parse.quote(item["input_path"], safe="")))
        if (len(data) != record["size"] or len(data) != item["size"] or hashlib.sha256(data).hexdigest() != record["sha256"]
                or record["sha256"] != item["sha256"] or record.get("http_mime") != mime
                or record.get("no_sniff") != "nosniff" or record.get("status") != 200
                or record.get("request_path") != request_path
                or record.get("request_url") != receipt["origin"] + record["request_path"]
                or record.get("response_url") != record["request_url"]):
            raise RuntimeError("input body evidence bytes or HTTP response differs")
        if item["kind"] == "image" and (mime != item["mime"] or record.get("image") != image_dimensions(data, fixture)):
            raise RuntimeError("input image evidence HTTP MIME or decoded dimensions differ")
        bodies.append(data)
    return receipt, bodies


def accept_stage(directory, result, fixture):
    try:
        if Path(directory).is_symlink():
            raise RuntimeError("input evidence case is a symbolic link")
        root = Path(directory).resolve(strict=True)
        before, after = (json.loads(_path(root, "draft-" + label + ".json").read_text()) for label in ("before", "after"))
        if before != after or {item["kind"] for item in before["files"]} != {"file", "image"} or before["state"] != "editing":
            raise RuntimeError("input evidence lost the original unsent snapshot")
        pre, pre_bytes = _read_receipt(root, "before", result, before, fixture)
        post, post_bytes = _read_receipt(root, "after", result, after, fixture)
        if (pre_bytes != post_bytes or pre["origin"] != post["origin"] or pre["thread_id"] != post["thread_id"]
                or pre["draft_key"] != "thread:" + pre["thread_id"] or post["draft_key"] != pre["draft_key"]
                or type(pre["draft_revision"]) is not int or pre["draft_revision"] <= 0
                or type(post["draft_revision"]) is not int
                or post["draft_revision"] != pre["draft_revision"]
                or [item["content_type"] for item in pre["files"]] != [item["content_type"] for item in post["files"]]):
            raise RuntimeError("input evidence changed draft scope, revision or original HTTP body")
        sent_ids = [_component(turn) for turn in result["turn_ids"] if (root / ("sent-input-" + _component(turn) + ".json")).exists()]
        if len(sent_ids) != 1:
            raise RuntimeError("input evidence has no unique actual sent input")
        sent = json.loads(_path(root, "sent-input-" + sent_ids[0] + ".json").read_text())
        receipt, sent_bytes = _read_receipt(root, "sent-" + sent_ids[0], result, sent, fixture)
        expected_text = before["text"].strip() + "\n\nAttached files:\n" + "\n".join(
            "- " + item["input_path"] for item in sent["files"] if item["kind"] == "file")
        if (sent_bytes != pre_bytes or receipt["thread_id"] != pre["thread_id"] or receipt["origin"] != pre["origin"]
                or sent["text"] != expected_text
                or [item["content_type"] for item in receipt["files"]] != [item["content_type"] for item in pre["files"]]
                or [_metadata(item) for item in sent["files"]] != [_metadata(item) for item in before["files"]]):
            raise RuntimeError("input evidence sent bytes or metadata differ from the saved draft")
        return True
    except (OSError, KeyError, ValueError, TypeError, struct.error, zlib.error) as error:
        raise RuntimeError("input body evidence is missing or malformed") from error
