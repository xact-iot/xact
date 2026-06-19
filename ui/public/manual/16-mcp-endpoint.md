# Embedded MCP Endpoint

XACT can expose an embedded Model Context Protocol (MCP) endpoint for AI agents and developer tools. The endpoint lets an authenticated XACT user browse real-time tags, inspect processing block schemas, query history, generate driver context, and optionally create or update selected XACT resources.

The MCP server runs inside the normal XACT HTTP server. It is disabled by default and uses the same user session tokens and UI permission model as the web application.

## When to Use MCP

Use the embedded MCP endpoint when you want an AI client to:

- Inspect the current RTDB tag tree.
- Find tags by name, description, type, or history settings.
- Read current tag values and metadata.
- Query historical metric series.
- Generate context for REST, MQTT, or NATS ingest drivers.
- Validate device provisioning plans before applying them.
- Manage reports, schedules, or tag calculations when explicitly enabled.

MCP is not a replacement for device ingest. Devices should still use REST ingest API keys, MQTT, or NATS as described in [Data Ingest](#data-ingest).

## Enabling the Endpoint

Set the MCP environment variables in the XACT `.env` file and restart the server:

```sh
MCP_ENABLED=yes
MCP_ROUTE=/api/v1/mcp
MCP_WRITE_TOOLS_ENABLED=no
MCP_TOOL_TIMEOUT_SECONDS=30
MCP_MAX_PAYLOAD_BYTES=1048576
```

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_ENABLED` | `false` | Enables the embedded MCP route when set to `yes`, `true`, `1`, or `on`. |
| `MCP_ROUTE` | `/api/v1/mcp` | Route inside the XACT application. Do not include the external proxy prefix such as `/xact`. |
| `MCP_WRITE_TOOLS_ENABLED` | `false` | Enables tools that can mutate XACT state. Permission checks still apply. |
| `MCP_TOOL_TIMEOUT_SECONDS` | `30` | Per-request timeout for JSON-RPC tool handling. |
| `MCP_MAX_PAYLOAD_BYTES` | `1048576` | Maximum JSON request body size accepted by the endpoint. |
| `MCP_DOCS_ROOT` | unset | Optional directory used when serving MCP documentation resources. |
| `MCP_EXAMPLES_ROOT` | unset | Optional directory used when returning driver examples. |

With the packaged `/xact` proxy path, the default external URL is:

```text
http://localhost:8080/xact/api/v1/mcp
```

If XACT is served without a proxy path, the default external URL is:

```text
http://localhost:8080/api/v1/mcp
```

## Authentication

The MCP endpoint is protected by the same JWT middleware as the XACT API. Clients must send:

```text
Authorization: Bearer <xact-session-token>
```

Use the normal login endpoint to obtain a token:

```sh
curl -sS -X POST http://localhost:8080/xact/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}'
```

The response includes a `token`, `token_type`, `expires_in`, and the user's current `tenant_id`. Tokens are live session tokens and expire after the configured session lifetime. If a user is deactivated or their token version changes, existing tokens are rejected.

REST ingest API keys are not accepted by the MCP endpoint. API keys are for device ingest only.

### Agent Tokens

For standalone agents and MCP clients, use an **agent token** instead of a username/password login flow. Agent tokens are created in the Agent Keys widget, are scoped to the current organisation, and carry the owning user's XACT roles for that organisation. MCP permission checks use those roles in the same way they use a normal user's roles.

Use an agent token directly as the bearer token:

```sh
export XACT_AGENT_TOKEN="xat_..."

curl -sS http://localhost:8080/xact/api/v1/mcp \
  -H "Authorization: Bearer $XACT_AGENT_TOKEN"
```

Delete the token from the Agent Keys widget to revoke access.

### Organisation Context

MCP tools run inside the organisation stored in the user's JWT. For a multi-organisation user, switch organisation before using MCP if needed:

```sh
curl -sS -X POST http://localhost:8080/xact/api/v1/auth/switch-org \
  -H "Authorization: Bearer $XACT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"org":"AcmeCorp"}'
```

Use the token returned by `switch-org` for subsequent MCP requests.

## Transport and Route Behaviour

The endpoint uses JSON-RPC over HTTP:

| Method | Behaviour |
|--------|-----------|
| `GET` | Returns server metadata, protocol version, route, and capabilities. |
| `POST` | Accepts MCP JSON-RPC requests such as `initialize`, `tools/list`, `tools/call`, `resources/list`, and `resources/read`. |
| Other methods | Return `405 Method Not Allowed`. |

The server reports MCP protocol version `2024-11-05`. It supports tools, resources, empty prompt lists, single JSON-RPC requests, and JSON-RPC batches. It does not expose a separate SSE or stdio transport; stdio-only clients need an HTTP-to-stdio bridge.

## Permissions

MCP uses the authenticated user's normal UI permissions. `SystemAdmin` bypasses permission checks, just as it does in the XACT UI.

Read-only and planning tools are available when the user has the listed permission, or when no extra permission is listed:

| Tool | Purpose | Permission |
|------|---------|------------|
| `xact_get_tag` | Read one tag's value and metadata | `tags.read` |
| `xact_browse_tree` | Browse RTDB nodes and leaves | `nodes.read` |
| `xact_find_tags` | Search RTDB tags | `tags.read` |
| `xact_query_history` | Query historical metric series in the current organisation | Authenticated organisation context |
| `xact_get_block_schemas` | Return processing block schemas | `tags.read` |
| `xact_generate_ingest_driver_context` | Generate REST, MQTT, or NATS driver context | Authenticated user |
| `xact_get_driver_examples` | Return bundled driver examples when available | Authenticated user |
| `xact_validate_provisioning_plan` | Validate a device provisioning plan without writing | Authenticated user |

Mutating tools require both `MCP_WRITE_TOOLS_ENABLED=yes` and the matching permission. Most mutating operations default to `dryRun: true`; pass `dryRun: false` only when you intend to apply the change.

| Tool | Mutating operations | Permission |
|------|---------------------|------------|
| `xact_provision_device` | Provision device tags through the ingest processor | `tags.write` |
| `xact_provision_scheduler` | `create`, `update`, `delete`, `run` | `scheduler.manage` |
| `xact_define_report` | `create`, `update`, `delete` | `reports.manage` |
| `xact_define_tag_calc` | `test`, `create`, `update`, `disable`, `delete` | `tagcalcs.manage` |

Scheduler tools still obey scheduler safety settings. Unsafe task types remain unavailable unless the server is configured to allow them.

## Testing with curl

Store a login token:

```sh
export XACT_TOKEN="$(curl -sS -X POST http://localhost:8080/xact/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"your-password"}' | jq -r .token)"
```

Check endpoint metadata:

```sh
curl -sS http://localhost:8080/xact/api/v1/mcp \
  -H "Authorization: Bearer $XACT_TOKEN"
```

Initialize an MCP session:

```sh
curl -sS -X POST http://localhost:8080/xact/api/v1/mcp \
  -H "Authorization: Bearer $XACT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 1,
    "method": "initialize",
    "params": {
      "protocolVersion": "2024-11-05",
      "capabilities": {},
      "clientInfo": {"name": "curl", "version": "1.0"}
    }
  }'
```

List available tools:

```sh
curl -sS -X POST http://localhost:8080/xact/api/v1/mcp \
  -H "Authorization: Bearer $XACT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

Browse the top of the current organisation's tree:

```sh
curl -sS -X POST http://localhost:8080/xact/api/v1/mcp \
  -H "Authorization: Bearer $XACT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 3,
    "method": "tools/call",
    "params": {
      "name": "xact_browse_tree",
      "arguments": {"path": "", "depth": 1}
    }
  }'
```

Read a tag:

```sh
curl -sS -X POST http://localhost:8080/xact/api/v1/mcp \
  -H "Authorization: Bearer $XACT_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "jsonrpc": "2.0",
    "id": 4,
    "method": "tools/call",
    "params": {
      "name": "xact_get_tag",
      "arguments": {"path": "PumpStation.Pump01.status"}
    }
  }'
```

## HTTP-Capable MCP Clients

Clients that support remote HTTP MCP servers need the endpoint URL and a bearer token. For standalone agents, prefer an agent token in an environment variable:

```json
{
  "mcpServers": {
    "xact": {
      "type": "http",
      "url": "http://localhost:8080/xact/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer ${XACT_AGENT_TOKEN}"
      }
    }
  }
}
```

Store the token in the client's secret or environment-variable mechanism when possible. Avoid committing bearer tokens to project configuration files.

## Claude Desktop and Other stdio Clients

Some desktop clients only launch stdio MCP servers. Use an HTTP-to-stdio bridge and pass the XACT URL plus the `Authorization` header. For bridges that accept a URL and a `--header` option, the configuration looks like this:

```json
{
  "mcpServers": {
    "xact": {
      "command": "npx",
      "args": [
        "-y",
        "mcp-remote",
        "http://localhost:8080/xact/api/v1/mcp",
        "--header",
        "Authorization: Bearer paste-session-token-here"
      ]
    }
  }
}
```

If the bridge or client supports secret storage or environment-variable interpolation, store the token there instead of writing it directly in the configuration. If the bridge uses a different header syntax, keep the same two required values: the XACT MCP URL and `Authorization: Bearer <token>`.

## VS Code, Cursor, and Similar Editors

Editor clients that support remote MCP servers usually use workspace or user settings. Use an HTTP server entry when available:

```json
{
  "servers": {
    "xact": {
      "type": "http",
      "url": "http://localhost:8080/xact/api/v1/mcp",
      "headers": {
        "Authorization": "Bearer ${env:XACT_TOKEN}"
      }
    }
  }
}
```

For stdio-only editor integrations, use the bridge pattern from the previous section.

## Applying Changes Safely

Leave write tools disabled unless the client must create or modify XACT resources:

```sh
MCP_WRITE_TOOLS_ENABLED=no
```

When write tools are enabled, keep the client's role as narrow as possible and ask the client to preview changes first. For example, a device provisioning request should be sent with the default dry run before it is applied:

```json
{
  "jsonrpc": "2.0",
  "id": 10,
  "method": "tools/call",
  "params": {
    "name": "xact_provision_device",
    "arguments": {
      "tenant": "default",
      "deviceType": "Pump",
      "deviceName": "Pump01",
      "tags": [
        {"name": "status", "type": "string", "description": "Current pump state"},
        {"group": "metrics", "name": "flow", "type": "number", "units": "L/s", "history": true}
      ]
    }
  }
}
```

After reviewing the response, apply the change by sending the full reviewed request again with `dryRun: false` inside `params.arguments`.

## Security Recommendations

- Prefer HTTPS when MCP clients connect across a network.
- Use a dedicated XACT user for AI clients rather than a shared administrator account.
- Grant only the permissions needed by the client's workflow.
- Keep `MCP_WRITE_TOOLS_ENABLED=no` for read-only analysis, troubleshooting, and dashboard assistance.
- Rotate tokens by logging out or changing the user's password when a token may have been exposed.
- Restrict network access to the XACT server with firewall or reverse-proxy rules.

## Example prompts

### Create a dashboard

The following prompt creates a dashboard with the requested characteristics. Some minor manual tweaks, like the default zoom level and actual icons to use fine tune the dashboard.

> Generate a top level dashboard showing the air quality sensors on a map. Show a black icon if the AQI is normal and red if it is in alarm. On hover and click show the main KPI values of the sensor, the air quality and particulate values. Implement the link to the detail  dashboard 'Air Quality Device'. Place both sensor types on the same layer.

### Ad hoc report

This simple prompt generates the required report.

> Create a PDF report showing the air quality sensors with the 5 worst AQI. The data must be displayed in charts. Compare the AQI for each sensor with the previous 3 day's history. The report must be professional and asthetically pleasing.
