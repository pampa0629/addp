const readThemeColor = (styles, variable) => styles.getPropertyValue(variable).trim()

export const readMermaidTheme = () => {
  const styles = getComputedStyle(document.documentElement)

  return {
    background: readThemeColor(styles, '--addp-bg-primary'),
    surface: readThemeColor(styles, '--addp-bg-secondary'),
    textPrimary: readThemeColor(styles, '--addp-text-primary'),
    textSecondary: readThemeColor(styles, '--addp-text-secondary'),
    border: readThemeColor(styles, '--addp-border-color'),
    borderLight: readThemeColor(styles, '--addp-border-color-light'),
    edge: readThemeColor(styles, '--addp-graph-edge-default'),
    edgeLabel: readThemeColor(styles, '--addp-graph-edge-label'),
    edgeLabelBackground: readThemeColor(styles, '--addp-graph-edge-label-stroke'),
    nodeStroke: readThemeColor(styles, '--addp-graph-node-stroke'),
    labelLight: readThemeColor(styles, '--addp-graph-label-light'),
    labelDark: readThemeColor(styles, '--addp-graph-label-dark'),
    categories: Array.from(
      { length: 12 },
      (_, index) => readThemeColor(styles, `--addp-graph-category-${index + 1}`)
    )
  }
}

export const initializeMermaidTheme = (mermaid, diagramConfig = {}) => {
  const theme = readMermaidTheme()

  mermaid.initialize({
    startOnLoad: false,
    theme: 'base',
    themeVariables: {
      background: theme.background,
      primaryColor: theme.surface,
      primaryTextColor: theme.textPrimary,
      primaryBorderColor: theme.border,
      secondaryColor: theme.surface,
      secondaryTextColor: theme.textPrimary,
      secondaryBorderColor: theme.border,
      tertiaryColor: theme.background,
      tertiaryTextColor: theme.textPrimary,
      tertiaryBorderColor: theme.borderLight,
      textColor: theme.textPrimary,
      lineColor: theme.edge,
      defaultLinkColor: theme.edge,
      mainBkg: theme.surface,
      nodeBorder: theme.border,
      clusterBkg: theme.background,
      clusterBorder: theme.border,
      edgeLabelBackground: theme.edgeLabelBackground,
      titleColor: theme.textPrimary,
      entityBkg: theme.surface,
      entityBorder: theme.border,
      attributeBackgroundColorOdd: theme.surface,
      attributeBackgroundColorEven: theme.background,
      relationshipLabelBackground: theme.edgeLabelBackground,
      relationshipLabelColor: theme.edgeLabel
    },
    ...diagramConfig
  })

  return theme
}

export const observeThemeChange = callback => {
  const observer = new MutationObserver(mutations => {
    if (mutations.some(mutation => mutation.attributeName === 'class')) callback()
  })

  observer.observe(document.documentElement, {
    attributes: true,
    attributeFilter: ['class']
  })

  return () => observer.disconnect()
}
