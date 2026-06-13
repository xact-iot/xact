#!/usr/bin/env python3
"""
XACT REST ingest example.

This example writes data for one device with two tags:

    default.python.example.sensors.temperature_c
    default.python.example.sensors.humidity_pct

The code is split into two clear phases:

1. Provision the device and tags by sending metadata-rich tag objects.
2. Periodically write value-only updates to those tags.

The network call is isolated in http_post_json(). On constrained targets such as
MicroPython or Arduino-style Python runtimes, replace only that function with the
target platform's HTTP client and keep the provisioning/retry logic unchanged.
"""

import json
import math
import random
import time


# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
# Keep these values near the top for constrained deployments where environment
# variables or command-line parsing may not be available.

XACT_SERVER_URL = "http://127.0.0.1:8080/xact"

# Create this key in the XACT UI/API for the "default" organisation.
# The README explains one way to create it with curl.
XACT_API_KEY = "replace-with-your-api-key"

# The REST ingest route builds this device path:
#   {TENANT}.{DEVICE_TYPE}.{DEVICE_NAME}
# which is: default.python.example
TENANT = "default"
DEVICE_TYPE = "python"
DEVICE_NAME = "example"

# How often to send value-only updates after provisioning.
WRITE_PERIOD_SECONDS = 5.0

# Retry settings. Keep MAX_RETRIES bounded so a production loop can keep making
# progress, log failures, and try again on the next cycle.
MAX_RETRIES = 5
INITIAL_RETRY_DELAY_SECONDS = 1.0
MAX_RETRY_DELAY_SECONDS = 30.0
REQUEST_TIMEOUT_SECONDS = 10.0


# ---------------------------------------------------------------------------
# Replaceable HTTP transport
# ---------------------------------------------------------------------------

class HttpPostError(Exception):
    """Raised when the POST failed or returned a non-2xx status."""

    def __init__(self, message, status_code=None):
        super().__init__(message)
        self.status_code = status_code


def http_post_json(
    url,
    headers,
    payload,
    timeout_seconds,
):
    """
    POST a JSON object and return (status_code, response_text).

    This CPython implementation uses only the standard library. For constrained
    environments, replace this function with code using the platform's network
    stack, for example MicroPython's urequests.post(...).
    """
    from urllib import request, error

    body = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    request_headers = dict(headers)
    request_headers["Content-Type"] = "application/json"

    req = request.Request(
        url=url,
        data=body,
        headers=request_headers,
        method="POST",
    )

    try:
        with request.urlopen(req, timeout=timeout_seconds) as resp:
            status_code = resp.getcode()
            response_text = resp.read(4096).decode("utf-8", "replace")
    except error.HTTPError as exc:
        response_text = exc.read(4096).decode("utf-8", "replace")
        raise HttpPostError(
            "HTTP %s from XACT: %s" % (exc.code, response_text.strip()),
            status_code=exc.code,
        )
    except OSError as exc:
        # Covers DNS failures, refused connections, timeouts, broken sockets,
        # and TLS/socket errors from the standard library transport.
        raise HttpPostError("network error posting to XACT: %s" % exc)

    if status_code < 200 or status_code >= 300:
        raise HttpPostError(
            "HTTP %s from XACT: %s" % (status_code, response_text.strip()),
            status_code=status_code,
        )

    return status_code, response_text


# ---------------------------------------------------------------------------
# XACT payloads
# ---------------------------------------------------------------------------

def build_ingest_url():
    """Return /api/v1/ingest/{tenant}/{devicetype}/{devicename}."""
    base_url = XACT_SERVER_URL.rstrip("/")
    return "%s/api/v1/ingest/%s/%s/%s" % (
        base_url,
        TENANT,
        DEVICE_TYPE,
        DEVICE_NAME,
    )


def build_provision_payload():
    """
    Build a metadata-rich payload used to provision the device and tags.

    XACT auto-provisions missing nodes from ingest payloads. Sending tag objects
    with a "value" plus fields such as "units", "description", "history",
    "persist", "publish", and "stalecheck" creates or updates those tag
    settings. Re-sending this payload is safe; existing tags are updated.
    """
    return {
        "sensors": {
            "temperature_c": {
                "value": 21.0,
                "units": "C",
                "description": "Example Python temperature sensor",
                "deadband": 0.1,
                "history": True,
                "persist": True,
                "publish": True,
                "stalecheck": 60,
                "limits": {
                    "hi": 80.0,
                    "lo": -20.0,
                },
            },
            "humidity_pct": {
                "value": 45.0,
                "units": "%",
                "description": "Example Python humidity sensor",
                "deadband": 0.5,
                "history": True,
                "persist": True,
                "publish": True,
                "stalecheck": 60,
                "limits": {
                    "hi": 95.0,
                    "lo": 5.0,
                },
            },
        }
    }


def build_value_payload(sequence):
    """
    Build the steady-state value-only payload.

    In production this function is where you would read real sensors, PLC
    registers, serial input, files, or another local data source.
    """
    wave = math.sin(sequence / 8.0)
    temperature = 22.0 + (wave * 3.0) + random.uniform(-0.15, 0.15)
    humidity = 50.0 + (math.cos(sequence / 10.0) * 8.0) + random.uniform(-0.5, 0.5)

    return {
        "sensors": {
            "temperature_c": round(temperature, 2),
            "humidity_pct": round(humidity, 2),
        }
    }


# ---------------------------------------------------------------------------
# Retry and reconnect behavior
# ---------------------------------------------------------------------------

def is_auth_error(exc):
    """Authentication errors require operator action, not blind retries."""
    return exc.status_code in (401, 403)


def is_reconnect_error(exc):
    """
    Return True when a successful future POST may represent a reconnected client.

    Any network failure has status_code None. HTTP 503 can also be transient when
    the ingest queue is busy during restart or overload.
    """
    return exc.status_code is None or exc.status_code == 503


def post_with_retries(payload, purpose):
    """
    POST payload with bounded exponential backoff.

    Returns True if a transient connectivity issue happened before the eventual
    success. The main loop uses that signal to re-provision after reconnect.
    """
    url = build_ingest_url()
    headers = {"Authorization": "ApiKey " + XACT_API_KEY}
    delay = INITIAL_RETRY_DELAY_SECONDS
    saw_reconnect_error = False

    for attempt in range(1, MAX_RETRIES + 1):
        try:
            http_post_json(url, headers, payload, REQUEST_TIMEOUT_SECONDS)
            if attempt > 1:
                print("%s succeeded after %d attempts" % (purpose, attempt))
            return saw_reconnect_error
        except HttpPostError as exc:
            if is_auth_error(exc):
                raise RuntimeError(
                    "%s failed: API key is missing, invalid, or belongs to "
                    "another organisation: %s" % (purpose, exc)
                )

            if is_reconnect_error(exc):
                saw_reconnect_error = True

            if attempt == MAX_RETRIES:
                raise RuntimeError(
                    "%s failed after %d attempts: %s"
                    % (purpose, MAX_RETRIES, exc)
                )

            print(
                "%s attempt %d/%d failed: %s; retrying in %.1fs"
                % (purpose, attempt, MAX_RETRIES, exc, delay)
            )
            time.sleep(delay)
            delay = min(delay * 2.0, MAX_RETRY_DELAY_SECONDS)

    return saw_reconnect_error


def provision_device_and_tags():
    """Phase 1: provision the device and its two tags."""
    print("Provisioning %s.%s.%s" % (TENANT, DEVICE_TYPE, DEVICE_NAME))
    post_with_retries(build_provision_payload(), "provision")
    print("Provisioning complete")


def write_values_forever():
    """
    Phase 2: periodically write values.

    If the client loses network connectivity and later reconnects, it sends the
    provisioning payload again before continuing value writes. This helps after
    server rebuilds, organisation resets, deleted tags, or a device reconnecting
    to a different XACT instance.
    """
    sequence = 0
    needs_reprovision = False
    while True:
        try:
            if needs_reprovision:
                print("Previous write cycle failed; re-provisioning before writing")
                provision_device_and_tags()
                needs_reprovision = False

            sequence += 1
            payload = build_value_payload(sequence)

            reconnected = post_with_retries(payload, "write values")
            if reconnected:
                print("Connection recovered; re-provisioning tags and re-sending values")
                provision_device_and_tags()
                post_with_retries(payload, "write values after reconnect")

            print("Wrote values: %s" % json.dumps(payload, sort_keys=True))
            time.sleep(WRITE_PERIOD_SECONDS)
        except RuntimeError as exc:
            # In production, send this to your target platform's logging system.
            # The loop keeps running so temporary outages do not stop telemetry.
            print("Write cycle failed: %s" % exc)
            needs_reprovision = True
            time.sleep(WRITE_PERIOD_SECONDS)


def main():
    """Run the two ingest phases."""
    provision_device_and_tags()
    write_values_forever()


if __name__ == "__main__":
    main()
