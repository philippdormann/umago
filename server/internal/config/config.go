// Package config reads Umago's configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config is the full server configuration.
type Config struct {
	Port         int
	DatabasePath string
	// DatabaseURL, when set, selects Postgres (e.g.
	// postgres://user:pass@host:5432/umago?sslmode=disable) and
	// DatabasePath is ignored.
	DatabaseURL string
	LogLevel    string

	// Days of raw events to keep. Rollups are kept forever; raw rows only
	// feed today's and yesterday's rebuild, so two days is the floor.
	RetentionDays int
	// RetentionDaysSet is true when UMAGO_RETENTION_DAYS was given; the
	// value then overrides whatever was saved from the settings page.
	RetentionDaysSet bool

	// Admin login for the dashboard and admin API. Both must be set to enable it.
	AdminUser     string
	AdminPassword string

	// MCPToken, when set, is a bearer token that grants read-only access to
	// the MCP endpoint (/mcp). The admin login works there too.
	MCPToken string

	// GoogleClientID and GoogleClientSecret are an OAuth client from Google
	// Cloud. Both must be set to offer "Connect Google Search Console".
	GoogleClientID     string
	GoogleClientSecret string

	// ScriptNames are the paths (relative to the site root, e.g.
	// "umago.js,a.min.js") the tracking snippet is served from. The
	// first entry is the canonical one shown in the dashboard snippet.
	// All entries serve the same bytes so adblocker-evasive aliases can
	// be rotated without touching the collect endpoint.
	ScriptNames []string

	// GeoIPPath, when set, points at an .mmdb file on disk and wins over
	// the database stored from the dashboard. Either way lookups stay off
	// unless the mode (below) enables them.
	GeoIPPath string
	// GeoIPMode pins the location detail: off (default), country or city.
	// When set it overrides the dashboard value.
	GeoIPMode    string
	GeoIPModeSet bool
	// GeoIPSource pins the database vendor: dbip or maxmind.
	GeoIPSource    string
	GeoIPSourceSet bool
	// MaxMindKey is the GeoLite2 license key; it wins over the stored key.
	MaxMindKey string
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	c := Config{
		Port:          8080,
		DatabasePath:  env("UMAGO_DATABASE_PATH", "/data/umago.db"),
		DatabaseURL:   strings.TrimSpace(env("UMAGO_DATABASE_URL", "")),
		LogLevel:      env("UMAGO_LOG_LEVEL", "info"),
		RetentionDays: 7,
		AdminUser:     env("UMAGO_ADMIN_USER", ""),
		AdminPassword: env("UMAGO_ADMIN_PASSWORD", ""),
		MCPToken:      strings.TrimSpace(env("UMAGO_MCP_TOKEN", "")),

		GoogleClientID:     strings.TrimSpace(env("UMAGO_GOOGLE_CLIENT_ID", "")),
		GoogleClientSecret: strings.TrimSpace(env("UMAGO_GOOGLE_CLIENT_SECRET", "")),
		ScriptNames:        []string{"umago.js"},
	}
	if v := os.Getenv("UMAGO_PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 || p > 65535 {
			return c, fmt.Errorf("UMAGO_PORT must be a port number, got %q", v)
		}
		c.Port = p
	}
	if v := os.Getenv("UMAGO_RETENTION_DAYS"); v != "" {
		d, err := strconv.Atoi(v)
		if err != nil || d < 2 {
			return c, fmt.Errorf("UMAGO_RETENTION_DAYS must be an integer of at least 2, got %q", v)
		}
		c.RetentionDays = d
		c.RetentionDaysSet = true
	}
	if (c.AdminUser == "") != (c.AdminPassword == "") {
		return c, fmt.Errorf("UMAGO_ADMIN_USER and UMAGO_ADMIN_PASSWORD must be set together")
	}
	if c.AdminPassword != "" && len(c.AdminPassword) < 8 {
		return c, fmt.Errorf("UMAGO_ADMIN_PASSWORD must be at least 8 characters")
	}
	if c.MCPToken != "" && len(c.MCPToken) < 16 {
		return c, fmt.Errorf("UMAGO_MCP_TOKEN must be at least 16 characters")
	}
	if (c.GoogleClientID == "") != (c.GoogleClientSecret == "") {
		return c, fmt.Errorf("UMAGO_GOOGLE_CLIENT_ID and UMAGO_GOOGLE_CLIENT_SECRET must be set together")
	}
	if v := strings.TrimSpace(os.Getenv("UMAGO_SCRIPT_NAMES")); v != "" {
		names, err := ParseScriptNames(v)
		if err != nil {
			return c, err
		}
		c.ScriptNames = names
	}
	c.GeoIPPath = strings.TrimSpace(os.Getenv("UMAGO_GEOIP_PATH"))
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("UMAGO_GEOIP_MODE"))); v != "" {
		if v != "off" && v != "country" && v != "city" {
			return c, fmt.Errorf("UMAGO_GEOIP_MODE must be off, country or city, got %q", v)
		}
		c.GeoIPMode, c.GeoIPModeSet = v, true
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("UMAGO_GEOIP_SOURCE"))); v != "" {
		if v != "dbip" && v != "maxmind" {
			return c, fmt.Errorf("UMAGO_GEOIP_SOURCE must be dbip or maxmind, got %q", v)
		}
		c.GeoIPSource, c.GeoIPSourceSet = v, true
	}
	c.MaxMindKey = strings.TrimSpace(os.Getenv("UMAGO_MAXMIND_LICENSE_KEY"))
	return c, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// ParseScriptNames parses a comma-separated UMAGO_SCRIPT_NAMES value into
// clean root-relative script paths. Entries may be given with or without a
// leading slash ("a.min.js, /b.js"); sub-paths are rejected so every alias
// stays a top-level file that cannot shadow /api, /l, /p or /share routes.
func ParseScriptNames(v string) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		n := strings.TrimSpace(part)
		n = strings.TrimPrefix(n, "/")
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		if strings.Contains(n, "/") || strings.Contains(n, "\\") || n == "." || n == ".." {
			return nil, fmt.Errorf("UMAGO_SCRIPT_NAMES must be comma-separated top-level .js files, got %q", part)
		}
		if !strings.HasSuffix(strings.ToLower(n), ".js") {
			return nil, fmt.Errorf("UMAGO_SCRIPT_NAMES must be comma-separated .js files, got %q", part)
		}
		for _, r := range n {
			if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '.' && r != '-' && r != '_' {
				return nil, fmt.Errorf("UMAGO_SCRIPT_NAMES must only contain [A-Za-z0-9._-], got %q", part)
			}
		}
		if !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil, fmt.Errorf("UMAGO_SCRIPT_NAMES must list at least one .js file")
	}
	return names, nil
}
