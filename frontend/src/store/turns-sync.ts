/** Opening and terminal refreshes share one request order. An older response
 *  must not claim the list before the latest requested snapshot can arrive. */
let generation = 0

export function beginTurnsRead(): number {
  return ++generation
}

export function isCurrentTurnsRead(token: number): boolean {
  return token === generation
}
