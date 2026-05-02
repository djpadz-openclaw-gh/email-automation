# Email Automation

Lua-powered email rule engine with a Go backend, Next.js frontend, and Kubernetes deployment.

## Architecture

```
┌─────────────┐     ┌──────────────┐     ┌─────────────┐
│  Next.js UI  │────▶│  Go API      │────▶│ PostgreSQL  │
│  (Monaco)    │     │  (Fiber)     │     │             │
└─────────────┘     └──────┬───────┘     └─────────────┘
                           │
                    ┌──────┴───────┐
                    │              │
              ┌─────▼─────┐ ┌─────▼─────┐
              │ Lua Engine │ │   NATS    │
              │ (gopher-  │ │  Event    │
              │  lua)     │ │   Bus     │
              └─────┬─────┘ └───────────┘
                    │
              ┌─────▼─────┐
              │ IMAP Pool │──▶ IMAP Servers
              │ (workers) │
              └───────────┘
```

- **API Server** — Fiber-based REST API for rules, accounts, and logs
- **Rule Engine** — Sandboxed Lua (gopher-lua) with no filesystem/network access
- **IMAP Workers** — Goroutine pool, one per account, with IDLE support
- **Scheduler** — Processes deferred actions on a timer
- **Notifier** — Telegram Bot API notifications
- **NATS** — Internal event bus for decoupled messaging
- **PostgreSQL** — Rules, accounts, execution logs, deferred actions

## Features

- Multi-tenant, multi-account support
- 18 pre-built Lua rules (migrated from Python)
- Monaco Editor with Lua syntax highlighting
- Reference popup (email context, actions, helpers, examples)
- Rule testing against synthetic email contexts
- Lua syntax validation
- Time-based rule execution (deferred actions)
- IMAP IDLE for real-time monitoring
- Telegram notifications
- Protocol-agnostic design (IMAP now, Graph/MAPI later)
- Production-ready Kubernetes deployment

## Pre-built Rules

| Rule | Action | Description |
|------|--------|-------------|
| Amazon | Move → @Amazon | Order/shipping emails after 24h |
| Atlassian | Move → @Receipts & Invoices | Payment confirmations |
| Calendar Responses | Archive | Accept/decline with .ics after 24h |
| Contabo | Move → @Receipts | Billing emails |
| CrowdStrike | Move → @SaneNews | Weekly intelligence reports |
| DMARC Reports | Move → @30DayTrash | Aggregate reports |
| Headway | Delete | Appointment reminders after date |
| Insight Marketing | Move → @SaneNews | Marketing emails |
| IronPort | Move → @Ironport | Alert emails (not replies) |
| Login Codes | Delete | Verification codes after 1h |
| Morgan Stanley | Move → @MorganStanley | Notifications after 24h |
| rblmon | Delete | "No blocks found" alerts |
| Receipts | Move → @Receipts | Order/payment confirmations |
| SaneBox | Delete | Digest emails after 24h |
| Scripps Video Visit | Delete | Telehealth links after 24h |
| Teams | Delete | Notification emails after 6h |
| USPS | Move → @30DayTrash | Informed Delivery after 1 day |
| Venmo | Move → @Receipts | "You paid" after 2 days |

## Quick Start

### Local Development

```bash
# Start dependencies
docker run -d --name postgres -e POSTGRES_USER=emailauto -e POSTGRES_PASSWORD=emailauto -e POSTGRES_DB=emailauto -p 5432:5432 postgres:16-alpine
docker run -d --name nats -p 4222:4222 nats:2.10-alpine

# Run backend
cd /path/to/email-automation
go run ./cmd/server

# Run frontend (separate terminal)
cd frontend
npm install
npm run dev
```

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SERVER_PORT` | 8080 | API server port |
| `DATABASE_URL` | postgres://emailauto:emailauto@localhost:5432/emailauto?sslmode=disable | PostgreSQL connection |
| `NATS_URL` | nats://localhost:4222 | NATS server |
| `RULES_DIR` | ./rules | Lua rules directory |
| `API_KEY` | (empty) | System admin API key |
| `TELEGRAM_BOT_TOKEN` | (empty) | Telegram bot token |
| `TELEGRAM_CHAT_ID` | (empty) | Telegram chat ID |
| `IMAP_WORKER_COUNT` | 4 | Max concurrent IMAP workers |
| `IMAP_POLL_INTERVAL` | 5m | Polling interval between IDLE |
| `SCHEDULER_INTERVAL` | 30s | Deferred action check interval |
| `LOG_LEVEL` | info | Log level (debug, info, warn, error) |
| `LOG_JSON` | true | JSON log format |

### Kubernetes Deployment

```bash
# Create namespace and infrastructure
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/rbac.yaml
kubectl apply -f k8s/secrets.yaml  # Edit secrets first!
kubectl apply -f k8s/postgres.yaml
kubectl apply -f k8s/nats.yaml

# Generate and apply rules ConfigMap
bash k8s/generate-rules-configmap.sh > k8s/configmap-rules.yaml
kubectl apply -f k8s/configmap-rules.yaml

# Build and push images
docker build -t registry.container-registry.svc.cluster.local:32000/email-automation:latest .
docker build -f Dockerfile.frontend -t registry.container-registry.svc.cluster.local:32000/email-automation-frontend:latest .
docker push registry.container-registry.svc.cluster.local:32000/email-automation:latest
docker push registry.container-registry.svc.cluster.local:32000/email-automation-frontend:latest

# Deploy application
kubectl apply -f k8s/api.yaml
kubectl apply -f k8s/frontend.yaml
kubectl apply -f k8s/ingress.yaml
```

## API

### Authentication

All API endpoints (except `/health` and `/ready`) require an API key:

```
X-API-Key: ea_your_tenant_key_here
```

Or via Bearer token:
```
Authorization: Bearer ea_your_tenant_key_here
```

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Health check |
| GET | `/ready` | Readiness check |
| POST | `/admin/tenants` | Create tenant (admin key) |
| GET | `/admin/tenants` | List tenants (admin key) |
| GET | `/api/v1/rules` | List rules |
| GET | `/api/v1/rules/:id` | Get rule |
| POST | `/api/v1/rules` | Create rule |
| PUT | `/api/v1/rules/:id` | Update rule |
| DELETE | `/api/v1/rules/:id` | Delete rule |
| POST | `/api/v1/rules/test` | Test rule against email |
| POST | `/api/v1/rules/validate` | Validate Lua syntax |
| GET | `/api/v1/accounts` | List accounts |
| POST | `/api/v1/accounts` | Create account |
| PUT | `/api/v1/accounts/:id` | Update account |
| DELETE | `/api/v1/accounts/:id` | Delete account |
| GET | `/api/v1/logs` | Execution logs |

## Writing Rules

Rules are Lua scripts that receive an `email` table and call an action function:

```lua
-- Example: Move old newsletters to a folder
local sender = email.sender_address:lower()
local subject = email.subject:lower()

if not sender:find("newsletter.example.com", 1, true) then
    return skip()
end

if not older_than_days(1) then
    return keep("Keep recent newsletters")
end

return move("@Newsletters", "Filed newsletter")
```

### Available Actions

- `skip()` — Rule doesn't apply, try next rule
- `delete(reason?)` — Delete the message
- `archive(reason?)` — Move to archive
- `move(folder, reason?)` — Move to IMAP folder
- `keep(reason?)` — Keep in inbox, stop rule chain
- `notify(message, reason?)` — Send Telegram notification
- `defer_action(action, target, delay_secs, reason?)` — Schedule for later
- `move_after(folder, delay_secs, reason?)` — Move after delay
- `delete_after(delay_secs, reason?)` — Delete after delay

### Helper Functions

- `contains(haystack, needle)` — Case-insensitive contains
- `contains_any(haystack, {needles})` — Check against list
- `starts_with(text, prefix)` / `ends_with(text, suffix)`
- `domain_of(email_addr)` — Extract domain
- `older_than(seconds)` / `older_than_hours(hours)` / `older_than_days(days)`
- `has_ics()` — Check for calendar attachment
- `has_attachment_type(mime_type)`
- `is_reply()` — Check for Re:/Fwd: prefix

## License

MIT
