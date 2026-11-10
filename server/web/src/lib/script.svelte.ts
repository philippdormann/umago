// Canonical tracking snippet name: first entry of UMAGO_SCRIPT_NAMES,
// served by GET /api/v1/theme as script_name. Defaults to umago.js until
// the theme loads so the UI never blocks on it.
let name = $state('umago.js')
let loaded = false
let inflight: Promise<string> | null = null

export function scriptName(): string {
  return name
}

function valid(v: string): boolean {
  return !!v && v.endsWith('.js') && !v.includes('/') && !v.includes('\\')
}

export function setScriptName(v: string) {
  if (valid(v)) name = v
  loaded = true
}

export function loadScriptName(): Promise<string> {
  if (loaded) return Promise.resolve(name)
  if (!inflight) {
    inflight = fetch('/api/v1/theme')
      .then((r) => (r.ok ? r.json() : null))
      .then((t) => {
        if (t && valid(t.script_name)) name = t.script_name
        loaded = true
        return name
      })
      .catch(() => {
        loaded = true
        return name
      })
      .finally(() => {
        inflight = null
      })
  }
  return inflight
}

/** Full snippet tag for a site, using the canonical script path. */
export function snippetFor(siteID: string): string {
  const src = `${location.origin}/${name}`
  return `<script defer src="${src}" data-site="${siteID}"><\/script>`
}
