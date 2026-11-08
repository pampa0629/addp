// Render only the fields declared by the plugin. Never serialize connection_info wholesale.
const cell = value => String(value ?? '')
  .replace(/[\\`*_[\]{}()#+.!|<>-]/g, '\\$&')
  .replace(/\r\n|\r|\n/g, '<br>')

export function buildEngineConfigurationNote({ engine, descriptor, summary, sections, generatedAt, t }) {
  if (descriptor?.type !== engine.engine_type || !descriptor?.connection_spec?.fields) {
    throw new TypeError('A matching connection field descriptor is required')
  }
  const key = name => t(`system.engine.configurationNote.${name}`)
  const basic = name => t(`system.engine.dialog.basicInfo.${name}`)
  const table = rows => [
    `| ${cell(key('field'))} | ${cell(key('value'))} |`, '| --- | --- |',
    ...rows.map(([label, value]) => `| ${cell(label)} | ${cell(value)} |`)
  ].join('\n')
  const connection = descriptor.connection_spec.fields
    .filter(field => !field.sensitive && Object.hasOwn(engine.connection_info || {}, field.key))
    .map(field => [field.key, engine.connection_info[field.key]])
    .filter(([, value]) => value === null || ['string', 'number', 'boolean'].includes(typeof value))

  const lines = [
    `# ${cell(key('title'))}`, '', key('notice'), '',
    `## ${cell(t('system.engine.dialog.detailTabs.basic'))}`, '',
    table([
      [basic('id'), engine.id], [basic('name'), engine.name],
      [basic('engineType'), engine.engine_type], [key('configurationVersion'), engine.version],
      [basic('status'), t(`system.engine.status.${engine.lifecycle_state}`)],
      [basic('serverVersion'), engine.server_version || key('notRecorded')],
      [basic('serverVersionCheckedAt'), engine.server_version_checked_at || key('notRecorded')],
      [basic('updatedAt'), engine.updated_at || key('notRecorded')], [key('generatedAt'), generatedAt]
    ]), '',
    `## ${cell(t('system.engine.dialog.detailTabs.connection'))}`, '',
    key('connectionNotice'), '', connection.length ? table(connection) : key('notRecorded'), '',
    `## ${cell(t('system.engine.dialog.detailTabs.capabilities'))}`, '', key('capabilityNotice'), '',
    ...(summary.length ? summary.map(value => `- ${cell(value)}`) : [key('notRecorded')]), ''
  ]
  for (const section of sections) {
    lines.push(`### ${cell(section.title)}`, '', cell(section.status), '')
    if (section.description) lines.push(cell(section.description), '')
    if (section.rows.length) lines.push(table(section.rows), '')
  }
  return { fileName: `engine-${engine.id}-configuration.md`, content: lines.join('\n') }
}
