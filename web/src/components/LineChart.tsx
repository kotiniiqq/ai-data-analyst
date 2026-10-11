import type { Chart } from '../types'
import { formatValue, monthLabel } from '../format'

const W = 640
const H = 220
const PAD = { top: 16, right: 16, bottom: 28, left: 72 }
const COLORS = ['var(--signal)', 'var(--ink)', 'var(--amber)', 'var(--green)']

export function LineChart({ chart }: { chart: Chart }) {
  const all = chart.series.flatMap((s) => s.values)
  let lo = Math.min(...all)
  let hi = Math.max(...all)
  if (lo === hi) {
    lo -= 1
    hi += 1
  }
  const n = chart.x.length
  const x = (i: number) => PAD.left + (n === 1 ? 0 : (i / (n - 1)) * (W - PAD.left - PAD.right))
  const y = (v: number) => PAD.top + (1 - (v - lo) / (hi - lo)) * (H - PAD.top - PAD.bottom)
  const names = chart.series.map((s) => s.name).join(', ')

  return (
    <figure className="chart">
      <svg viewBox={`0 0 ${W} ${H}`} role="img" aria-label={`Line chart of ${names} by month`}>
        {[lo, (lo + hi) / 2, hi].map((v) => (
          <g key={v} className="grid">
            <line x1={PAD.left} x2={W - PAD.right} y1={y(v)} y2={y(v)} />
            <text x={PAD.left - 8} y={y(v)} dy="0.32em" textAnchor="end">
              {formatValue(Math.round(v * 100) / 100)}
            </text>
          </g>
        ))}
        {chart.series.map((s, si) => (
          <path
            key={s.name}
            className="line"
            stroke={COLORS[si % COLORS.length]}
            d={s.values.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join(' ')}
          />
        ))}
        <text className="axis" x={x(0)} y={H - 6} textAnchor="start">
          {monthLabel(chart.x[0])}
        </text>
        {n > 1 && (
          <text className="axis" x={x(n - 1)} y={H - 6} textAnchor="end">
            {monthLabel(chart.x[n - 1])}
          </text>
        )}
      </svg>
      {chart.series.length > 1 && (
        <figcaption className="legend">
          {chart.series.map((s, si) => (
            <span key={s.name}>
              <i style={{ background: COLORS[si % COLORS.length] }} />
              {s.name}
            </span>
          ))}
        </figcaption>
      )}
    </figure>
  )
}
