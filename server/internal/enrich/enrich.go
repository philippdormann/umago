// Package enrich derives country, device, browser, OS and referrer from a
// request without any external database or service.
package enrich

import (
	"net/http"
	"net/url"
	"strings"
)

// Country resolves a country code: a proxy/CDN header first, then the
// visitor's IANA time zone. Returns "" when unknown.
func Country(h http.Header, tz string) string {
	if c := HeaderCountry(h); c != "" {
		return c
	}
	if cc, ok := tzCountry[strings.TrimSpace(tz)]; ok {
		return cc
	}
	return ""
}

// HeaderCountry returns the proxy/CDN country header ("CF-IPCountry",
// "X-Vercel-IP-Country", "X-Country-Code", "X-Geo-Country",
// "CloudFront-Viewer-Country"), or "" when absent. Callers use it to decide
// whether a GeoIP lookup is still needed.
func HeaderCountry(h http.Header) string {
	for _, k := range []string{"CF-IPCountry", "X-Vercel-IP-Country", "X-Country-Code", "X-Geo-Country", "CloudFront-Viewer-Country"} {
		if v := strings.ToUpper(strings.TrimSpace(h.Get(k))); len(v) == 2 && v != "XX" && v != "T1" {
			return v
		}
	}
	return ""
}

// Region derives a coarse place name from an IANA time zone: the last path
// segment with underscores as spaces ("America/New_York" -> "New York").
// It is the best location signal available without a GeoIP database.
func Region(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" || !strings.Contains(tz, "/") {
		return ""
	}
	seg := tz[strings.LastIndex(tz, "/")+1:]
	if seg == "" || strings.HasPrefix(seg, "GMT") || strings.HasPrefix(seg, "UTC") || strings.HasPrefix(seg, "Etc") {
		return ""
	}
	return strings.ReplaceAll(seg, "_", " ")
}

// UTM extracts campaign tags from a page URL. source falls back to the
// common ?ref= and ?source= conventions.
func UTM(raw string) (source, campaign string) {
	src, _, camp, _, _ := UTMTags(raw)
	return src, camp
}

// UTMTags extracts the full campaign tag set from a page URL.
func UTMTags(raw string) (source, medium, campaign, content, term string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", "", "", ""
	}
	q := u.Query()
	source = strings.TrimSpace(q.Get("utm_source"))
	if source == "" {
		source = strings.TrimSpace(q.Get("ref"))
	}
	if source == "" {
		source = strings.TrimSpace(q.Get("source"))
	}
	medium = strings.TrimSpace(q.Get("utm_medium"))
	campaign = strings.TrimSpace(q.Get("utm_campaign"))
	content = strings.TrimSpace(q.Get("utm_content"))
	term = strings.TrimSpace(q.Get("utm_term"))
	if len(source) > 80 {
		source = source[:80]
	}
	if len(medium) > 80 {
		medium = medium[:80]
	}
	if len(campaign) > 80 {
		campaign = campaign[:80]
	}
	if len(content) > 80 {
		content = content[:80]
	}
	if len(term) > 80 {
		term = term[:80]
	}
	return strings.ToLower(source), strings.ToLower(medium), campaign, content, term
}

// AdTags holds paid ad click ids from a page URL, mirroring Umami's
// website_event columns (gclid, fbclid, msclkid, ttclid, li_fat_id, twclid).
type AdTags struct {
	Gclid   string
	Fbclid  string
	Msclkid string
	Ttclid  string
	LiFatID string
	Twclid  string
}

// HasAd reports whether any click id is present.
func (a AdTags) HasAd() bool {
	return a.Gclid != "" || a.Fbclid != "" || a.Msclkid != "" || a.Ttclid != "" || a.LiFatID != "" || a.Twclid != ""
}

func truncAd(v string) string {
	v = strings.TrimSpace(v)
	if len(v) > 100 {
		v = v[:100]
	}
	return v
}

// ParseAdTags extracts paid ad click ids from a page URL's query string.
func ParseAdTags(raw string) AdTags {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return AdTags{}
	}
	q := u.Query()
	li := strings.TrimSpace(q.Get("li_fat_id"))
	if li == "" {
		li = strings.TrimSpace(q.Get("lifatid"))
	}
	return AdTags{
		Gclid:   truncAd(q.Get("gclid")),
		Fbclid:  truncAd(q.Get("fbclid")),
		Msclkid: truncAd(q.Get("msclkid")),
		Ttclid:  truncAd(q.Get("ttclid")),
		LiFatID: truncAd(li),
		Twclid:  truncAd(q.Get("twclid")),
	}
}

// Channel groups a visit's acquisition into one bucket: Direct, Organic
// Search, Paid Ads, Social, Email, LLM, or Referral.
func Channel(refHost, source, medium string) string {
	return ChannelWithAds(refHost, source, medium, AdTags{})
}

// ChannelWithAds is Channel with paid ad click ids taken into account: any
// click id (gclid, fbclid, msclkid, ttclid, li_fat_id, twclid) forces Paid
// Ads, as do paid-looking mediums containing cpc, ppc, paid or retargeting.
func ChannelWithAds(refHost, source, medium string, ads AdTags) string {
	if ads.HasAd() {
		return "Paid Ads"
	}
	med := strings.ToLower(strings.TrimSpace(medium))
	switch med {
	case "cpc", "ppc", "paid", "cpm", "banner", "paid-social", "paidsearch", "paid-search", "display":
		return "Paid Ads"
	case "email", "newsletter":
		return "Email"
	case "social", "social-media", "sm":
		return "Social"
	case "organic", "seo":
		return "Organic Search"
	}
	if strings.Contains(med, "paid") || strings.Contains(med, "cpc") || strings.Contains(med, "ppc") || strings.Contains(med, "retargeting") {
		return "Paid Ads"
	}
	src := strings.ToLower(strings.TrimSpace(source))
	if host := strings.ToLower(strings.TrimSpace(refHost)); host != "" {
		if isEmailHost(host) {
			return "Email"
		}
		if isSearchHost(host) {
			return "Organic Search"
		}
		if isSocialHost(host) {
			return "Social"
		}
		if isLLMHost(host) {
			return "LLM"
		}
		return "Referral"
	}
	if src != "" {
		return "Referral"
	}
	return "Direct"
}

func hostIn(host string, list []string) bool {
	for _, h := range list {
		if host == h || strings.HasSuffix(host, "."+h) {
			return true
		}
	}
	return false
}

var searchHosts = []string{"google.com", "bing.com", "duckduckgo.com", "search.yahoo.com", "yahoo.com", "yandex.com", "baidu.com", "ecosia.org", "startpage.com", "brave.com", "kagi.com", "mojeek.com", "qwant.com", "swisscows.com", "ask.com", "aol.com", "seznam.cz", "naver.com", "daum.net"}

var socialHosts = []string{"facebook.com", "x.com", "twitter.com", "instagram.com", "linkedin.com", "reddit.com", "tiktok.com", "youtube.com", "pinterest.com", "snapchat.com", "threads.net", "mastodon.social", "bsky.app", "bluesky.com", "tumblr.com", "whatsapp.com", "telegram.me", "t.me", "discord.com", "slack.com", "medium.com", "substack.com", "news.ycombinator.com", "lobste.rs", "producthunt.com", "indiehackers.com", "github.com"}

var llmHosts = []string{"chatgpt.com", "openai.com", "claude.ai", "anthropic.com", "perplexity.ai", "copilot.microsoft.com", "gemini.google.com", "bard.google.com", "chat.deepseek.com", "grok.com", "x.ai", "mistral.ai", "you.com", "phind.com", "komo.ai"}

var emailHosts = []string{"gmail.com", "mail.google.com", "outlook.com", "outlook.live.com", "hotmail.com", "live.com", "yahoo.com", "mail.yahoo.com", "proton.me", "protonmail.com", "icloud.com", "mail.aol.com", "zoho.com", "gmx.com", "hey.com"}

func isSearchHost(host string) bool { return hostIn(host, searchHosts) }
func isSocialHost(host string) bool { return hostIn(host, socialHosts) }
func isLLMHost(host string) bool    { return hostIn(host, llmHosts) }
func isEmailHost(host string) bool  { return hostIn(host, emailHosts) }

// ReferrerAliases turns well-known hosts into the names people expect.
var ReferrerAliases = map[string]string{
	"t.co": "x.com", "twitter.com": "x.com", "l.facebook.com": "facebook.com", "lm.facebook.com": "facebook.com",
	"m.facebook.com": "facebook.com", "com.google.android.gm": "gmail.com", "android-app://com.google.android.googlequicksearchbox": "google.com",
	"www.google.com": "google.com", "www.bing.com": "bing.com", "duckduckgo.com": "duckduckgo.com", "com.linkedin.android": "linkedin.com",
	"www.linkedin.com": "linkedin.com", "old.reddit.com": "reddit.com", "www.reddit.com": "reddit.com", "out.reddit.com": "reddit.com",
	"news.ycombinator.com": "news.ycombinator.com", "www.youtube.com": "youtube.com", "m.youtube.com": "youtube.com",
}

// Referrer returns the referrer host, or "" for direct or same-site traffic.
func Referrer(ref, siteDomain string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if a, ok := ReferrerAliases[host]; ok {
		host = a
	}
	host = strings.TrimPrefix(host, "www.")
	if SameSite(host, siteDomain) {
		return ""
	}
	return host
}

// LocalHost reports whether host is a local development address, which is
// accepted for every site so the snippet can be tested before deploying.
func LocalHost(host string) bool {
	host = strings.ToLower(host)
	return host == "" || host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".test") ||
		strings.HasPrefix(host, "192.168.") || strings.HasPrefix(host, "10.")
}

// SameSite reports whether host is the site domain or one of its subdomains.
func SameSite(host, domain string) bool {
	host = strings.ToLower(strings.TrimPrefix(host, "www."))
	domain = strings.ToLower(strings.TrimPrefix(domain, "www."))
	return host == domain || strings.HasSuffix(host, "."+domain)
}

// Path extracts a normalised path from a page URL: no query, no fragment,
// trailing slash trimmed, capped in length.
func Path(raw string) (path, host string) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "/", ""
	}
	p := u.Path
	if p == "" {
		p = "/"
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
		if p == "" {
			p = "/"
		}
	}
	if len(p) > 200 {
		p = p[:200]
	}
	return p, strings.ToLower(u.Hostname())
}
