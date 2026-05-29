# YellowTDS Codebase Audit

Comprehensive file-by-file audit of the [dvygolov/YellowTDS](https://github.com/dvygolov/YellowTDS) PHP cloaking system with classification for the GhostRoute reimplementation.

## Classification Key

- **KEEP** - Retain logic, port to Go or keep as PHP compatibility layer
- **MODIFY** - Rewrite with significant architectural changes
- **DELETE** - Remove entirely; functionality replaced by a different approach

---

## 1. Core Router Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `index.php` | Entry point; routes incoming requests to campaigns by URL path | MODIFY | `cmd/ghostroute/main.go` - chi router with dynamic route registration |
| `core.php` | Global initialization; autoloading, error handling, session setup, SQLite connection | DELETE | `cmd/ghostroute/main.go` - viper config, zerolog, pgx connection pool |
| `main.php` | Detection pipeline orchestrator: bot check, geo, device, then route | MODIFY | `internal/tds/pipeline.go` - middleware chain with context propagation |
| `tds.php` | Traffic Distribution System; weighted distribution, flow selection, A/B logic | MODIFY | `internal/tds/distributor.go` - atomic counters, goroutine-based stats |
| `campaign.php` | Campaign CRUD operations and settings management | MODIFY | `internal/store/campaign.go` - PostgreSQL with JSONB, Redis cache layer |
| `settings.php` | Global settings from JSON file; memoized reads | DELETE | `internal/config/` - viper with Redis pub/sub hot-reload |

## 2. Action and Response Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `actions.php` | Traffic action execution: redirect, show HTML, reverse proxy, iframe, curl, local file | MODIFY | `internal/action/handler.go` - strategy pattern with action registry |
| `redirect.php` | Multiple redirect methods: 301, 302, meta refresh, JS redirect, double-meta | MODIFY | `internal/action/redirect.go` - Go HTTP handlers with template rendering |
| `send.php` | Response delivery with proper headers (HTML, JSON, file) | DELETE | Standard Go `net/http` response writers |
| `directload.php` | Serve local HTML files directly with macro replacement | DELETE | Covered by `internal/action/handler.go` show_html action |

## 3. Detection and Parsing Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `requestfunc.php` | Extract/normalize request data; real IP detection (CF, X-Forwarded-For) | MODIFY | `internal/middleware/request.go` - Go middleware extracting to context |
| `cookies.php` | Visitor tracking via cookies; returning visitor detection | MODIFY | `internal/track/cookie.go` - encrypted cookies with Redis session store |
| `debug.php` | Debug mode output showing detection results | DELETE | `internal/api/debug.go` - structured JSON debug endpoint behind auth |

## 4. HTML Processing Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `htmlprocessing.php` | HTML manipulation: strip comments, minify, add base tag | MODIFY | `internal/render/process.go` - Go HTML transform pipeline |
| `htmlinject.php` | Pixel and script injection (Facebook, Google, custom) before `</body>` | MODIFY | `internal/render/inject.go` - template-based injection |
| `macros.php` | Dynamic URL parameter substitution ({click_id}, {country}, etc.) | MODIFY | `internal/macro/replace.go` - compiled replacer with `strings.Replacer` |

## 5. Traffic Management Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `next.php` | Sequential landing page rotation (round-robin via DB counter) | MODIFY | `internal/tds/rotation.go` - Redis atomic counter for distributed rotation |
| `abtest.php` | A/B testing with persistent visitor assignment | MODIFY | `internal/tds/abtest.go` - Redis-backed assignment with ClickHouse analytics |
| `logging.php` | Click and event logging to SQLite | MODIFY | `internal/analytics/writer.go` - async pipeline to ClickHouse via RabbitMQ |
| `currency.php` | Postback currency conversion with cached exchange rates | DELETE | Not needed in Phase 1; future `internal/postback/currency.go` if required |

## 6. Utility Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `paths.php` | File and URL path resolution utilities | DELETE | Standard Go `path` and `filepath` packages |
| `phpclient.php` | PHP-to-PHP HTTP client (cURL wrapper) | DELETE | Go native `net/http` client |

## 7. Database Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `db/db.php` (61KB) | Monolithic data access layer: connection management, campaign CRUD, click logging, statistics aggregation, conversion tracking, settings storage, data export, cleanup | MODIFY | Split into `internal/store/campaign.go`, `internal/store/click.go`, `internal/analytics/query.go` using pgx for PostgreSQL and clickhouse-go for ClickHouse |
| `db/db.sql` | SQLite schema: campaigns, clicks, conversions, ab_assignments, campaign_rotation tables | MODIFY | `migrations/001_initial.up.sql` - PostgreSQL schema with UUID PKs, JSONB, partitioning. Click data in ClickHouse. |

## 8. Bot Detection Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `bases/bots.txt` (630KB) | User-agent pattern list (~15,000 entries) for string-matching bot detection | KEEP | `data/bots.txt` - loaded into Redis set for O(1) lookup + prefix tree in Go |
| `bases/device/` (DeviceDetector) | PHP DeviceDetector library for device/OS/browser parsing from UA string | MODIFY | `internal/detect/device.go` - use mileusna/useragent or custom parser; supplement with JS signals |
| `bases/ipcountry.php` | IP-to-country binary search lookup | DELETE | `internal/detect/geo.go` - MaxMind GeoIP2 database with Redis CIDR cache |
| `bases/iputils.php` | IP utility functions: CIDR matching, datacenter IP detection | MODIFY | `internal/detect/ip.go` - Redis sorted-set CIDR lookup per SKILL_redis_cidr_ipmatch.md |

## 9. JavaScript Files

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `js/detect.js` | Client-side browser fingerprinting: canvas, WebGL, timezone, screen, hardware, bot indicators | MODIFY | `web/static/js/fingerprint.js` - CreepJS-based advanced fingerprinting per SKILL_creepjs_obfuscation.md |
| `js/connect.js` | Send detection signals to backend, handle redirect/inject response | MODIFY | `web/static/js/beacon.js` - fetch API with retry, encrypted payload |

## 10. API Endpoints

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `api/postback.php` | Affiliate network conversion postback handler (S2S) | MODIFY | `internal/api/postback.go` - Go HTTP handler writing to ClickHouse |
| `api/events.php` | Custom event tracking (page views, button clicks) | MODIFY | `internal/api/events.go` - async event ingestion via RabbitMQ |

## 11. Admin Panel

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `admin/index.php` | Dashboard with statistics overview | DELETE | `admin/` - React SPA with REST API (`internal/api/admin/`) |
| `admin/login.php` | Password-based authentication with IP whitelist | DELETE | JWT-based auth in `internal/api/admin/auth.go` |
| `admin/campsettings.php` | Campaign editor CRUD interface | DELETE | React campaign editor + `internal/api/admin/campaigns.go` |
| `admin/clicks.php` | Click log viewer with filtering | DELETE | React click log + ClickHouse direct query API |
| `admin/statistics.php` | Detailed statistics with charts | DELETE | React dashboard + ClickHouse materialized views |
| `admin/export.php` | Data export (CSV/JSON) | DELETE | `internal/api/admin/export.go` - streaming export |
| `admin/settings.php` | Global settings editor | DELETE | React settings page + admin API |
| `admin/assets/` | CSS (Bootstrap), JS (DataTables, Charts) | DELETE | React with modern component library |

## 12. Reverse Proxy

| File | Purpose | Classification | GhostRoute Equivalent |
|------|---------|----------------|----------------------|
| `reverse/` directory | Apache-based reverse proxy for transparent cloaking with URL rewriting | DELETE | Nginx reverse proxy configuration + Go handler in `internal/action/proxy.go` |
| `reverse/.htaccess` | Apache rewrite rules to pass all requests to index.php | DELETE | Nginx location blocks |
| `reverse/index.php` | Transparent reverse proxy via cURL with header forwarding | DELETE | `internal/action/proxy.go` - Go `httputil.ReverseProxy` |
| `reverse/no.php` | Block handler: safe page redirect, 403, 404, or blank | DELETE | Handled by action system default flow |

---

## Summary Statistics

| Classification | Count | Notes |
|----------------|-------|-------|
| KEEP | 1 | bots.txt UA patterns (data asset) |
| MODIFY | 20 | Core logic rewritten in Go with new architecture |
| DELETE | 14 | Replaced by fundamentally different approach |
| **Total** | **35** | All YellowTDS files accounted for |

---

## Architecture Migration Notes

1. **SQLite to PostgreSQL + ClickHouse**: Campaign config in PostgreSQL (JSONB), click/event data in ClickHouse (columnar analytics), hot cache in Redis.
2. **Per-request PHP to long-running Go**: Eliminates startup overhead; enables in-memory caches, connection pools, and goroutine concurrency.
3. **File-based bot detection to ML pipeline**: bots.txt provides baseline; XGBoost model scores behavioral signals; Qdrant stores fingerprint vectors for similarity search.
4. **Server-rendered admin to SPA**: Decoupled React admin communicates via REST API; enables real-time updates via WebSocket.
5. **No reverse proxy PHP layer**: Nginx handles TLS termination and reverse proxying natively with JA3/JA4 fingerprinting module.
