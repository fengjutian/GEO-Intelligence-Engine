import { isIP } from 'node:net'
import { lookup } from 'node:dns/promises'

const blockedIPv4 = [/^127\./, /^10\./, /^192\.168\./, /^169\.254\./, /^0\./]

export function assertPublicHttpUrl(input: string): URL {
  const url = new URL(input)
  if (!['http:', 'https:'].includes(url.protocol)) throw new Error('UNSUPPORTED_SCHEME')
  if (url.username || url.password) throw new Error('URL_CREDENTIALS_FORBIDDEN')
  const host = url.hostname.toLowerCase().replace(/^\[|\]$/g, '')
  if (host === 'localhost' || host.endsWith('.localhost')) throw new Error('PRIVATE_ADDRESS')
  if (isIP(host) === 4 && (blockedIPv4.some((rule) => rule.test(host)) || /^172\.(1[6-9]|2\d|3[01])\./.test(host))) throw new Error('PRIVATE_ADDRESS')
  if (isIP(host) === 6 && (host === '::1' || host.startsWith('fe80:') || host.startsWith('fc') || host.startsWith('fd'))) throw new Error('PRIVATE_ADDRESS')
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
