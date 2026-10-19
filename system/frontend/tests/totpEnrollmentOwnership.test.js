import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
const source = name => readFileSync(new URL(`../src/${name}`, import.meta.url), 'utf8')
describe('TOTP enrollment material ownership', () => {
  it('shares the enrollment display between self-service and administrator recovery', () => {
    for (const path of ['components/iam/MFASecurityPanel.vue', 'views/AdministratorRecovery.vue']) {
      const text = source(path)
      expect(text).toContain('<TOTPEnrollmentDetails')
      expect(text).not.toContain("from 'qrcode'")
      expect(text).not.toContain('<canvas')
    }
    expect(source('components/iam/TOTPEnrollmentDetails.vue')).toContain("from 'qrcode'")
  })
})
