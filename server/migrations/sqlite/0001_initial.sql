CREATE TABLE sites (
    id             TEXT PRIMARY KEY,
    name           TEXT NOT NULL,
    domain         TEXT NOT NULL UNIQUE,
    home_country   TEXT NOT NULL DEFAULT '',   -- ISO 3166-1 alpha-2; arcs converge here
    favicon        BLOB,
    favicon_type   TEXT NOT NULL DEFAULT '',
    favicon_at     TEXT NOT NULL DEFAULT '',
    position       INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL,
    accent         TEXT NOT NULL DEFAULT '',
    default_range  TEXT NOT NULL DEFAULT '',
    owner_team_id  TEXT NOT NULL DEFAULT ''
);

-- Raw events. Short retention; rollups are the source of truth for the UI.
CREATE TABLE events (
    id         INTEGER PRIMARY KEY,
    site_id    TEXT NOT NULL,
    ts         TEXT NOT NULL,
    kind       TEXT NOT NULL,              -- pageview | event
    name       TEXT NOT NULL DEFAULT '',   -- custom event name
    path       TEXT NOT NULL DEFAULT '',
    ref_host   TEXT NOT NULL DEFAULT '',   -- '' = direct
    country    TEXT NOT NULL DEFAULT '',   -- ISO alpha-2, '' = unknown
    device     TEXT NOT NULL DEFAULT '',
    browser    TEXT NOT NULL DEFAULT '',
    os         TEXT NOT NULL DEFAULT '',
    visitor    TEXT NOT NULL,               -- day-scoped hash; never an IP
    region     TEXT NOT NULL DEFAULT '',   -- time-zone city, e.g. "London"
    utm_source TEXT NOT NULL DEFAULT '',
    utm_campaign TEXT NOT NULL DEFAULT '',
    visit_id   TEXT NOT NULL DEFAULT '',
    distinct_id TEXT NOT NULL DEFAULT '',
    utm_medium TEXT NOT NULL DEFAULT '',
    utm_content TEXT NOT NULL DEFAULT '',
    utm_term   TEXT NOT NULL DEFAULT '',
    channel    TEXT NOT NULL DEFAULT '',
    language   TEXT NOT NULL DEFAULT '',
    title      TEXT NOT NULL DEFAULT '',
    hostname   TEXT NOT NULL DEFAULT '',
    lcp        REAL,
    inp        REAL,
    cls        REAL,
    fcp        REAL,
    ttfb       REAL,
    tag        TEXT NOT NULL DEFAULT '',
    gclid      TEXT NOT NULL DEFAULT '',
    fbclid     TEXT NOT NULL DEFAULT '',
    msclkid    TEXT NOT NULL DEFAULT '',
    ttclid     TEXT NOT NULL DEFAULT '',
    li_fat_id  TEXT NOT NULL DEFAULT '',
    twclid     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX events_site_ts ON events(site_id, ts);
CREATE INDEX events_site_visit ON events(site_id, visit_id);

CREATE TABLE hourly_stats (
    site_id   TEXT NOT NULL,
    hour      TEXT NOT NULL,             -- YYYY-MM-DDTHH (UTC)
    pageviews INTEGER NOT NULL DEFAULT 0,
    visitors  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (site_id, hour)
);

CREATE TABLE daily_stats (
    site_id   TEXT NOT NULL,
    day       TEXT NOT NULL,             -- YYYY-MM-DD (UTC)
    dim       TEXT NOT NULL,             -- total | page | ref | country | device | browser | os | event
    key       TEXT NOT NULL,
    pageviews INTEGER NOT NULL DEFAULT 0,
    visitors  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (site_id, day, dim, key)
);

-- Cached referrer favicons.
CREATE TABLE favicons (
    host       TEXT PRIMARY KEY,
    data       BLOB,
    type       TEXT NOT NULL DEFAULT '',
    fetched_at TEXT NOT NULL,
    failed     INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash    TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    expires_at    TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    user_id       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,             -- first characters, for display
    created_at   TEXT NOT NULL,
    last_used_at TEXT NOT NULL DEFAULT ''
);

CREATE TABLE google_connections (
    site_id       TEXT PRIMARY KEY,
    property      TEXT NOT NULL DEFAULT '',   -- Search Console siteUrl, e.g. sc-domain:example.com
    email         TEXT NOT NULL DEFAULT '',
    refresh_token TEXT NOT NULL,
    connected_at  TEXT NOT NULL,
    synced_at     TEXT NOT NULL DEFAULT '',
    sync_error    TEXT NOT NULL DEFAULT ''
);

CREATE TABLE search_terms (
    site_id     TEXT NOT NULL,
    day         TEXT NOT NULL,               -- YYYY-MM-DD
    query       TEXT NOT NULL,
    clicks      INTEGER NOT NULL,
    impressions INTEGER NOT NULL,
    position    REAL NOT NULL,
    PRIMARY KEY (site_id, day, query)
);

CREATE TABLE polar_connections (
    site_id        TEXT PRIMARY KEY,
    access_token   TEXT NOT NULL,
    server         TEXT NOT NULL DEFAULT 'https://api.polar.sh',
    product_ids    TEXT NOT NULL DEFAULT '',   -- comma separated; empty = every product
    webhook_secret TEXT NOT NULL DEFAULT '',
    connected_at   TEXT NOT NULL,
    synced_at      TEXT NOT NULL DEFAULT '',
    sync_error     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE polar_orders (
    site_id         TEXT NOT NULL,
    order_id        TEXT NOT NULL,
    created_at      TEXT NOT NULL,             -- RFC 3339 UTC
    status          TEXT NOT NULL,
    paid            INTEGER NOT NULL,
    net_amount      INTEGER NOT NULL,          -- cents, after discounts, before tax
    refunded_amount INTEGER NOT NULL,          -- cents
    currency        TEXT NOT NULL,
    country         TEXT NOT NULL DEFAULT '',
    product         TEXT NOT NULL DEFAULT '',
    ref             TEXT NOT NULL DEFAULT '',  -- first-touch referrer host
    source          TEXT NOT NULL DEFAULT '',  -- first-touch utm_source / ?ref=
    campaign        TEXT NOT NULL DEFAULT '',
    landing         TEXT NOT NULL DEFAULT '',  -- first-touch landing path
    PRIMARY KEY (site_id, order_id)
);
CREATE INDEX polar_orders_site_created ON polar_orders (site_id, created_at);

-- Custom event properties, flattened to key/value rows.
CREATE TABLE event_data (
    id       TEXT PRIMARY KEY,
    site_id  TEXT NOT NULL,
    event_id INTEGER NOT NULL,
    key      TEXT NOT NULL,
    sval     TEXT NOT NULL DEFAULT '',
    nval     REAL
);
CREATE INDEX event_data_site_key ON event_data(site_id, key);

-- Visit assignment state: the current open visit per day-scoped visitor.
CREATE TABLE visits (
    site_id    TEXT NOT NULL,
    visitor    TEXT NOT NULL,
    visit_id   TEXT NOT NULL,
    started_at TEXT NOT NULL,
    last_at    TEXT NOT NULL,
    PRIMARY KEY (site_id, visitor)
);

-- Per-day visit rollups (bounces, total visit duration in seconds).
CREATE TABLE visit_stats (
    site_id  TEXT NOT NULL,
    day      TEXT NOT NULL,
    visits   INTEGER NOT NULL DEFAULT 0,
    bounces  INTEGER NOT NULL DEFAULT 0,
    duration INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (site_id, day)
);

-- Short links and tracking pixels. Hits are recorded as events.
CREATE TABLE links (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    name       TEXT NOT NULL DEFAULT '',
    url        TEXT NOT NULL,
    kind       TEXT NOT NULL DEFAULT 'link',
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX links_site ON links(site_id);

-- Saved funnel definitions: steps JSON [{type, value}].
CREATE TABLE funnels (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    steps      TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX funnels_site ON funnels(site_id);

-- Users, teams, sharing, API keys, 2FA.
CREATE TABLE users (
    id           TEXT PRIMARY KEY,
    username     TEXT NOT NULL UNIQUE,
    pass_hash    TEXT NOT NULL,
    role         TEXT NOT NULL DEFAULT 'admin',
    totp_secret  TEXT NOT NULL DEFAULT '',
    totp_enabled INTEGER NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
);

CREATE TABLE backup_codes (
    id         TEXT PRIMARY KEY,
    user_id    TEXT NOT NULL,
    code_hash  TEXT NOT NULL,
    used       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX backup_codes_user ON backup_codes(user_id);

CREATE TABLE teams (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE team_members (
    team_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    role    TEXT NOT NULL DEFAULT 'member',
    PRIMARY KEY (team_id, user_id)
);

CREATE TABLE shares (
    id         TEXT PRIMARY KEY,
    site_id    TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    created_by TEXT NOT NULL DEFAULT ''
);
CREATE INDEX shares_site ON shares(site_id);

-- Full-access API keys (unlike the read-only MCP tokens).
CREATE TABLE api_keys (
    id           TEXT PRIMARY KEY,
    name         TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    prefix       TEXT NOT NULL,
    scopes       TEXT NOT NULL DEFAULT 'read,write',
    expires_at   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    last_used_at TEXT NOT NULL DEFAULT '',
    rotated_at   TEXT NOT NULL DEFAULT ''
);

-- Optional GeoIP location: mode/source/key plus the downloaded MMDB.
-- The database stays off unless the mode is country or city.
CREATE TABLE geoip (
    id          TEXT PRIMARY KEY,            -- always 'geoip'
    mode        TEXT NOT NULL DEFAULT 'off', -- off | country | city
    source      TEXT NOT NULL DEFAULT 'dbip',-- dbip | maxmind
    maxmind_key TEXT NOT NULL DEFAULT '',    -- GeoLite2 license key (maxmind source)
    edition     TEXT NOT NULL DEFAULT '',    -- MMDB database type, e.g. GeoLite2-City
    build_epoch INTEGER NOT NULL DEFAULT 0,  -- MMDB build timestamp (unix seconds)
    sha256      TEXT NOT NULL DEFAULT '',    -- hex of the stored file
    size_bytes  INTEGER NOT NULL DEFAULT 0,
    sync_error  TEXT NOT NULL DEFAULT '',
    synced_at   TEXT NOT NULL DEFAULT '',
    updated_at  TEXT NOT NULL DEFAULT '',
    data        BLOB                         -- the .mmdb file, NULL when none stored
);
