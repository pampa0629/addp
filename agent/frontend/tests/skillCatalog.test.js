import { describe, expect, it } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import en from '../src/i18n/en.json'
import zh from '../src/i18n/zh-cn.json'

describe('registered Skill home', () => {
  it('covers exactly the authoritative runtime Skill catalog in both languages', () => {
    const root = resolve(process.cwd(), '../../skills')
    const names = readdirSync(root).filter(name => readFileSync(resolve(root, name, 'agents/addp.yaml'), 'utf8').includes('addp.skill-runtime/v1')).sort()
    for (const locale of [zh, en]) {
      expect(Object.keys(locale.agent.skills).sort()).toEqual(names)
      for (const skill of Object.values(locale.agent.skills)) {
        expect(skill.title).toBeTruthy(); expect(skill.description).toBeTruthy(); expect(skill.example).toBeTruthy()
      }
    }
  })
})
