// The polling APIs return JSON metadata. Retain unchanged records (including
// their nested histories) so Vue does not redraw every card on each refresh.
export function reconcileMonitorData<T>(previous: T | undefined, incoming: T): T {
  if (Object.is(previous, incoming)) return previous as T
  if (!previous || !incoming || typeof previous !== 'object' || typeof incoming !== 'object') return incoming
  if (Array.isArray(previous) && Array.isArray(incoming)) {
    const keyed = previous.every(item => item && typeof item === 'object' && typeof item.id === 'number')
      && incoming.every(item => item && typeof item === 'object' && typeof item.id === 'number')
    const byID = keyed ? new Map(previous.map(item => [item.id, item])) : undefined
    let identical = previous.length === incoming.length
    const result = incoming.map((item, index) => {
      const value = reconcileMonitorData(byID ? byID.get(item.id) : previous[index], item)
      if (value !== previous[index]) identical = false
      return value
    })
    return (identical ? previous : result) as T
  }
  if (Array.isArray(previous) || Array.isArray(incoming)) return incoming
  const old = previous as Record<string, unknown>, next = incoming as Record<string, unknown>
  const keys = Object.keys(next)
  let identical = Object.keys(old).length === keys.length
  const result: Record<string, unknown> = {}
  for (const key of keys) {
    result[key] = reconcileMonitorData(old[key], next[key])
    if (!Object.prototype.hasOwnProperty.call(old, key) || result[key] !== old[key]) identical = false
  }
  return (identical ? previous : result) as T
}
