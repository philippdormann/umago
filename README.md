<p align="center">
  <picture>
    <source srcset="umago-transparent-white-font.png" media="(prefers-color-scheme: dark)" />
    <img src="umago-transparent-black-font.png" width="160" alt="Umago logo" />
  </picture>

  <h1 align="center">Umago</h1>

<p align="center">
  <img src="https://img.shields.io/github/go-mod/go-version/philippdormann/umago?filename=server%2Fgo.mod" alt="Go version" />
  <img src="https://img.shields.io/github/license/philippdormann/umago" alt="License" />
  <img src="https://github.com/philippdormann/umago/actions/workflows/ci.yml/badge.svg" alt="CI" />
</p>

A tiny, self-hosted web analytics service: visitors, page views, top pages, referrers, countries, devices and simple custom events. Not a product analytics platform.

One Go binary, one database (SQLite by default, Postgres if you prefer), one Docker container. Sites add a cookieless snippet of about 1 KB. No sessions, funnels, cohorts, heatmaps or replay. No third-party services at runtime: favicons are fetched by Umago itself, the world map ships its own outlines, and brand icons are bundled.

</p>

```html
<script defer src="https://umago.example.com/umago.js" data-site="site_…"></script>
```

## Why Umago?

Existing options were either heavy to self-host (large images, Next.js, ClickHouse/Postgres required, lots of dependencies) or cloud-only with per-event pricing. Umago is the small alternative:

- **Small to run.** One Go binary with the Svelte UI embedded, one ~30 MB Docker image idling at under 10 MB. No Next.js, no heavy stack.
- **SQLite by default, Postgres if you prefer.** No ClickHouse, no mandatory external database. Raw events prune after retention; rollups stay forever.
- **Location on your terms.** Off by default (proxy country header plus browser time zone, regions from the time-zone city; no IPs stored, ever). Opt in to IP-based `country` or `city` detail from DB-IP Lite or MaxMind GeoLite2 when you want it.
- **Fewer runtime dependencies.** No third-party services at runtime: favicons are fetched by Umago itself, the world map ships its own outlines, brand icons are bundled.
- **Agent-native and indie-complete.** Read-only [MCP endpoint](#mcp-for-ai-agents) so the agent you already use can answer "how are my sites doing?", plus [Google Search Console](#google-search-terms) terms and [Polar revenue](#revenue-from-polar) next to traffic.

## Architecture

The snippet posts one small JSON body per page view (site id, URL, referrer, screen width, time zone) with `sendBeacon`, and again on `pushState` and `popstate` for single-page apps. The Go server validates the host against the site's domain, drops bots, derives browser, OS, device, country, region and campaign tags, and pushes the event onto an in-memory queue. The request never touches the database. A writer goroutine commits the queue every second or every 200 events in one transaction. Every minute (and on demand when the dashboard is opened) today's and yesterday's rollups are rebuilt from raw events; the dashboard reads only rollups, so it stays fast however much traffic you keep. The embedded Svelte UI lists your sites and, per site, shows a chart, tabbed breakdown cards, a live 3D globe and a range map.

## What is in the box

| Part | Where |
| --- | --- |
| Go server (collect endpoint, batched writer, rollups, favicons, MCP, embedded web UI) | `server/` |
| Web UI (Svelte 5, Tailwind, shadcn-svelte, MapLibre, built into the binary) | `server/web/` |
| Tracking snippet (served at `/umago.js`) | `server/internal/api/umago.js` |

## Quick start (Docker, SQLite)

```bash
git clone https://github.com/philippdormann/umago && cd umago
cp .env.example .env          # set UMAGO_ADMIN_USER and UMAGO_ADMIN_PASSWORD
mkdir -p data && chown 1000:1000 data   # Linux hosts only; the container runs as uid 1000
docker compose -f docker-compose.sqlite.yml up -d --build   # or drop --build to pull ghcr.io/philippdormann/umago
open http://localhost:8082
```

Prebuilt images are published to `ghcr.io/philippdormann/umago` on every `x.y.z` tag (no `v` prefix): `latest`, `1`, `1.2`, `1.2.3` and so on for linux/amd64 and linux/arm64.

Add a website, click **Tracking code**, and paste the snippet before `</head>`. Data appears as soon as you open the dashboard.

Data lives in `./data/umago.db`. Back up by copying that file (use `sqlite3 data/umago.db ".backup backup.db"` for a consistent copy while running).

## Quick start (Docker, Postgres)

```bash
docker compose -f docker-compose.postgres.yml up -d --build
open http://localhost:8082
```

This starts Umago plus a Postgres 18 container; data lives in the `pgdata` volume. Set `POSTGRES_PASSWORD` in the environment (or `.env`) to change the database password from the sample default. The same `UMAGO_DATABASE_URL` shape works against any Postgres you manage yourself.

## Quick start (binary)

```bash
cd server/web && bun install && bun run build   # builds the Svelte UI into the Go embed directory
cd ../.. && cd server && CGO_ENABLED=0 go build -o ../bin/umago ./cmd/umago
UMAGO_DATABASE_PATH=./umago.db ./bin/umago     # listens on :8080
```

Configuration is the same set of environment variables as Docker (see [Configuration](#configuration)). `UMAGO_DATABASE_PATH` defaults to `/data/umago.db`, so set it to somewhere writable.

## Track a site

Paste the snippet, then visit the site. That is the whole integration.

**Custom events** from your page, with properties that are stored and breakable down per event:

```js
umago('signup')
umago('download', { plan: 'pro', seats: 3 })
```

**Declarative clicks** without JavaScript: any element with `data-umago-event` fires on click, with `data-umago-event-<key>` attributes as properties:

```html
<button data-umago-event="signup" data-umago-event-plan="pro">Sign up</button>
```

`data-umami-event` / `data-umami-event-<key>` are still accepted as aliases, so markup copied from Umami keeps working. Where both are present on the same element, `data-umago-event*` wins.

**Logged-in users**: `umago.identify('user-123')` tags this and later hits with your own user id (a `distinct_id` dimension, never a cookie).

**Web vitals**: add `data-performance` and the snippet reports LCP/INP/CLS/FCP/TTFB for a Vitals card on the dashboard.

**Snippet options**: `data-domains="example.com,app.example.com"` limits the snippet to hosts, `data-exclude-search` and `data-exclude-hash` strip query/hash from URLs, `data-do-not-track` honours browser DNT (the server also drops `DNT: 1` hits), `data-auto-track="false"` disables automatic tracking while keeping `umago()` calls, and `data-auto-pageview="false"` disables just the automatic pageviews. `data-before-send="fn"` names a `window` function that can edit or cancel each payload.

**Single-page apps** are handled: the snippet re-sends on `pushState` and `popstate`.

**Testing locally.** Pages served from `localhost`, `127.0.0.1`, `*.localhost`, `*.local`, `*.test` or a private LAN address are accepted for every site, so you can try the snippet on a dev server before deploying. Anything else must match the site's domain or a subdomain of it. Run with `UMAGO_LOG_LEVEL=debug` to see why an event was dropped.

**What is dropped.** Known bot user agents (anything with `bot`, `crawl`, `spider`, `headless`, `curl`, `wget` and friends), browsers with `navigator.webdriver` set, `DNT: 1` requests, events whose page host does not match the site, and bodies over 64 KB (4 KB per event in batch mode). The endpoint always answers 202 so it cannot be used to probe which site ids exist.

## Dashboard

The index lists every site with its favicon, a 14-day sparkline and this week's visitors against last week's. Drag the grip to reorder.

Per site:

- **Visitors, visits, page views, views per visitor, bounce rate and average visit duration** with the change versus the previous equal window, over 24h, 48h, 7d, 30d, 90d, 180d or a custom range. **Compare** overlays the previous window on the chart.
- **Chart**: hourly for short ranges, daily for long ones, with the dark tooltip on hover.
- **People in the last 30 minutes** with a per-minute strip. The Pages card shows who is online right now: `Pages · N online` in its title and per-page live visitors and views, refreshed every five seconds.
- **Tabbed cards**: Pages with Entry and Exit; Sources as Referrer, Channel (Direct, Organic Search, Paid Ads, Social, Email, LLM, Referral), Source (`utm_source`, falling back to `?ref=` and `?source=`), Medium, Campaign, Content or Term; Paid ads as `gclid`, `fbclid`, `msclkid`, `ttclid`, `li_fat_id` or `twclid` click ids; Locations as Countries or Regions; Devices as Browsers, OS, Devices or Languages; titles and hosts; logged-in users; Events with per-event property breakdowns. The expand icon beside a title opens the full list with a filter box. Bars grow in when a tab changes.
- **Sessions**: recent visits with length, pages, events and entry → exit.
- **Web vitals**: LCP/INP/CLS/FCP/TTFB percentiles and slowest paths, once the snippet runs with `data-performance`.
- **Funnels**: saved page/event step sequences with per-step conversion, evaluated from raw events.
- **Journeys**: the paths visits actually take: top page sequences with share of visits, top pages per step, and top step-to-step flows. Pick 2-7 steps per visit and optionally keep only visits starting at one entry page. Reads raw events, so it reaches back exactly as far as retention.
- **Live**: a 3D globe of visitors from the last five minutes, dots sized by count, arcs flowing from each country to your home country, refreshed every five seconds. Switch to the range map for the whole window.
- **Settings** per site: name, domain, default date range, accent colour, home country, owning team, refetch favicon, short links and pixels, public share links, and data reset. The default range is the one the dashboard opens on, and the accent replaces the account-wide colour while you are looking at that site.

## How it works

**Visitors and visits.** There are no cookies and no stored IPs. A visitor is `sha256(daily salt + site + IP + user agent)`, truncated to 16 hex characters. The salt rotates every UTC day and is persisted, so a restart does not split a day and nobody can be followed from one day to the next. A day's visitor count is exact; multi-day totals are the sum of daily uniques, the same convention Plausible uses. A visit is a burst of hits with gaps under 30 minutes; bounces are visits with exactly one page view and no custom events, and duration sums each visit's first-to-last hit.

**Country and region.** A proxy country header (`CF-IPCountry`, `X-Vercel-IP-Country`, `X-Country-Code`, `X-Geo-Country`, `CloudFront-Viewer-Country`) is used when present. Otherwise the visitor's browser time zone is mapped to a country with a table generated from the IANA zone file. "Regions" are the city part of that time zone (`Europe/London` → London), the only sub-country signal available without a GeoIP database, so there are no cities.

**GeoIP (optional, off by default).** Settings → Location offers `off`, `country` and `city` detail from an IP database: DB-IP Lite (free download, no key) or MaxMind GeoLite2 (free license key). The lookup order per hit is proxy header, then the database, then the time zone; `city` additionally resolves the city into the region. The file is stored in the database itself (memory-mapped at load, so RAM barely moves) and refreshes from the dashboard. Or point `UMAGO_GEOIP_PATH` at an `.mmdb` on disk to use a file instead. IPs are still never stored, only the resolved country and city. Note the vendors' attribution: DB-IP data is CC BY 4.0, and MaxMind asks for a GeoLite2 credit note.

**Browser, OS, device.** A small ordered user-agent matcher (Edge before Chrome, Chrome before Safari, `CriOS` and `FxiOS` on iOS) plus the screen width the snippet sends. No external database.

**Referrers and campaigns.** Host only, same-site traffic counts as direct, and common hosts are aliased (`t.co` → `x.com`, `www.google.com` → `google.com`, and so on). The full `utm_source/medium/campaign/content/term` set is stored, plus paid ad click ids (`gclid`, `fbclid`, `msclkid`, `ttclid`, `li_fat_id`, `twclid`), and every hit is bucketed into a channel: Direct, Organic Search, Paid Ads, Social, Email, LLM or Referral. Any click id, or a paid-looking medium (`cpc`, `ppc`, `paid`, `retargeting` and friends), forces Paid Ads.

**Rollups.** `hourly_stats` holds page views and distinct visitors per hour. `daily_stats` holds, per day, a total plus every dimension (page, entry, exit, channel, referrer, country, region, language, device, browser, OS, title, host, event, all five UTM tags, all six ad click ids, logged-in user), capped at 500 keys per dimension per day with the rest folded into "Other". `visit_stats` holds per-day visits, bounces and visit seconds. Raw events are pruned after the retention period; two days is the floor because today and yesterday are rebuilt from them. Event properties, sessions, funnels, journeys and vitals read raw events directly, so they reach back exactly as far as retention.

**Favicons.** Umago fetches your site's icon from the site itself (declared `<link rel="icon">`, then `/favicon.ico`) and caches referrer icons the same way, so the dashboard never asks Google or a CDN. Every fetch goes through a guard that refuses loopback, private, link-local and carrier-grade NAT addresses, including after redirects, and dials the checked IP rather than the name.

**Map.** Country outlines come from Natural Earth (public domain) bundled into the UI, rendered by MapLibre with no tiles and no attribution requirement. Antarctica is left out and rings that cross the antimeridian are unwrapped so nothing fills the canvas. MapLibre loads in its own chunk after the dashboard's first paint.

**Icons.** Browser and OS marks come from [SVGL](https://svgl.app), downloaded once and bundled. Devices are small stroke glyphs. Flags are emoji; on systems that do not render them the country name still shows.

## Settings

The gear in the header opens `/settings`:

- **Overview**: sites, raw events, rollup rows, database size, uptime.
- **Appearance**: accent colour (four design swatches or any hex; it recolours the wordmark, links, chart, bars, arcs and map in both themes) and the title. Both are public so the login screen matches. Light/dark follows the OS automatically; there is no manual switch.
- **MCP**: the endpoint URL, an on/off switch, and API tokens. Minting shows the secret once with a ready-to-paste config block; the list shows name, prefix, created and last used, with revoke.
- **API keys**: full-access keys (`umago_key_…`) for scripts and CI, with read-only scope, rotate-in-place and revoke. Unlike MCP tokens these may write.
- **Security**: two-factor authentication for your own account (authenticator app plus one-time backup codes).
- **Users** (admins): accounts with admin/member/viewer roles. The environment login is bootstrapped as the first admin.
- **Teams** (admins): groups whose members share pinned sites; unpinned sites stay visible to everyone signed in. Assign a site to a team from its settings.
- **Retention**: 2 to 90 days for raw events, unless `UMAGO_RETENTION_DAYS` pins it.
- **Location** (admins): optional GeoIP detail (`off`/`country`/`city`), database source (DB-IP Lite or MaxMind GeoLite2 with key), refresh-now, `.mmdb` upload, and stored-database status, plus a status line spelling out whether lookups are actually active. The Overview metrics show the effective mode and the stored size.
- **Data**: export download, events written since start, and the dropped count if the queue ever overflowed.

## Access

Set `UMAGO_ADMIN_USER` and `UMAGO_ADMIN_PASSWORD` and the UI shows a sign-in screen; that login becomes the first admin account. Admins invite members (site managers) and viewers (read-only) in **Settings → Users**, group them in **Teams**, and pin sites to a team from the site's settings. Sessions last 30 days, survive restarts, and are invalidated when the password changes. TOTP two-factor lives in **Settings → Security**. Leave both env vars unset and everything is open. Only do that behind your own proxy, Tailscale or VPN.

**Sharing.** Each site's settings can mint public share links: read-only dashboard URLs (`/share/…`) showing traffic with no login and no settings. Revoking the link kills it.

## MCP (for AI agents)

Umago speaks the [Model Context Protocol](https://modelcontextprotocol.io) at `/mcp` (Streamable HTTP), read-only. Point the agent you already use at it and ask things like *"how are my sites doing this week?"*, *"did anything spike on example.com in the last month?"* or *"where is example.com's traffic coming from?"*. There is no LLM inside Umago; it serves structured numbers plus small computed signals so the agent does not have to do arithmetic on long series.

| Tool | Returns |
| --- | --- |
| `list_sites` | Every site with visitors this week and last, and visitors online now |
| `overview` | Every site over a range with the change versus the previous window, a rising/falling/flat trend, spike buckets, the peak, and the top page, referrer, country and events. The right first call |
| `site_stats` | Full detail for one site: totals, series, top 10 of every breakdown, and the same signals. Takes `filters` to narrow to matching visitors |
| `breakdown` | The complete list for any dimension, up to 500 rows. Takes the same `filters` |
| `funnel` | A saved funnel evaluated over a range: per-step visitors, rates and conversion |
| `journey` | Top page sequences with visits and share, top pages per step, top step-to-step flows |
| `sessions` | Recent visits with duration, pages, events and entry/exit |
| `vitals` | Web Vitals percentiles and slowest paths, when the site reports them |
| `search_terms` | Google search queries from Search Console with clicks, impressions and position, for connected sites |
| `revenue` | Polar revenue: totals, series, revenue per visitor, attributed versus unattributed orders, and revenue by first-touch referrer, source, campaign, landing page, country and product |

Ranges are `24h`, `48h`, `7d`, `30d`, `90d`, `180d`; words like `week` and `month` are accepted. Custom date windows work through the dashboard and API (`range=custom&from=…&to=…`). Sites resolve by id, name, domain or a fuzzy match. `filters` is a map of dimension to key, such as `{"ref": "google.com", "country": "GB"}`; filtered answers come from raw events and say so when the range was cut to the retention window. The server instructions explain how revenue attribution is collected and why the unattributed bucket exists, so the agent does not mistake pre-attribution orders for direct sales.

Mint a token in **Settings → MCP** (or set `UMAGO_MCP_TOKEN`) and use it as a bearer token. The admin login works there too. Turning the endpoint off in Settings returns `404 mcp_disabled` to everyone.

```bash
claude mcp add --transport http umago https://umago.example.com/mcp --header "Authorization: Bearer umago_tok_…"
```

Any client that supports Streamable HTTP with a custom header can connect the same way.

Things to ask once it is connected:

- *"Give me an overview of all my sites for the last 30 days."*
- *"Which referrer sent the most visitors to example.com this week, and is it growing?"*
- *"Show me example.com's traffic from Germany over the last month."* (uses `filters`)
- *"What did people search on Google to find example.com?"* (needs [Search Console](#google-search-terms))
- *"How much revenue came from Product Hunt visitors?"* (needs [Polar](#revenue-from-polar))

## API

All endpoints are under `/api/v1`. Errors are JSON: `{"error": "code", "message": "..."}`.

| Method | Path | Auth | Purpose |
| --- | --- | --- | --- |
| GET | `/health` | none | `{"status":"ok"}` |
| GET | `/umago.js` (+ every `UMAGO_SCRIPT_NAMES` alias) | none | The tracking snippet, cached for a day |
| POST | `/api/v1/collect` | none | Ingest one event; always 202 |
| POST | `/api/v1/batch` | none | Ingest up to 50 events; always 202 |
| GET | `/l/:slug` | none | Short link: records a click, 302s to the target |
| GET | `/p/:slug` | none | Tracking pixel: records a hit, serves a 1px gif |
| GET | `/api/v1/share/:slug` | none | Public dashboard data for a share link |
| GET | `/api/v1/theme` | none | `{accent, title}` for the UI |
| GET/POST | `/api/v1/sites` | admin | List (with 7-day card and live count) / create `{name?, domain}` |
| GET/PATCH/DELETE | `/api/v1/sites/:id` | admin | Manage `{name?, domain?, home_country?, owner_team_id?}` |
| POST | `/api/v1/sites/reorder` | admin | `{ids}` in display order |
| POST | `/api/v1/sites/:id/reset` | admin | Delete the site's events and rollups, keep the site |
| GET | `/api/v1/sites/:id/stats?range=` | admin | Totals, previous window, series and top-10 breakdowns. `range=custom&from=&to=` for windows, `compare=prev` for the overlay |
| GET | `/api/v1/sites/:id/breakdown?dim=&range=&limit=` | admin | Full list for one dimension, up to 500 rows |
| GET | `/api/v1/sites/:id/event-data?event=&key=` | admin | Custom event property keys, then top values |
| GET | `/api/v1/sites/:id/vitals?range=` | admin | Web Vitals percentiles and slowest paths |
| GET | `/api/v1/sites/:id/sessions?range=` | admin | Recent visits |
| GET/POST | `/api/v1/sites/:id/funnels` | admin | List / create `{name, steps}` funnel definitions |
| PUT/DELETE | `/api/v1/sites/:id/funnels/:fid` | admin | Edit / delete a funnel |
| GET | `/api/v1/sites/:id/funnels/:fid?range=` | admin | Evaluate a funnel: per-step visitors and conversion |
| GET | `/api/v1/sites/:id/journeys?range=&steps=&limit=&start=` | admin | Journey: top page sequences, pages per step, step-to-step flows (`steps` 2-7, `limit` 1-100) |
| GET/POST | `/api/v1/sites/:id/links` | admin | List / create `{slug, url, name?, kind?}` short links and pixels |
| DELETE | `/api/v1/sites/:id/links/:linkID` | admin | Delete a link |
| GET/POST | `/api/v1/sites/:id/shares` | admin | List / mint public share links |
| DELETE | `/api/v1/sites/:id/shares/:sid` | admin | Revoke a share link |
| GET | `/api/v1/sites/:id/live` | admin | Last 5 minutes by country and top live pages, per-minute visitors for the last 30 minutes |
| GET | `/api/v1/sites/:id/favicon` | admin | The stored site icon |
| POST | `/api/v1/sites/:id/refresh-favicon` | admin | Refetch it now |
| GET | `/api/v1/favicon?host=` | admin | A cached referrer icon |
| POST | `/api/v1/rollup` | admin | Flush the queue and rebuild today's rollups now |
| GET | `/api/v1/status` | admin | Counts, database size, uptime, ingest stats |
| GET/PATCH | `/api/v1/settings` | admin | `accent`, `title`, `mcp_enabled`, `retention_days` |
| GET/POST | `/api/v1/tokens` | admin | List / mint `{name}` (returns `secret` once) |
| DELETE | `/api/v1/tokens/:id` | admin | Revoke |
| GET/POST | `/api/v1/keys` | admin | List / mint `{name, scopes?}` full-access keys (returns `secret` once) |
| POST | `/api/v1/keys/:id/rotate` | admin | Replace a key's secret in place (returns `secret` once) |
| DELETE | `/api/v1/keys/:id` | admin | Revoke |
| GET/POST | `/api/v1/users` | admin | List / create `{username, password, role}` accounts |
| PATCH/DELETE | `/api/v1/users/:uid` | admin | Change role/password, delete |
| GET/POST | `/api/v1/teams` | admin | List / create teams |
| PATCH/DELETE | `/api/v1/teams/:tid` | admin | Rename / delete a team |
| GET/POST | `/api/v1/teams/:tid/members` | admin | List / add `{user_id, role}` members |
| DELETE | `/api/v1/teams/:tid/members/:uid` | admin | Remove a member |
| POST | `/api/v1/auth/2fa/verify` | none | Redeem a login challenge `{token, code}` |
| GET/POST | `/api/v1/auth/2fa/status` etc. | admin | TOTP setup, confirm, disable for your own account |
| GET | `/api/v1/export` | admin | Every site and daily rollup as one JSON file |
| GET | `/api/v1/geoip` | admin | GeoIP status: effective mode/source, edition, build date, size, sync state |
| PUT | `/api/v1/geoip` | admin | Set `{mode?, source?, maxmind_key?}` (`off`/`country`/`city`, `dbip`/`maxmind`) |
| POST | `/api/v1/geoip/refresh` | admin | Download the database now; keeps the old one on failure |
| POST | `/api/v1/geoip/upload` | admin | Upload an `.mmdb` file (multipart `file`, up to 300 MB) |
| DELETE | `/api/v1/geoip/data` | admin | Delete the stored database, keep the settings |
| GET | `/api/v1/openapi.json` | none | This API as an OpenAPI 3.0 document |
| GET | `/api/v1/sites/:id/google` | admin | Search Console connection status |
| GET | `/api/v1/sites/:id/google/connect` | admin | Start the Google OAuth flow (302s to Google) |
| PATCH/DELETE | `/api/v1/sites/:id/google` | admin | Pick the property / disconnect (revokes grant, deletes terms) |
| POST | `/api/v1/sites/:id/google/sync` | admin | Pull Search Console data now |
| GET | `/api/v1/sites/:id/search-terms?range=` | admin | Google search terms with clicks, impressions, position |
| GET/PUT/DELETE | `/api/v1/sites/:id/polar` | admin | Polar status / connect `{access_token?, server?, product_ids?, webhook_secret?}` / disconnect |
| POST | `/api/v1/sites/:id/polar/sync` | admin | Reconcile Polar orders now |
| GET | `/api/v1/sites/:id/revenue?range=` | admin | Polar revenue: totals, series, breakdowns |
| POST | `/mcp` | token or admin | Read-only [MCP](#mcp-for-ai-agents) endpoint |

**Admin auth.** Set `UMAGO_ADMIN_USER` and `UMAGO_ADMIN_PASSWORD` and the UI shows a sign-in screen; that login becomes the first admin account, with TOTP two-factor available in Settings. Admin endpoints then need the session cookie it sets (`POST /api/v1/auth/login`, plus `POST /api/v1/auth/2fa/verify` for 2FA accounts) or HTTP Basic credentials (`curl -u user:pass …`, unavailable to 2FA accounts). Sessions last 30 days, survive restarts, and are invalidated when the password changes. Leave both unset and everything is open. Only do that behind your own proxy, Tailscale or VPN.

**API tokens** (`umago_tok_…`) are minted in Settings, stored as hashes, and accepted for `/mcp` and every `GET` admin endpoint. They get `403 read_only` on anything that writes. **API keys** (`umago_key_…`) are full-access: they may write, honour a `read` scope that limits them to `GET`, and rotate in place.

## Configuration

| Variable | Default | Notes |
| --- | --- | --- |
| `UMAGO_PORT` | `8080` | |
| `UMAGO_DATABASE_PATH` | `/data/umago.db` | SQLite file (WAL mode, migrations applied on start). Ignored when `UMAGO_DATABASE_URL` is set |
| `UMAGO_DATABASE_URL` | | Postgres DSN, e.g. `postgres://user:pass@host:5432/umago?sslmode=disable`. When set, Postgres is used instead of SQLite |
| `UMAGO_RETENTION_DAYS` | `7` | Days of raw events to keep, minimum 2. When set it pins the value; leave unset to manage it from Settings. Rollups are kept forever |
| `UMAGO_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. JSON logs on stdout |
| `UMAGO_ADMIN_USER` | | Dashboard username; set together with the password |
| `UMAGO_ADMIN_PASSWORD` | | 8+ characters. Unset = no login |
| `UMAGO_MCP_TOKEN` | | Optional fixed bearer token for the [MCP endpoint](#mcp-for-ai-agents); 16+ characters. Tokens minted in Settings work without it |
| `UMAGO_GOOGLE_CLIENT_ID` | | OAuth client for [Google Search Console](#google-search-terms); set together with the secret |
| `UMAGO_GOOGLE_CLIENT_SECRET` | | |
| `UMAGO_SCRIPT_NAMES` | `umago.js` | Comma-separated top-level `.js` aliases the snippet is served from (e.g. `a.min.js,b.js`). First entry is canonical; every alias serves identical bytes and derives `/api/v1/collect` from its own URL |
| `UMAGO_GEOIP_PATH` | | `.mmdb` file on disk, winning over the database stored from Settings. Either way lookups stay off unless the mode enables them |
| `UMAGO_GEOIP_MODE` | `off` | Pins location detail to `off`, `country` or `city`; overrides Settings |
| `UMAGO_GEOIP_SOURCE` | `dbip` | Pins the vendor to `dbip` or `maxmind`; overrides Settings |
| `UMAGO_MAXMIND_LICENSE_KEY` | | GeoLite2 license key; wins over the stored key |

Umago reads the client IP from `X-Forwarded-For` (or `X-Real-IP`), which Traefik and most proxies set. The IP is only ever hashed.

## Google search terms

Google strips the query from the referrer, so the only way to see which searches bring people in is the Search Console API. Each site's settings panel has a **Connect Google Search Console** button once an OAuth client is configured. Umago pulls the last 16 months on connect and refreshes once a day; the dashboard gets a **Search terms** card and the MCP server a `search_terms` tool. Google's data trails by two to three days.

1. In [Google Cloud](https://console.cloud.google.com/apis/credentials), create a project, enable the **Google Search Console API**, and create an **OAuth client ID** of type *Web application*.
2. Add the redirect URI shown in the site's settings panel, `https://umago.example.com/api/v1/google/callback`. It must match exactly.
3. Set `UMAGO_GOOGLE_CLIENT_ID` and `UMAGO_GOOGLE_CLIENT_SECRET` and restart.
4. On the OAuth consent screen, add your Google account as a test user, or publish the app. While the app is in *Testing*, Google expires the grant after seven days and Umago shows **Reconnect Google**; publishing removes that limit (no verification needed for your own use, you just click through an "unverified app" warning once).
5. Open a site, **Settings**, **Connect Google Search Console**. Umago picks the property matching the domain, preferring a domain property (`sc-domain:`) over URL-prefix ones, and asks you to choose if none matches.

Only the `webmasters.readonly` scope is requested. The refresh token is stored in the database alongside everything else; disconnecting revokes it with Google and deletes the stored terms.

## Filtering

Click any row in Pages, Sources, Locations, Devices or Events to narrow the whole dashboard to the visitors who matched it: the chart, the totals and every other card. Filters stack, sit in the URL so they can be shared, and clear with one click. Because filtered views are built from raw events rather than rollups, they only reach back as far as raw events are kept: with the default 7 days, a 30d range under a filter shows the last week and says so. Raise retention in Settings if you want to filter longer ranges. The MCP `site_stats` and `breakdown` tools take the same `filters`.

## Revenue from Polar

If you sell through [Polar](https://polar.sh), each site can show revenue next to traffic, the way datafa.st does with Stripe. Open a site, **Settings**, **Polar**, and paste an organization access token (Polar dashboard, Settings, Developers; the `orders:read` scope is enough). Umago pulls two years of orders, then reconciles once a day. Add a webhook in Polar pointing at the URL shown in the panel, subscribed to the `order.*` events, and paste its secret so sales appear within seconds. If the organization sells several products, list the product ids that belong to this site so the others are ignored.

The dashboard gets revenue, orders and revenue per visitor tiles, revenue bars behind the traffic chart, and a **Revenue** card broken down by first-touch referrer, source, campaign, landing page, country and product. Revenue is the net amount after discounts and before tax, less refunds, in the currency you charge in. The MCP server gains a `revenue` tool.

### Attributing sales to a source

Polar only knows what your checkout tells it. Add `data-attribution` to the snippet and it remembers each visitor's first referrer and landing URL in their own browser (`localStorage`, never sent to Umago, no cookie):

```html
<script defer src="https://umago.example.com/umago.js" data-site="site_…" data-attribution></script>
```

When you create a Polar checkout, read `umago.attribution()` (it returns `{ r: referrer, l: landing URL, t: timestamp }` or `null`) and pass `attr_ref` and `attr_landing` in the checkout `metadata`. Umago normalises them with the same rules as page views, so "Revenue by source" agrees with "Sources". Orders placed before you wire this up count towards totals but show as unattributed.

## Deploying with Coolify / Dokploy

Both compose samples work as Git-based applications. Pick `docker-compose.sqlite.yml` (bind mount `./data`, or swap it for a named volume since the image runs as uid 1000 and a root-owned bind mount blocks SQLite writes) or `docker-compose.postgres.yml` (named `pgdata` volume, no host files involved).

1. New application from your Git repository, Docker Compose build pack, compose path `docker-compose.sqlite.yml` or `docker-compose.postgres.yml`.
2. Set `UMAGO_ADMIN_USER` and `UMAGO_ADMIN_PASSWORD` (at least 8 characters) in the platform's environment variables. For Postgres also set `POSTGRES_PASSWORD`.
3. Add your domain with HTTPS pointing at port `8080`. For Postgres the `postgres` service needs no domain; it stays on the internal network.
4. Deploy, open the domain, and sign in. Use `https://umago.example.com/umago.js` in your tracking snippet.

`UMAGO_LOG_LEVEL` defaults to `info`. Leave `UMAGO_RETENTION_DAYS` empty to use the dashboard's saved retention setting (7 days initially); setting it explicitly overrides the dashboard. `UMAGO_MCP_TOKEN` is optional and must be at least 16 characters when set. For Google Search Console, set both `UMAGO_GOOGLE_CLIENT_ID` and `UMAGO_GOOGLE_CLIENT_SECRET`, using `https://umago.example.com/api/v1/google/callback` as the OAuth redirect URI.

The existing Dockerfile supplies the `/health` check. Keep the data volume (`./data` or `pgdata`) when redeploying to preserve analytics.

## Footprint

The image is about 30 MB and idles at under 10 MB of memory. Storage is dominated by rollups: one row per site per day per breakdown key, so a site with a hundred pages and a dozen referrers adds a few hundred rows a day. Raw events are the only thing that grows with traffic, and they are pruned.

## Privacy

Umago is designed to avoid personal data rather than to manage it:

- No cookies, no fingerprinting, no cross-day tracking. A visitor is
  `sha256(daily salt + site + IP + user agent)`, truncated to 16 hex
  characters. The salt rotates every UTC day, so nobody can be followed from
  one day to the next. IPs are only ever hashed, never stored.
- `DNT: 1` is honoured by the server, and the snippet supports
  `data-do-not-track`. Known bots, `navigator.webdriver` clients and
  off-domain hosts are dropped.
- No third-party services at runtime: favicons are fetched by Umago itself
  through an SSRF guard, the world map ships its own outlines, and brand
  icons are bundled. The dashboard makes no calls to Google or a CDN.
- Location defaults to proxy country headers plus browser time zone only
  (regions are the city part of the time zone). IP-based GeoIP is off by
  default and opt-in via Settings → Location.
- The only browser storage Umago ever uses is opt-in first-touch attribution
  (`data-attribution`): referrer and landing URL stay in the visitor's own
  `localStorage` and are never sent to Umago. They only reach your server if
  you read `umago.attribution()` and pass it into a Polar checkout.
- Data stays where you put it (SQLite file or your Postgres). Raw events are
  pruned after the retention window (2–90 days, 7 by default); daily and
  hourly rollups are kept forever. You can export everything
  (Settings → Data) or wipe a site (site Settings → data reset).

Operator note: put Umago behind HTTPS, make sure your proxy overwrites
`X-Forwarded-For` rather than appending to it, and add your own privacy
policy / DPA wording for your sites. Umago cannot do that part for you.

## Development

```bash
cd server && UMAGO_DATABASE_PATH=./data/umago.db go run ./cmd/umago   # API on :8080
cd server/web && bun install && bun run dev                           # UI on :5173, proxies /api
cd server && go test ./...                                            # Go tests
cd server/web && bun run check && bun run test                        # web checks + tests
```

Requires Go 1.27 and Bun 1.4. Both database drivers are pure Go, so `CGO_ENABLED=0` builds work everywhere. The Go tests cover ingest through rollup to stats with a fixed clock, day-scoped hashing, the user-agent table, timezone mapping, the SSRF guard, favicon parsing, the MCP tools over a real client, tokens and settings.

## Contributing

Security reports go through [SECURITY.md](SECURITY.md).

## Licence

MIT. See [LICENSE](LICENSE).
