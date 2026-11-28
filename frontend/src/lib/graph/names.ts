/** An other-subject name that explains work instead of naming a thing.
 *  A colon, an equals sign, a parenthesis or a space makes it a phrase. The
 *  same characters are refused when the name is written. */
export function explanationName(name: string): boolean {
  return /[:：=＝()（）]/.test(name) || /\s/u.test(name)
}
