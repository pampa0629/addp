const presetOptions = [
  { key: 'daily_midnight', label: '每天凌晨执行', cron: '0 0 * * *', description: '每天 00:00 执行' },
  { key: 'hourly', label: '每小时执行', cron: '0 * * * *', description: '每小时整点执行' },
  { key: 'quarter', label: '每15分钟执行', cron: '*/15 * * * *', description: '每 15 分钟执行一次' }
]

const presetOptionMapByKey = presetOptions.reduce((acc, item) => {
  acc[item.key] = item
  return acc
}, {})

const presetOptionMapByCron = presetOptions.reduce((acc, item) => {
  acc[item.cron] = item
  return acc
}, {})

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

const sortWeekDays = (days = []) => {
  return Array.from(new Set(days))
    .filter(day => weeklyOrder.includes(day))
    .sort((a, b) => weeklyOrder.indexOf(a) - weeklyOrder.indexOf(b))
}

const buildScheduleFromForm = (form) => {
  if (!form) return null
  const { hour, minute, display } = normalizeTimeString(form.time || '09:00')
  if (form.mode === 'daily') {
    return {
      cron: `${minute} ${hour} * * *`,
      description: `每天 ${display} 执行`
    }
  }
  if (form.mode === 'weekly') {
    const sortedDays = sortWeekDays(form.weekDays)
    if (sortedDays.length === 0) {
      return null
    }
    const labels = sortedDays.map(day => weeklyLabelMap[day] || `周${day}`)
    return {
      cron: `${minute} ${hour} * * ${sortedDays.join(',')}`,
      description: `每周${labels.join('、')} ${display} 执行`
    }
  }
  if (form.mode === 'monthly') {
    const day = Math.min(Math.max(Number(form.dayOfMonth) || 1, 1), 31)
    return {
      cron: `${minute} ${hour} ${day} * *`,
      description: `每月 ${day} 日 ${display} 执行`
    }
  }
  return null
}

const generateScheduleDescription = (form) => {
  const result = buildScheduleFromForm(form)
  if (result) {
    return result.description
  }
  if (form?.mode === 'weekly' && (!form.weekDays || form.weekDays.length === 0)) {
    return '请选择至少一个执行日'
  }
  return '请选择完整的执行时间'
}

const isSpecificNumber = (value, min, max) => /^\d+$/.test(value) && Number(value) >= min && Number(value) <= max

const decodeScheduleToForm = (cron) => {
  if (!cron) return null
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return null
  const [minute, hour, dayOfMonth, month, dayOfWeek] = parts
  if (month !== '*') return null
  if (!isSpecificNumber(minute, 0, 59) || !isSpecificNumber(hour, 0, 23)) return null

  const time = `${String(Number(hour)).padStart(2, '0')}:${String(Number(minute)).padStart(2, '0')}`

  if (dayOfMonth === '*' && dayOfWeek === '*') {
    return {
      mode: 'daily',
      time,
      weekDays: ['1'],
      dayOfMonth: 1
    }
  }

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

const describeCron = (cron) => {
  if (!cron) return ''
  const normalized = cron.trim().replace(/\s+/g, ' ')
  if (presetOptionMapByCron[normalized]) {
    return presetOptionMapByCron[normalized].description
  }

  const decoded = decodeScheduleToForm(normalized)
  if (decoded) {
    const result = buildScheduleFromForm(decoded)
    if (result) {
      return result.description
    }
  }

  const intervalMatch = normalized.match(/^\*\/(\d+) \* \* \* \*$/)
  if (intervalMatch) {
    return `每 ${intervalMatch[1]} 分钟执行一次`
  }

  const hourlyMatch = normalized.match(/^(\d{1,2}) \* \* \* \*$/)
  if (hourlyMatch) {
    const minute = Math.min(Math.max(Number(hourlyMatch[1]) || 0, 0), 59)
    return `每小时的 ${String(minute).padStart(2, '0')} 分执行`
  }

  return '已设置自定义调度'
}

export {
  presetOptions,
  presetOptionMapByKey,
  presetOptionMapByCron,
  weeklyOptions,
  buildScheduleFromForm,
  generateScheduleDescription,
  decodeScheduleToForm,
  describeCron
}
