import { isIP } from 'node:net'
import { lookup } from 'node:dns/promises'

function isBlockedIPv4(host: string): boolean {
  const octets = host.split('.').map(Number)
  const [a = -1, b = -1, c = -1] = octets
  return a === 0 || a === 10 || a === 127 || a >= 224
    || (a === 100 && b >= 64 && b <= 127)
    || (a === 169 && b === 254)
    || (a === 172 && b >= 16 && b <= 31)
    || (a === 192 && ((b === 0 && (c === 0 || c === 2)) || b === 168))
    || (a === 198 && (b === 18 || b === 19 || (b === 51 && c === 100)))
    || (a === 203 && b === 0 && c === 113)
}

function isBlockedIPv6(host: string): boolean {
  const normalized = host.toLowerCase()
  if (normalized.startsWith('::ffff:')) return isBlockedIPv4(normalized.slice(7))
  return normalized === '::' || normalized === '::1' || normalized.startsWith('fc') || normalized.startsWith('fd')
    || /^fe[89ab]/.test(normalized) || normalized.startsWith('ff') || normalized.startsWith('2001:db8:')
}

export function assertPublicHttpUrl(input: string): URL {
  const url = new URL(input)
  if (!['http:', 'https:'].includes(url.protocol)) throw new Error('UNSUPPORTED_SCHEME')
  if (url.username || url.password) throw new Error('URL_CREDENTIALS_FORBIDDEN')
  const host = url.hostname.toLowerCase().replace(/^\[|\]$/g, '')
  if (host === 'localhost' || host.endsWith('.localhost')) throw new Error('PRIVATE_ADDRESS')
  if (isIP(host) === 4 && isBlockedIPv4(host)) throw new Error('PRIVATE_ADDRESS')
  if (isIP(host) === 6 && isBlockedIPv6(host)) throw new Error('PRIVATE_ADDRESS')
  if (url.port && !['80', '443'].includes(url.port)) throw new Error('PORT_FORBIDDEN')
  return url
}

export async function assertPublicResolvedUrl(input: string): Promise<URL> {
  const url = assertPublicHttpUrl(input)
  const addresses = await lookup(url.hostname, { all: true, verbatim: true })
  if (addresses.length === 0) throw new Error('DNS_NO_RESULTS')
  for (const address of addresses) assertPublicHttpUrl(`${url.protocol}//${address.family === 6 ? `[${address.address}]` : address.address}`)
  return url
}
