import { describe, expect, it } from 'vitest'
import { privateHost, unencryptedOutside } from './transport'

describe('how the page was reached', () => {
  it('knows the home network, this machine and a VPN', () => {
    for (const h of ['localhost', '127.0.0.1', '192.168.1.20', '10.0.0.4', '172.16.9.9', '172.31.0.1', '169.254.3.3', '100.101.102.103', 'mediabox', 'nas.local', 'box.lan', 'roxy.home.arpa', 'mediabox.tail1234.ts.net', '[::1]', '[fe80::1]', '[fd7a:115c:a1e0::1]']) {
      expect(privateHost(h), h).toBe(true)
    }
  })

  it('takes every other address for the outside', () => {
    for (const h of ['203.0.113.9', 'roxy.example.com', '172.32.0.1', '100.63.0.1', '100.128.0.1', '192.169.0.1', '11.0.0.1', '[2600:1700::1]', 'example.ts.net.evil.com', 'local']) {
      // "local" alone is one word: a machine's name. Everything else here is public.
      expect(privateHost(h), h).toBe(h === 'local')
    }
  })

  it('warns only for plain http from outside', () => {
    expect(unencryptedOutside({ protocol: 'http:', hostname: 'roxy.example.com' })).toBe(true)
    expect(unencryptedOutside({ protocol: 'http:', hostname: '203.0.113.9' })).toBe(true)
    expect(unencryptedOutside({ protocol: 'https:', hostname: 'roxy.example.com' })).toBe(false)
    expect(unencryptedOutside({ protocol: 'http:', hostname: '192.168.1.20' })).toBe(false)
    expect(unencryptedOutside({ protocol: 'http:', hostname: 'mediabox.tail1234.ts.net' })).toBe(false)
  })
})
