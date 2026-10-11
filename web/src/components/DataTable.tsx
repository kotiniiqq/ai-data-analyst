import { useState } from 'react'
import type { Rows } from '../types'
import { formatValue } from '../format'

const PREVIEW = 10

export function DataTable({ rows }: { rows: Rows }) {
  const [all, setAll] = useState(false)
  const shown = all ? rows.rows : rows.rows.slice(0, PREVIEW)
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            {rows.columns.map((c) => (
              <th key={c} scope="col">
                {c}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {shown.map((r, i) => (
            <tr key={i}>
              {r.map((v, j) => (
                <td key={j} className={typeof v === 'number' ? 'num' : undefined}>
                  {formatValue(v)}
                </td>
              ))}
            </tr>
          ))}
        </tbody>
      </table>
      {!all && rows.rows.length > PREVIEW && (
        <button type="button" className="link" onClick={() => setAll(true)}>
          Show all {rows.rows.length} rows
        </button>
      )}
    </div>
  )
}
