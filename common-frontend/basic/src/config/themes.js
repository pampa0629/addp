/**
 * 主题配置文件
 *
 * 增加新主题步骤：
 * 1. 在此文件中添加主题配置
 * 2. 在 theme.css 中添加对应的 CSS 变量定义
 */

export const THEME_CONFIGS = [
  {
    value: 'system',
    label: '跟随系统',
    icon: 'Monitor',
    isDarkTheme: null // null 表示跟随系统
  },
  {
    value: 'light',
    label: '浅色模式',
    icon: 'Sunny',
    isDarkTheme: false
  },
  {
    value: 'dark',
    label: '深色模式',
    icon: 'Moon',
    isDarkTheme: true
  },
  {
    value: 'blue',
    label: '蓝色模式',
    icon: 'Cloudy',
    isDarkTheme: true // 蓝色主题视为深色（用于 Element Plus 深色 CSS）
  },
  {
    value: 'purple',
    label: '紫色模式',
    icon: 'MagicStick',
    isDarkTheme: true // 紫色主题视为深色（用于 Element Plus 深色 CSS）
  }
]

// 导出主题值列表（用于验证）
export const THEME_VALUES = THEME_CONFIGS.map(t => t.value)

// 根据主题值获取配置
export function getThemeConfig(value) {
  return THEME_CONFIGS.find(t => t.value === value)
}

// 判断主题是否为深色
export function isThemeDark(value, systemPrefersDark = false) {
  const config = getThemeConfig(value)
  if (!config) return false

  if (config.isDarkTheme === null) {
    // 跟随系统
    return systemPrefersDark
  }

  return config.isDarkTheme
}
