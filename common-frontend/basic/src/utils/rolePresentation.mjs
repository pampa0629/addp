export function resolveRoleName(role, t, te) {
  if (role?.name) return role.name
  if (role?.name_i18n_key && te(role.name_i18n_key)) return t(role.name_i18n_key)
  if (role?.role_name) return role.role_name
  if (role?.role_name_i18n_key && te(role.role_name_i18n_key)) return t(role.role_name_i18n_key)
  return role?.role_key || ''
}

export function resolveRoleDescription(role, t, te) {
  if (role?.description) return role.description
  if (role?.description_i18n_key && te(role.description_i18n_key)) return t(role.description_i18n_key)
  const fallbackKey = role?.role_key ? `roles.${role.role_key}.description` : ''
  return fallbackKey && te(fallbackKey) ? t(fallbackKey) : ''
}
