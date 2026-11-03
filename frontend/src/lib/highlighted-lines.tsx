import { useHighlightedCode } from "./use-highlighted-code"

/** Fence body. Line breaks stay in the text, including one trailing newline,
 *  so copying a selection and reading the DOM keep the original characters. */
export function HighlightedLines({ text, lang }: { text: string; lang: string }) {
  const lines = useHighlightedCode(text, lang)
  const trailing = text.endsWith("\n")
  return (
    <>
      {lines.map((line, index) => (
        <span key={index}>
          {line}
          {index < lines.length - 1 ? "\n" : null}
        </span>
      ))}
      {trailing ? "\n" : null}
    </>
  )
}
