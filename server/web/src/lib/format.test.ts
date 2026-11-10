import { describe, expect, it } from 'vitest'
import { countryCentroid, countryName, flag, fmtAgo, fmtDelta, fmtExact, fmtNum, fmtRatio } from './format'

describe('format', () => {
  it('compacts numbers', () => {
    expect(fmtNum(980)).toBe('980')
    expect(fmtNum(12400)).toBe('12.4k')
    expect(fmtNum(10000)).toBe('10k')
    expect(fmtNum(1500000)).toBe('1.5m')
  })
  it('formats deltas', () => {
    expect(fmtDelta(108, 100)).toBe('+8%')
    expect(fmtDelta(97, 100)).toBe('-3%')
    expect(fmtDelta(5, 0)).toBe('new')
    expect(fmtDelta(0, 0)).toBe('')
  })
  it('formats ratios', () => {
    expect(fmtRatio(162, 100)).toBe('1.62')
    expect(fmtRatio(200, 100)).toBe('2')
    expect(fmtRatio(0, 0)).toBe('0')
  })
  it('knows countries', () => {
    expect(countryName('GB')).toBe('United Kingdom')
    expect(countryName('')).toBe('Unknown')
    expect(countryCentroid('JP')?.length).toBe(2)
    expect(countryCentroid('XX')).toBeNull()
    expect(flag('GB')).toBe('🇬🇧')
    expect(flag('')).toBe('')
  })
  it('formats exact timestamps', () => {
    expect(fmtExact('not-a-date')).toBe('not-a-date')
    expect(fmtExact('2026-01-15T12:00:00Z')).toBe(new Date('2026-01-15T12:00:00Z').toLocaleString())
  })
  it('formats relative age', () => {
    const now = new Date('2026-01-15T12:00:00Z').getTime()
    expect(fmtAgo('2026-01-15T11:59:30Z', now)).toBe('just now')
    expect(fmtAgo('2026-01-15T11:55:00Z', now)).toBe('5m ago')
    expect(fmtAgo('2026-01-15T09:00:00Z', now)).toBe('3h ago')
    expect(fmtAgo('2026-01-11T12:00:00Z', now)).toBe('4d ago')
    expect(fmtAgo('garbage', now)).toBe('-')
  })
})
