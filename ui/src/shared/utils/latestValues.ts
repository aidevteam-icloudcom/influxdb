import {get, range, flatMap} from 'lodash'
import {isNumeric, Table} from '@influxdata/vis'

/*
  Return a list of the maximum elements in `xs`, where the magnitude of each
  element is computed using the passed function `d`.
*/
const maxesBy = <X>(xs: X[], d: (x: X) => number): X[] => {
  let maxes = []
  let maxDist = -Infinity

  for (const x of xs) {
    const dist = d(x)

    if (dist > maxDist) {
      maxes = [x]
      maxDist = dist
    } else if (dist === maxDist && dist !== -Infinity) {
      maxes.push(x)
    }
  }

  return maxes
}

const EXCLUDED_COLUMNS = new Set([
  '_start',
  '_stop',
  '_time',
  'table',
  'result',
  '',
])

/*
  Determine if the values in a column should be considered in `latestValues`.
*/
const isValueCol = (table: Table, colKey: string): boolean => {
  const {name, type} = table.columns[colKey]

  return isNumeric(type) && !EXCLUDED_COLUMNS.has(name)
}

/*
  We sort the column keys that we pluck latest values from, so that:

  - Columns named `_value` have precedence
  - The returned latest values are in a somewhat stable order
*/
const sortTableKeys = (keyA: string, keyB: string): number => {
  if (keyA.includes('_value')) {
    return -1
  } else if (keyB.includes('_value')) {
    return 1
  } else {
    return keyA.localeCompare(keyB)
  }
}

/*
  Return a list of the most recent numeric values present in a `Table`.

  This utility searches any numeric column to find values, and uses the `_time`
  column as their associated timestamp.

  If the table only has one row, then a time column is not needed.
*/
export const latestValues = (table: Table): number[] => {
  const valueColsData = Object.keys(table.columns)
    .sort((a, b) => sortTableKeys(a, b))
    .filter(k => isValueCol(table, k))
    .map(k => table.columns[k].data) as number[][]

  if (!valueColsData.length) {
    return []
  }

  const timeColData = get(
    table,
    'columns._time.data',
    get(table, 'columns._stop.data') // Fallback to `_stop` column if `_time` column missing
  )

  if (!timeColData && table.length !== 1) {
    return []
  }

  const d = i => {
    const time = timeColData[i]

    if (time && valueColsData.some(colData => !isNaN(colData[i]))) {
      return time
    }

    return -Infinity
  }

  const latestRowIndices =
    table.length === 1 ? [0] : maxesBy(range(table.length), d)

  const latestValues = flatMap(latestRowIndices, i =>
    valueColsData.map(colData => colData[i])
  )

  const definedLatestValues = latestValues.filter(x => !isNaN(x))

  return definedLatestValues
}
