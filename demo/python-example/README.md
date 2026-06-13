# XACT Python REST Ingest Example

This directory contains a small, dependency-free Python 3 example for writing ingest data to XACT with the REST API.

The script writes one device with two tags:

```text
default.python.example.sensors.temperature_c
default.python.example.sensors.humidity_pct
```

The code is intentionally split into two phases:

1. Provision the device and tags by sending tag metadata.
2. Periodically write value-only updates.

**Note:** Provisioning is optional, but convenient. It defines the tag processing pipeline such as limit checks, historic recording etc. These can be defined manually with the Tag Manager.

The HTTP transport is isolated in `http_post_json()` so it can be replaced with a target-specific function for constrained runtimes such as MicroPython-style deployments.

## Configure the Example

Edit the constants at the top of [ingest_example.py](ingest_example.py):

```python
XACT_SERVER_URL = "https://127.0.0.1:8443/xact"
XACT_API_KEY = "replace-with-your-xact-api-key"
TENANT = "default"
DEVICE_TYPE = "python"
DEVICE_NAME = "example"
```

`TENANT`, `DEVICE_TYPE`, and `DEVICE_NAME` form the device path:

```text
{TENANT}.{DEVICE_TYPE}.{DEVICE_NAME}
```

For the defaults, the device path is `default.python.example`.

## Get an API Key

The REST ingest endpoint requires:

```text
Authorization: ApiKey <key>
```

Create an API key for the same organisation used in `TENANT`:

1. Log in to the XACT UI.
2. Open the Organisations Manager.
3. Select the organisation, for example `default`.
4. Create a new API key, for example named `Python REST Example`.
5. Copy the generated key into `XACT_API_KEY`.

The raw key is only shown when it is created, so store it somewhere appropriate for your deployment.

## Run It

```sh
python3 ingest_example.py
```

The first request provisions the tags. Later requests send only values.

## Adapting for Production

Replace `build_value_payload()` with reads from your real data source. Keep the JSON shape the same if you want the tags to remain under `sensors.temperature_c` and `sensors.humidity_pct`.

Change `build_provision_payload()` to define your tag names, units, deadbands, limits, history, persistence, publish, and stale-check behavior. Re-sending provisioning is safe because XACT updates existing tag metadata and processing block settings.

For constrained targets, replace only `http_post_json()`. It must POST the JSON payload to the provided URL with the provided headers and raise `HttpPostError` for network or HTTP failures. The retry loop, reconnect detection, and re-provision behavior can stay unchanged.

For multiple devices, create one set of `TENANT`, `DEVICE_TYPE`, and `DEVICE_NAME` values per device, or refactor `build_ingest_url()` to accept device identity as parameters.

## Checking if it works

If the console shows the logs below then data is written to the XACT server. Use the Tags Manager widget to view the values under the 'python' top level branch.

```
$ python3 ingest_example.py 
  Provisioning default.python.example
  Provisioning complete
  Wrote values: {"sensors": {"humidity_pct": 57.6, "temperature_c": 22.42}}
      :
```