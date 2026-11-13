export type RobotsRule = { allow: boolean; pattern: string }
export type RobotsGroup = { agents: string[]; rules: RobotsRule[] }
export type RobotsFile = { groups: RobotsGroup[]; sitemaps: string[]; parserVersion: string }

export const ROBOTS_PARSER_VERSION = 'robots-v1'
export const CRAWLER_USER_AGENT = 'GEO-Intelligence-Crawler/1.0'

/**
 * Parses robots.txt into agent groups. Consecutive User-agent lines share one group; a
 * User-agent line after a rule starts a new group. Comments and unknown directives are ignored.
 */
export function parseRobots(text: string): RobotsFile {
  const groups: RobotsGroup[] = []
  const sitemaps: string[] = []
  let current: RobotsGroup | null = null
  let lastLineWasAgent = false
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.replace(/#.*$/, '').trim()
    if (!line) continue
    const separator = line.indexOf(':')
    if (separator < 0) continue
    const key = line.slice(0, separator).trim().toLowerCase()
    const value = line.slice(separator + 1).trim()
    if (key === 'user-agent') {
      if (!current || !lastLineWasAgent) {
        current = { agents: [], rules: [] }
        groups.push(current)
      }
      if (value) current.agents.push(value.toLowerCase())
      lastLineWasAgent = true
      continue
    }
    lastLineWasAgent = false
    if (key === 'sitemap') {
      if (value) sitemaps.push(value)
      continue
    }
    if (!current || (key !== 'allow' && key !== 'disallow')) continue
    if (value === '') continue
    current.rules.push({ allow: key === 'allow', pattern: value })
  }
  return { groups, sitemaps, parserVersion: ROBOTS_PARSER_VERSION }
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

function patternToRegExp(pattern: string): RegExp {
  const anchored = pattern.endsWith('$')
  const body = anchored ? pattern.slice(0, -1) : pattern
  const source = body.split('*').map(escapeRegExp).join('.*')
  return new RegExp(`^${source}${anchored ? '$' : ''}`)
}

function agentMatches(agent: string, userAgent: string): boolean {
  if (agent === '*') return true
  return userAgent.toLowerCase().includes(agent)
}

export type RobotsDecision = { allowed: boolean; matchedPattern: string | null; specificity: number }

/**
 * Evaluates the longest matching rule for the request path, including its query string.
 * Allow wins a tie. No matching rule means the path is allowed.
 */
export function evaluateRobots(file: RobotsFile, path: string, userAgent: string = CRAWLER_USER_AGENT): RobotsDecision {
  const agent = userAgent.toLowerCase()
  const matching = file.groups.filter((group) => group.agents.some((value) => agentMatches(value, agent)))
  // RFC 9309: when a group names this crawler, the wildcard groups no longer apply.
  const specific = matching.filter((group) => group.agents.some((value) => value !== '*' && agent.includes(value)))
  const rules = (specific.length > 0 ? specific : matching).flatMap((group) => group.rules)
  let best: RobotsRule | null = null
  let bestSpecificity = -1
  for (const rule of rules) {
    if (!patternToRegExp(rule.pattern).test(path)) continue
    const specificity = rule.pattern.replace(/\*/g, '').length
    if (specificity > bestSpecificity || (specificity === bestSpecificity && rule.allow && best && !best.allow)) {
      best = rule
      bestSpecificity = specificity
    }
  }
  if (!best) return { allowed: true, matchedPattern: null, specificity: 0 }
  return { allowed: best.allow, matchedPattern: best.pattern, specificity: bestSpecificity }
}

export function isAllowedByRobots(file: RobotsFile, url: string, userAgent: string = CRAWLER_USER_AGENT): boolean {
  const parsed = new URL(url)
  return evaluateRobots(file, `${parsed.pathname}${parsed.search}`, userAgent).allowed
}

/** Empty rule set used when robots.txt is missing (HTTP 4xx): everything is allowed. */
export function emptyRobots(): RobotsFile {
  return { groups: [], sitemaps: [], parserVersion: ROBOTS_PARSER_VERSION }
}
