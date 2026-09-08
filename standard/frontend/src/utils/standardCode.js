export const STANDARD_STABLE_CODE_PATTERN = /^[a-z][a-z0-9_]*$/

export function isValidStandardStableCode(value, maxLength = 100) {
  const code = typeof value === 'string' ? value.trim() : ''
  return code.length > 0 && code.length <= maxLength && STANDARD_STABLE_CODE_PATTERN.test(code)
}

export function buildStandardCodeRules(t, requiredMessageKey, maxLength = 100) {
  return [
    { required: true, message: t(requiredMessageKey), trigger: 'blur' },
    {
      trigger: 'blur',
      validator: (_rule, value, callback) => {
        if (!value || isValidStandardStableCode(value, maxLength)) callback()
        else callback(new Error(t('standard.common.codeFormatInvalid')))
      }
    }
  ]
}
