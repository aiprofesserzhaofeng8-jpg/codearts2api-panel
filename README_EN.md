<div align="center">

# CodeArts2API Panel

**A multi-account API gateway and management panel for CodeArts, WorkBuddy, and Trae CN/SOLO**

Access CodeArts, WorkBuddy, and Trae CN/SOLO accounts through Chat Completions, Responses, and Anthropic Messages compatible endpoints.

[中文](README.md) · **English**

[![CI](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/workflows/ci.yml/badge.svg?branch=develop)](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.27.3%2B-00ADD8?logo=go)](go.mod)

[Deployment](#deployment) · [Client setup](#client-setup) · [Documentation](#documentation) · [Commit history](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/commits/develop/) · [Contributors](#contributors-and-acknowledgments)

</div>

> This is the `develop` version with native WorkBuddy and Trae CN/SOLO adapters. Trae international accounts are not supported, and live Trae inference has not been validated with an account in this environment. The original CodeArts baseline is pinned at [v0.1.0](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/releases/tag/v0.1.0). See [multi-provider setup and verification limits](docs/MULTI-PROVIDER.md). Real WorkBuddy model generation has not yet been validated with an account in this environment.

## Overview

CodeArts2API Panel provides account management, model request forwarding, protocol translation, and usage tracking for personal use. The service uses the Go standard library, embeds its web interface in the binary, and stores state in a local JSON file.

Run it locally, on a server, or in Docker. The listening address and port are configurable, and an HTTPS reverse proxy can provide public access. The current architecture supports one process and one administrator. Multiple instances must not share the same state file.

## Features

| Feature | Description |
| --- | --- |
| Multiple accounts | Staged browser authorization, pasted callbacks, account preview and explicit confirmation, import and deletion |
| Account scheduling | Least in-flight requests, round robin, or sequential exhaustion; session affinity, concurrency limits, pause, and cooldown |
| Protocol compatibility | Chat Completions, Responses, and Anthropic Messages, with streaming and tool calls |
| Administrator access | Password hashing, 8-hour sessions, revocation on logout or password changes, and management audit logs |
| API keys | Create, disable, delete, reveal, and copy keys; request, token, and USD budgets |
| Usage tracking | Input, output, and cached tokens; estimated cost, latency, date filters, and pagination |
| Model configuration | Model catalog, aliases, USD pricing, context recommendations, and summary policies |
| Summary recovery | Completeness checks, bounded retries, optional fallback models, and automatic installation of Pi's segmented recovery adapter |
| Client setup | Launchers for Pi, Codex, and Claude Code that read recommended settings from the gateway |

Upstream charges depend on the account's actual entitlements and credits. The USD price table provides reference API cost estimates, not Huawei credit billing. Token and USD budgets are settled using reported usage, so an individual request can exceed the remaining budget.

## Deployment

### Option 1: Build from source

Requires Git and Go 1.27.3 or later. The commands below use a POSIX shell.

```bash
git clone --branch develop https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel.git
cd codearts2api-panel
./build.sh
./start.sh
```

Default address: [http://127.0.0.1:10087/](http://127.0.0.1:10087/).

The script runs in the foreground. Press `Ctrl+C` to stop it. Configure a service manager such as systemd separately if you need automatic startup.

Set a custom listening address and port:

```bash
./start.sh -host 0.0.0.0 -port 10087
```

Or use environment variables:

```bash
CODEARTS_HOST=0.0.0.0 CODEARTS_PORT=10087 ./start.sh
```

Restart the service after changing these settings. `0.0.0.0` means listening on all interfaces; use the server's IP address or domain name in your browser.

### Option 2: Docker Compose

Requires Docker and Compose.

```bash
git clone --branch develop https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel.git
cd codearts2api-panel
cp .env.example .env
docker compose up -d --build
```

Compose stores data in a persistent named volume and defaults to the port mapping `127.0.0.1:10087:10087`. To allow direct remote access, change the mapping in `docker-compose.yml` to `10087:10087` and configure the server firewall and security group accordingly.

Useful commands:

```bash
docker compose logs -f codearts2api
docker compose restart codearts2api
docker compose down
```

### Domains and public access

A public deployment can use `Client → HTTPS domain → Nginx/Caddy → Gateway`. With a reverse proxy on the same host, the gateway can keep listening on `127.0.0.1:10087`. Preserve the original Host header, disable response buffering, and allow sufficient read timeouts for streaming responses.

The service does not provide HTTPS itself. Keep administrator authentication and API key checks enabled, and leave local automatic unlock and anonymous access disabled. When TLS terminates at the proxy, configure the trusted proxy to add the Secure attribute to administrator session cookies. See [Security](SECURITY.md) and [Management](docs/MANAGEMENT.md) for deployment boundaries.

### Updates and backups

Back up the state file or Docker data volume before updating the code and rebuilding:

```bash
git pull --ff-only
./build.sh
# Restart the running service. For Docker deployments, use:
# docker compose up -d --build
```

Backups contain account credentials and need protection. Check the corresponding commit descriptions for migration and rollback requirements; older versions may reject newer state formats.

## Initial setup

1. Retrieve the administrator password and API key generated on first startup.
2. Open the panel and sign in with `admin_password`.
3. Under the account pool (账号池), open the browser login dialog, generate a link, and copy it to another browser to authorize the account.
4. Paste the final callback URL, submit the authorization information, review the account preview, and confirm saving.
5. Refresh the model catalog and use an available model ID. Model probes consume real upstream quota.
6. Under API keys (API 密钥), create a separate key for each client and configure budgets as needed.

### Where is the administrator login password?

The first startup generates an administrator password. Open the initial credentials file below and use the **value of `admin_password`** to sign in to the panel. The `api_key` field is for client API calls, not administrator login.

**Source / start.sh deployment**: run this from the project directory to view `data/state.json.access.json`:

```bash
cat data/state.json.access.json
```

**Docker Compose deployment**: view `/data/codearts2api-data.json.access.json` inside the container:

```bash
docker compose exec codearts2api cat /data/codearts2api-data.json.access.json
```

If you supplied `CODEARTS_ADMIN_PASSWORD` at startup, use that password. The initial credentials file is not updated after later password changes. The value stored in `state.json` is a password hash and cannot be used as the login password.

To reset a forgotten password, set a new `CODEARTS_ADMIN_PASSWORD` in the service's startup environment and restart it. When using `start.sh`, stop the existing service first, then run:

```bash
CODEARTS_ADMIN_PASSWORD='replace-with-your-new-password' ./start.sh
```

For Docker Compose, set `CODEARTS_ADMIN_PASSWORD` in `.env`, then run `docker compose up -d` to apply the new environment variable. The initial credentials file contains a real password and API key; keep it private and do not upload it publicly.

See [Management](docs/MANAGEMENT.md) and [Panel](docs/PANEL.md) for login, credential refresh, and recovery details.

## API endpoints

| Endpoint | Purpose |
| --- | --- |
| `POST /v1/chat/completions` | Chat Completions, streaming/non-streaming, and tool calls |
| `POST /v1/responses`, alias `/responses` | Responses-to-Chat translation |
| `POST /v1/messages` | Anthropic Messages-to-Chat translation |
| `GET /v1/models` | Model catalog and available client configuration recommendations |
| `GET /v1/client-profile` | Client recommendations for a specified model |
| `GET /v1/capabilities` | Gateway capabilities and upstream protocol information |
| `/admin/api/*`, `GET /metrics` | Management endpoints and metrics requiring administrator authentication |
| `GET /healthz` | Service health check without authentication |

Use a client API key for `/v1/*`. Replace the sample model with one available to your account:

```bash
export CODEARTS_API_KEY='your-api-key'
curl http://127.0.0.1:10087/v1/chat/completions \
  -H "Authorization: Bearer $CODEARTS_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"model":"deepseek-v4.1-flash","messages":[{"role":"user","content":"Hello"}],"stream":true}'
```

Responses support is implemented through protocol translation. Native compaction, WebSocket, `store=true`, and `previous_response_id` are not supported. A model alias does not mean the upstream serves that vendor's actual model. See [Responses compatibility](docs/RESPONSES.md) for supported behavior and tool-call limitations.

For Trae, use **Add Trae** in the account panel, paste the authorization callback or import complete credentials, and select `trae/cn/<config_name>`. See [Trae setup and validation limits](docs/TRAE.md).

## Client setup

```bash
python3 scripts/connect-client.py pi --url http://127.0.0.1:10087
python3 scripts/connect-client.py codex --url http://127.0.0.1:10087
python3 scripts/connect-client.py claude --url http://127.0.0.1:10087
```

Provide the key through the hidden prompt or the `CODEARTS_API_KEY` environment variable. The launcher reads model recommendations and automatically installs Pi's bounded summary recovery adapter. On Linux, it checks Codex's native sandbox before launch. Add `--check` to validate configuration without starting a model request.

See [Client setup](docs/CLIENTS.md). Client versions and local permissions affect compatibility; measured results and outstanding checks are recorded in the [acceptance snapshot](docs/acceptance/QUALITY-SNAPSHOT.md).

## Documentation

The detailed documents and management interface are currently primarily in Chinese. This English README does not imply an English UI or translated versions of every linked document.

| Document | Contents |
| --- | --- |
| [Panel and account pool](docs/PANEL.md) | Login flow, scheduling, cooldown, and failure handling |
| [WorkBuddy growth tasks](docs/WORKBUDDY-GROWTH.md) | Read current progress and claim eligible rewards; task execution is not automated |
| [Management and budgets](docs/MANAGEMENT.md) | Administrator sessions, audit logs, credential refresh, key budgets, and recovery |
| [Usage and pricing](docs/USAGE.md) | Token accounting, USD pricing, date filters, and pagination |
| [Client setup](docs/CLIENTS.md) | Pi, Codex, and Claude Code configuration and limitations |
| [Protocol compatibility](docs/RESPONSES.md) | Responses translation, tools, and reasoning history |
| [Sub2API design references](docs/SUB2API-ALIGNMENT.md) | Implemented capabilities and remaining gaps |
| [Quality acceptance plan](docs/acceptance/QUALITY-PLAN.md) | Methods, acceptance criteria, and repeatable checks |
| [Security policy](SECURITY.md) | Vulnerability reporting, deployment boundaries, and data protection |

## Development and contributions

```bash
go test ./...
go test -race ./...
go vet ./...
python3 -m unittest discover -s scripts -p test_connect_client.py
node scripts/pi/codearts-compaction.test.mjs
```

The Go service uses only the standard library. Pi adapter tests use Node.js 24; browser checks additionally require Playwright and a browser. Live upstream acceptance tests consume real account quota; consult the acceptance plan before running them.

Issues and pull requests are welcome. Record each change's motivation, resulting behavior, validation, and limitations in its commit description. Keep the README focused on current usage. See [CONTRIBUTING.md](CONTRIBUTING.md) and the [commit history](https://github.com/aiprofesserzhaofeng8-jpg/codearts2api-panel/commits/develop/).

## Contributors and acknowledgments

<table>
  <tr>
    <td align="center">
      <a href="https://github.com/aiprofesserzhaofeng8-jpg"><img src="https://github.com/aiprofesserzhaofeng8-jpg.png?size=80" width="64" height="64" alt="Project maintainer avatar"></a><br>
      <a href="https://github.com/aiprofesserzhaofeng8-jpg">aiprofesserzhaofeng8-jpg</a><br>
      Project maintenance
    </td>
    <td align="center">
      <a href="CONTRIBUTORS.md#ai-辅助开发贡献"><img src="https://github.com/openai.png?size=80" width="64" height="64" alt="OpenAI tool identifier"></a><br>
      OpenAI ChatGPT / Codex<br>
      AI-assisted development attribution
    </td>
  </tr>
</table>

| Contributor / project | Contribution |
| --- | --- |
| [aiprofesserzhaofeng8-jpg](https://github.com/aiprofesserzhaofeng8-jpg) | Requirements, usage feedback, repository maintenance, and publishing |
| **OpenAI ChatGPT / Codex** | AI assistance with design discussions, implementation, troubleshooting, testing, and documentation |
| [CodeArts2API](https://github.com/2951461586/CodeArts2API) and its contributors | Base CodeArts protocol, signing, login, and service implementation |
| [WorkBuddy2API Panel](https://github.com/linguo2625469/workbuddy2api-panel) and its original authors | Account management concepts and visual design references |
| [Sub2API](https://github.com/Wei-Shaw/sub2api) and its contributors | Responses conversion library, management design, and README organization references |

AI attribution records the use of OpenAI tools for assisted development. It does not imply maintenance by OpenAI, an official partnership, or endorsement. See [CONTRIBUTORS.md](CONTRIBUTORS.md) for details.

## License

The base project uses [Apache-2.0](LICENSE). WorkBuddy-related design references retain their [MIT notices](LICENSE.workbuddy). Reused code under `internal/responsescompat` uses [LGPL-3.0](internal/responsescompat/LICENSE), and the bundled Ubuntu AppArmor profile retains its GPL-2.0-or-later notices.

See [NOTICE](NOTICE) for component origins, modification notices, and license boundaries. This is an independent community project.
