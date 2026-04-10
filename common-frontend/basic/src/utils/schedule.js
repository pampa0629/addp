/**
 * 前端调度配置工具函数
 * 支持 daily/weekly/monthly 转换为 Cron 表达式
 * 统一各模块的调度配置逻辑
 *
 * 描述生成函数（buildScheduleFromForm / generateScheduleDescription / describeCron）
 * 接受可选的 t 参数（vue-i18n 的翻译函数）。若提供则使用多语言输出，否则回退到中文。
 */

// 预设选项（key 对应 i18n schedule.presetLabel.* 和 schedule.presetDesc.*）
const presetOptions = [
  { key: 'every-minute', i18nKey: 'everyMinute', label: '每分钟', cron: '* * * * *', description: '每分钟执行一次' },
  { key: 'every-15min', i18nKey: 'every15min', label: '每15分钟', cron: '*/15 * * * *', description: '每 15 分钟执行一次' },
  { key: 'every-30min', i18nKey: 'every30min', label: '每30分钟', cron: '*/30 * * * *', description: '每 30 分钟执行一次' },
  { key: 'hourly', i18nKey: 'hourly', label: '每小时', cron: '0 * * * *', description: '每小时整点执行' },
  { key: 'daily-midnight', i18nKey: 'dailyMidnight', label: '每天凌晨', cron: '0 0 * * *', description: '每天 00:00 执行' },
  { key: 'weekly-monday', i18nKey: 'weeklyMonday', label: '每周一零点', cron: '0 0 * * 1', description: '每周一 00:00 执行' },
  { key: 'monthly-first', i18nKey: 'monthlyFirst', label: '每月1号零点', cron: '0 0 1 * *', description: '每月 1 号 00:00 执行' }
]

const presetOptionMapByKey = presetOptions.reduce((acc, item) => {
  acc[item.key] = item
  return acc
}, {})

const presetOptionMapByCron = presetOptions.reduce((acc, item) => {
  acc[item.cron] = item
  return acc
}, {})

// 星期选项（value 对应 i18n schedule.weekday.*）
const weeklyOptions = [
  { value: '1', label: '周一' },
  { value: '2', label: '周二' },
  { value: '3', label: '周三' },
  { value: '4', label: '周四' },
  { value: '5', label: '周五' },
  { value: '6', label: '周六' },
  { value: '0', label: '周日' }
]

const weeklyOrder = ['1', '2', '3', '4', '5', '6', '0']

const weeklyLabelMap = weeklyOptions.reduce((acc, item) => {
  acc[item.value] = item.label
  return acc
}, {})

/**
 * 标准化时间字符串
 */
const normalizeTimeString = (time) => {
  const [hourPart = '0', minutePart = '0'] = (time || '').split(':')
  let hour = Number(hourPart)
  let minute = Number(minutePart)
  if (Number.isNaN(hour)) hour = 0
  if (Number.isNaN(minute)) minute = 0
  hour = Math.min(Math.max(hour, 0), 23)
  minute = Math.min(Math.max(minute, 0), 59)
  return {
    hour,
    minute,
    display: `${String(hour).padStart(2, '0')}:${String(minute).padStart(2, '0')}`
  }
}

/**
 * 排序星期选项
 */
const sortWeekDays = (days = []) => {
  return Array.from(new Set(days))
    .filter(day => weeklyOrder.includes(day))
    .sort((a, b) => weeklyOrder.indexOf(a) - weeklyOrder.indexOf(b))
}

/**
 * 获取星期显示名称（支持 i18n）
 */
const getWeekdayLabel = (day, t) => {
  if (t) {
    return t(`schedule.weekday.${day}`, weeklyLabelMap[day] || `Day${day}`)
  }
  return weeklyLabelMap[day] || `周${day}`
}

/**
 * 从表单构建调度配置
 * @param {Object} form - 表单数据 { mode, time, weekDays, dayOfMonth }
 * @param {Function} [t] - vue-i18n 翻译函数（可选）
 * @returns {Object|null} - { cron, description } 或 null
 */
const buildScheduleFromForm = (form, t) => {
  if (!form) return null
  const { hour, minute, display } = normalizeTimeString(form.time || '09:00')

  if (form.mode === 'daily') {
    const desc = t
      ? t('schedule.desc.daily', { time: display })
      : `每天 ${display} 执行`
    return { cron: `${minute} ${hour} * * *`, description: desc }
  }

  if (form.mode === 'weekly') {
    const sortedDays = sortWeekDays(form.weekDays)
    if (sortedDays.length === 0) {
      return null
    }
    const labels = sortedDays.map(day => getWeekdayLabel(day, t))
    const daysStr = labels.join(t ? ' ' : '、')
    const desc = t
      ? t('schedule.desc.weekly', { days: daysStr, time: display })
      : `每周${daysStr} ${display} 执行`
    return {
      cron: `${minute} ${hour} * * ${sortedDays.join(',')}`,
      description: desc
    }
  }

  if (form.mode === 'monthly') {
    const day = Math.min(Math.max(Number(form.dayOfMonth) || 1, 1), 31)
    const desc = t
      ? t('schedule.desc.monthly', { day, time: display })
      : `每月 ${day} 日 ${display} 执行`
    return { cron: `${minute} ${hour} ${day} * *`, description: desc }
  }

  return null
}

/**
 * 生成调度描述
 * @param {Object} form
 * @param {Function} [t]
 */
const generateScheduleDescription = (form, t) => {
  const result = buildScheduleFromForm(form, t)
  if (result) {
    return result.description
  }
  if (form?.mode === 'weekly' && (!form.weekDays || form.weekDays.length === 0)) {
    return t ? t('schedule.error.selectDay') : '请选择至少一个执行日'
  }
  return t ? t('schedule.error.selectComplete') : '请选择完整的执行时间'
}

/**
 * 检查是否为特定数字
 */
const isSpecificNumber = (value, min, max) => /^\d+$/.test(value) && Number(value) >= min && Number(value) <= max

/**
 * 解码 Cron 表达式为表单数据
 * @param {String} cron - Cron 表达式
 * @returns {Object|null} - 表单数据或 null
 */
const decodeScheduleToForm = (cron) => {
  if (!cron) return null
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return null
  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts
  if (month !== '*') return null
  if (!isSpecificNumber(minute, 0, 59) || !isSpecificNumber(hour, 0, 23)) return null

  const time = `${String(Number(hour)).padStart(2, '0')}:${String(Number(minute)).padStart(2, '0')}`

  // 每天
  if (dayOfMonth === '*' && dayOfWeek === '*') {
    return {
      mode: 'daily',
      time,
      weekDays: ['1'],
      dayOfMonth: 1
    }
  }

  // 每周
  if (dayOfMonth === '*' && dayOfWeek !== '*') {
    const days = dayOfWeek.split(',').filter(Boolean)
    if (days.length > 0 && days.every(day => weeklyOrder.includes(day))) {
      return {
        mode: 'weekly',
        time,
        weekDays: sortWeekDays(days),
        dayOfMonth: 1
      }
    }
  }

  // 每月
  if (dayOfWeek === '*' && dayOfMonth !== '*' && isSpecificNumber(dayOfMonth, 1, 31)) {
    return {
      mode: 'monthly',
      time,
      weekDays: ['1'],
      dayOfMonth: Number(dayOfMonth)
    }
  }

  return null
}

/**
 * 描述 Cron 表达式
 * @param {String} cron - Cron 表达式
 * @param {Function} [t] - vue-i18n 翻译函数（可选）
 * @returns {String} - 本地化描述
 */
const describeCron = (cron, t) => {
  if (!cron) return ''
  const normalized = cron.trim().replace(/\s+/g, ' ')

  // 检查预设选项
  const preset = presetOptionMapByCron[normalized]
  if (preset) {
    return t
      ? t(`schedule.presetDesc.${preset.i18nKey}`, preset.description)
      : preset.description
  }

  // 尝试解码为标准格式
  const decoded = decodeScheduleToForm(normalized)
  if (decoded) {
    const result = buildScheduleFromForm(decoded, t)
    if (result) {
      return result.description
    }
  }

  // 匹配 */N 格式（每N分钟）
  const intervalMatch = normalized.match(/^\*\/(\d+) \* \* \* \*$/)
  if (intervalMatch) {
    return t
      ? t('schedule.desc.everyN', { n: intervalMatch[1] })
      : `每 ${intervalMatch[1]} 分钟执行一次`
  }

  // 匹配每小时格式
  const hourlyMatch = normalized.match(/^(\d{1,2}) \* \* \* \*$/)
  if (hourlyMatch) {
    const minute = Math.min(Math.max(Number(hourlyMatch[1]) || 0, 0), 59)
    const minuteStr = String(minute).padStart(2, '0')
    return t
      ? t('schedule.desc.hourlyAt', { minute: minuteStr })
      : `每小时的 ${minuteStr} 分执行`
  }

  return t ? t('schedule.desc.custom') : '已设置自定义调度'
}

/**
 * 验证 Cron 表达式格式
 * @param {String} cron - Cron 表达式
 * @returns {Boolean} - 是否有效
 */
const validateCron = (cron) => {
  if (!cron) return false
  const parts = cron.trim().split(/\s+/)
  // 支持 5 字段（分钟级）或 6 字段（秒级）
  if (parts.length !== 5 && parts.length !== 6) return false

  // 简单验证（实际验证应该调用后端 API）
  return true
}

export {
  presetOptions,
  presetOptionMapByKey,
  presetOptionMapByCron,
  weeklyOptions,
  weeklyOrder,
  weeklyLabelMap,
  normalizeTimeString,
  sortWeekDays,
  buildScheduleFromForm,
  generateScheduleDescription,
  decodeScheduleToForm,
  describeCron,
  validateCron
}
