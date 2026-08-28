import { describe, expect, it } from 'vitest'
import { isValidOAuthRedirectURI, validateOAuthRedirectURIs } from './oauthClientValidation'

describe('external OAuth client redirect URI validation', () => {
  it('accepts HTTPS and IP-literal loopback HTTP callbacks', () => {
    expect(isValidOAuthRedirectURI('https://bi.example.com/oauth/callback')).toBe(true)
    expect(isValidOAuthRedirectURI('http://127.0.0.1:49152/callback')).toBe(true)
    expect(isValidOAuthRedirectURI('http://[::1]:49152/callback')).toBe(true)
  })

  it('rejects insecure remote, localhost, fragment, wildcard, and credential-bearing callbacks', () => {
    expect(isValidOAuthRedirectURI('http://bi.example.com/callback')).toBe(false)
    expect(isValidOAuthRedirectURI('http://localhost/callback')).toBe(false)
    expect(isValidOAuthRedirectURI('https://localhost/callback')).toBe(false)
    expect(isValidOAuthRedirectURI('https://bi.example.com/callback#token')).toBe(false)
    expect(isValidOAuthRedirectURI('https://*.example.com/callback')).toBe(false)
    expect(isValidOAuthRedirectURI('https://user:password@bi.example.com/callback')).toBe(false)
  })

  it('requires a bounded unique callback list', () => {
    expect(validateOAuthRedirectURIs(['https://bi.example.com/callback'])).toBe(true)
    expect(validateOAuthRedirectURIs([])).toBe(false)
    expect(validateOAuthRedirectURIs(['https://bi.example.com/callback', 'https://bi.example.com/callback'])).toBe(false)
  })
})
