// Turns the points the API returns into what the three charts draw.
import { t } from '../../../i18n'
import type { ApiPoint } from '../types'
import { COLORS } from '../util'
import type { ChartSeries } from './Chart.vue'

// requests per minute, stacked by outcome; 5xx sits at the bottom so that it is read off the baseline
export function requestSeries(points: ApiPoint[], stepSeconds: number): ChartSeries[] {
  const perMinute = (v: number) => v / (stepSeconds / 60)
  return [
    { key: 'c5', label: '5xx', color: COLORS.c5, values: points.map(p => perMinute(p.c5)) },
    { key: 'c4', label: '4xx', color: COLORS.c4, values: points.map(p => perMinute(p.c4)) },
    { key: 'ok', label: '2xx', color: COLORS.ok, values: points.map(p => perMinute(p.ok)) },
  ]
}
export function errorRateSeries(points: ApiPoint[]): ChartSeries[] {
  return [{ key: 'rate', label: t('ad.error_rate'), color: COLORS.c5, values: points.map(p => { const n = p.ok + p.c4 + p.c5; return n ? p.c5 / n : 0 }) }]
}
export function latencySeries(points: ApiPoint[]): ChartSeries[] {
  return [
    { key: 'p95', label: 'P95', color: COLORS.p95, values: points.map(p => p.p95) },
    { key: 'p50', label: 'P50', color: COLORS.p50, values: points.map(p => p.p50) },
  ]
}
