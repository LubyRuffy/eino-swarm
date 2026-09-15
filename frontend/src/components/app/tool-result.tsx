import { MemoMarkdown } from "@/components/app/markdown"
import { isMarkdownPath, parseReadResult, type ReadListing } from "@/lib/read-result"
import { viewTool, type SearchHit } from "@/lib/tool-view"

/** Expanded tool output. Built-in tools are shown as what they did — a
 *  command, a query, a file — not the JSON envelope the model spoke. */
export function ToolResultBody({
  name,
  args,
  result,
  failed,
}: {
  name: string
  args: string
  result?: string
  failed?: boolean
}) {
  const listing = name === "read" && result ? parseReadResult(result) : undefined
  const view = viewTool(name, args, result, failed)
  return (
    <div
      className={`space-y-2 border-l-2 pl-3 text-[12px] ${
        view.failed ? "border-destructive" : "border-border"
      }`}
    >
      {result === undefined ? (
        <p className="text-muted-foreground">running…</p>
      ) : listing ? (
        <FileBody listing={listing} failed={view.failed} />
      ) : (
        <ParsedBody view={view} />
      )}
    </div>
  )
}

function ParsedBody({
  view,
}: {
  view: ReturnType<typeof viewTool>
}) {
  return (
    <div className="space-y-2">
      {view.failed && view.error ? (
        <p
          role="alert"
          className="rounded-md bg-destructive/10 px-2 py-1.5 text-destructive"
        >
          {view.error}
        </p>
      ) : null}
      {view.hits ? <SearchHits hits={view.hits} /> : null}
      {view.body ? (
        <pre
          className={`thin-scrollbar max-h-72 overflow-auto rounded bg-muted p-2 font-mono ${
            view.failed && !view.error ? "text-destructive" : ""
          }`}
        >
          {view.body}
        </pre>
      ) : null}
      {!view.body && !view.hits && !view.error ? (
        <p className="text-muted-foreground">(no output)</p>
      ) : null}
    </div>
  )
}

function SearchHits({ hits }: { hits: SearchHit[] }) {
  return (
    <ul className="space-y-2">
      {hits.map((hit, i) => (
        <li key={`${hit.url || hit.title}:${i}`} className="min-w-0">
          <p className="truncate font-medium text-foreground">{hit.title}</p>
          {hit.url ? (
            <p className="truncate font-mono text-[11px] text-muted-foreground">{hit.url}</p>
          ) : null}
          {hit.snippet ? <p className="text-muted-foreground">{hit.snippet}</p> : null}
        </li>
      ))}
    </ul>
  )
}

function FileBody({ listing, failed }: { listing: ReadListing; failed?: boolean }) {
  const empty = listing.lines.length === 0
  return (
    <div
      className={`overflow-hidden rounded bg-muted ${failed ? "text-destructive" : ""}`}
      data-testid="file-body"
    >
      <p className="truncate border-b border-border px-2 py-1 text-[11px] text-muted-foreground">
        {listing.path}
        <span className="opacity-70"> · {listing.encoding}</span>
      </p>
      {empty ? (
        <p className="px-2 py-2 text-muted-foreground">(empty file)</p>
      ) : isMarkdownPath(listing.path) ? (
        <div className="md thin-scrollbar max-h-72 overflow-auto px-3 py-2 [&>:first-child]:mt-0">
          <MemoMarkdown text={listing.body} />
        </div>
      ) : (
        <div className="thin-scrollbar max-h-72 overflow-auto">
          <table className="w-full font-mono">
            <tbody>
              {listing.lines.map((line) => (
                <tr key={line.n} className="align-top">
                  <td className="select-none whitespace-nowrap px-2 py-0 text-right text-muted-foreground">
                    {line.n}
                  </td>
                  <td className="stream-text w-full whitespace-pre-wrap py-0 pr-2">
                    {line.text}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
