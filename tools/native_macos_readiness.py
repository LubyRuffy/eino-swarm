"""Ordinary native startup evidence; recovery journals are a separate contract.

Only explicit read-only pending observations may be polled. Refusals, unknown
helper outcomes and evidence failures stop the current case without replaying it.
"""
import base64
import json
import os
from pathlib import Path
import struct
import subprocess
import uuid
import zlib


class Failure(RuntimeError):
    def __init__(self, kind, stage, message):
        super().__init__(message)
        self.failure_kind, self.stage = kind, stage


def failure_record(error, stage):
    if isinstance(error, Failure):
        kind, stage = error.failure_kind, error.stage
    elif isinstance(error, subprocess.TimeoutExpired):
        kind = "native_observation_timeout"
    elif isinstance(error, PermissionError):
        kind = "native_permission_refused"
    elif isinstance(error, TimeoutError):
        kind = "native_readiness_deadline"
    else:
        kind = "native_assertion_refused" if stage == "scenario" else "native_readiness_refused"
    return dict(failure_kind=kind, failure_stage=stage, retryable=False)


def save_json(path, row):
    """Keep evidence private without changing the compiler/subprocess umask."""
    path = Path(path)
    temporary = path.with_name(path.name + "." + uuid.uuid4().hex + ".tmp")
    try:
        descriptor = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
        with os.fdopen(descriptor, "w", encoding="utf-8") as stream:
            json.dump(row, stream, indent=2, allow_nan=False)
            stream.write("\n")
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def stream_record(value):
    if isinstance(value, bytes):
        return dict(type="bytes", base64=base64.b64encode(value).decode("ascii"))
    return dict(type="text", text=value)


def window_pending(row, pid):
    """Validate refusal predicates even when the helper reports ready=false."""
    if not isinstance(row, dict) or row.get("pid") != pid or type(row.get("ready")) is not bool:
        raise Failure("native_identity_refused", "window", "native readiness identity or ready state is invalid")
    if row.get("registration_pending") is True:
        if (row["ready"] or row.get("case_verified") is not False or row.get("bundle_verified") is not False
                or type(row.get("window_count")) is not int or row.get("window_count") != 0 or row.get("windows") != []
                or row.get("activation_requested") is True):
            raise Failure("native_identity_refused", "window", "native registration-pending observation is inconsistent")
        return "application_registration_pending"
    if row.get("case_verified") is not True:
        raise Failure("native_identity_refused", "window", "native case identity was not verified")
    if row.get("screen_capture_allowed") is not True:
        raise Failure("native_permission_refused", "window", "native screen capture permission is unavailable")
    count, windows = row.get("window_count"), row.get("windows")
    if (type(count) is not int or count not in (0, 1) or not isinstance(windows, list) or len(windows) > 1
            or any(not isinstance(window, dict) or window.get("owner_pid") != pid
                   or type(window.get("id")) is not int or window["id"] <= 0 for window in windows)
            or (windows and (row.get("window_id") != windows[0]["id"] or row.get("window_owner_pid") != pid))
            or any(type(row.get(key, 0)) is not int or row.get(key, 0) > 1 or row.get(key, 0) < 0
                   for key in ("textareas", "send_count"))):
        raise Failure("native_window_refused", "window", "native window lacks a unique case-owned window")
    if count == 0:
        if row["ready"]:
            raise Failure("native_window_refused", "window", "native ready observation has no AX window")
        return "application_window_pending"
    return None if row["ready"] else "application_content_pending"


def pixel_observation(path):
    """Decode ordinary screenshot scanlines before checking nonblank content.

    The caller validates PNG chunks and dimensions first. These statistics are
    observation evidence, never proof of a product recovery or a cold start.
    """
    raw, offset, header, data = Path(path).read_bytes(), 8, None, bytearray()
    while offset + 12 <= len(raw):
        length = struct.unpack(">I", raw[offset:offset+4])[0]
        kind, payload = raw[offset+4:offset+8], raw[offset+8:offset+8+length]
        if kind == b"IHDR":
            header = struct.unpack(">IIBBBBB", payload)
        if kind == b"IDAT":
            data.extend(payload)
        offset += length+12
    width, height, depth, color, _, _, _ = header
    channels = {0: 1, 2: 3, 4: 2, 6: 4}.get(color)
    if depth not in (8, 16) or not channels:
        raise Failure("native_pixels_invalid", "pixels", "native screenshot content encoding is unsupported")
    bpp, stride = channels * (depth // 8), width * channels * (depth // 8)
    decoded, previous, buckets = zlib.decompress(data), bytearray(stride), {}
    for y in range(height):
        start = y * (stride+1)
        mode, scan = decoded[start], bytearray(decoded[start+1:start+1+stride])
        for x in range(stride):
            left, up = scan[x-bpp] if x >= bpp else 0, previous[x]
            upper_left = previous[x-bpp] if x >= bpp else 0
            if mode == 1:
                value = left
            elif mode == 2:
                value = up
            elif mode == 3:
                value = (left+up)//2
            elif mode == 4:
                estimate = left+up-upper_left
                distances = [abs(estimate-value) for value in (left, up, upper_left)]
                value = (left, up, upper_left)[distances.index(min(distances))]
            else:
                value = 0
            scan[x] = (scan[x]+value) & 255
        # Bound histogram work while including the whole image, not one corner.
        for x in range(0, width, max(1, width//256)):
            values = [scan[x*bpp + channel*(depth//8)] >> 4 for channel in range(1 if color in (0, 4) else 3)]
            key = tuple(values)
            buckets[key] = buckets.get(key, 0)+1
        previous = scan
    samples = sum(buckets.values())
    dominant = max(buckets.values()) / samples
    return dict(dimensions=[width, height], sampled_pixels=samples, buckets=len(buckets),
                dominant_fraction=dominant, content_visible=len(buckets) >= 16 and dominant <= .985)
