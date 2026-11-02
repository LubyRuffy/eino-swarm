import { createServer, type ServerResponse } from "node:http"

type Message = { role: string; content?: string; tool_call_id?: string }
type Input = {
  model?: string
  messages: Message[]
  tools?: { function: { name: string } }[]
  stream?: boolean
}
type NodeReceipt = { ok: boolean; node: { id: string; version: number } }

/** Real model HTTP → Registry tool → durable project nodes → snapshot/SSE.
 * This is protocol evidence, not a measurement of autonomous model reasoning. */
export async function startNuwaFixture() {
  const errors: string[] = []
  const workerModels: string[] = []
  let taskNode: { id: string; version: number } | undefined
  let workerFact: { id: string; version: number } | undefined
  let threadID = "",
    sourceTurn = ""
  let releaseTask!: () => void
  const taskGate = new Promise<void>((resolve) => {
    releaseTask = resolve
  })
  let revision: { id: string; version: number } | undefined
  const reply = (response: ServerResponse, input: Input, delta: object, finish = "stop") => {
    response.writeHead(200, {
      "Content-Type": input.stream ? "text/event-stream" : "application/json",
    })
    response.end(
      input.stream
        ? `data: ${JSON.stringify({ id: "nuwa-fixture", choices: [{ index: 0, delta, finish_reason: null }] })}\n\ndata: ${JSON.stringify({ id: "nuwa-fixture", choices: [{ index: 0, delta: {}, finish_reason: finish }] })}\n\ndata: [DONE]\n\n`
        : JSON.stringify({ choices: [{ index: 0, message: delta, finish_reason: finish }] })
    )
  }
  const call = (
    response: ServerResponse,
    input: Input,
    id: string,
    args: object,
    name = "record_work"
  ) =>
    reply(
      response,
      input,
      {
        role: "assistant",
        tool_calls: [
          {
            index: 0,
            id,
            type: "function",
            function: { name, arguments: JSON.stringify(args) },
          },
        ],
      },
      "tool_calls"
    )
  const endpoint = createServer((request, response) => {
    if (request.method === "GET") {
      response.writeHead(200, { "Content-Type": "application/json" })
      response.end(JSON.stringify({ data: [{ id: "nuwa-fixture" }, { id: "nuwa-worker" }] }))
      return
    }
    let raw = ""
    request.setEncoding("utf8")
    request.on("data", (part: string) => {
      raw += part
    })
    request.on("end", async () => {
      try {
        if (request.headers.authorization) throw new Error("Nuwa fixture must remain keyless")
        const input = JSON.parse(raw) as Input
        if (input.messages[0]?.content?.startsWith("## Schedule answer claims")) {
          reply(response, input, { role: "assistant", content: JSON.stringify({ claims: [] }) })
          return
        }
        if (!input.tools?.some((tool) => tool.function.name === "record_work"))
          throw new Error("record_work absent at real model boundary")
        const result = (id: string) => {
          const message = input.messages.findLast(
            (message) => message.role === "tool" && message.tool_call_id === id
          )
          if (!message) return undefined
          const receipt = JSON.parse(message.content ?? "{}") as NodeReceipt
          if (!receipt.ok || !receipt.node?.id)
            throw new Error(`Node write refused: ${message.content}`)
          return receipt.node
        }
        if (input.model === "nuwa-worker") {
          workerModels.push(input.model)
          if (!taskNode) throw new Error("Worker started before its shared task")
          const fact = result("nuwa-fact")
          if (!fact) {
            call(response, input, "nuwa-fact", {
              kind: "fact",
              title: "Measured finding",
              summary: "The input supports a bounded conclusion",
              detail: "Detailed observation with its original source.",
              status: "reported",
              depends_on: [taskNode.id],
              evidence: [
                { thread_id: threadID, turn_id: sourceTurn, seq: 1, label: "Original task input" },
              ],
            })
          } else {
            workerFact = fact
            reply(response, input, {
              role: "assistant",
              content: "The independent observation is recorded with source evidence.",
            })
          }
          return
        }
        if (revision) {
          if (!result("nuwa-revise"))
            call(response, input, "nuwa-revise", {
              ...revision,
              kind: "fact",
              title: "Measured finding",
              summary: "A later check superseded the initial result",
              status: "superseded",
              detail: "The earlier result remains in history.",
            })
          else
            reply(response, input, {
              role: "assistant",
              content: "The shared result has been revised.",
            })
          return
        }
        const task = result("nuwa-task")
        if (!task) {
          call(response, input, "nuwa-task", {
            kind: "task",
            title: "Compare the evidence",
            summary: "Inspect and compare the supplied material",
            status: "running",
          })
          return
        }
        taskNode = task
        await taskGate
        const catalogMessage = input.messages.findLast(
          (message) => message.role === "tool" && message.tool_call_id === "nuwa-models"
        )
        if (!catalogMessage) {
          call(response, input, "nuwa-models", {}, "list_models")
          return
        }
        const catalog = JSON.parse(catalogMessage.content ?? "{}") as {
          models?: { ref: string; provider_id: string; model: string }[]
        }
        const chosen = catalog.models?.find(
          (model) => model.provider_id === "nuwa-worker-endpoint" && model.model === "nuwa-worker"
        )
        if (!chosen) throw new Error("The configured independent model did not reach list_models")
        const spawned = input.messages.findLast(
          (message) => message.role === "tool" && message.tool_call_id === "nuwa-spawn"
        )
        if (!spawned) {
          call(
            response,
            input,
            "nuwa-spawn",
            {
              role: "independent-review",
              task: `Review the supplied evidence for shared task ${task.id}; record the bounded observation and cite its source.`,
              model_ref: chosen.ref,
            },
            "spawn_agent"
          )
          return
        }
        const started = JSON.parse(spawned.content ?? "{}") as { agent_id?: string; error?: string }
        if (!started.agent_id || started.error)
          throw new Error(`Worker spawn refused: ${spawned.content}`)
        const waited = input.messages.findLast(
          (message) => message.role === "tool" && message.tool_call_id === "nuwa-wait"
        )
        if (!waited) {
          call(
            response,
            input,
            "nuwa-wait",
            { agent_ids: [started.agent_id], timeout_s: 30 },
            "wait_agents"
          )
          return
        }
        const settled = JSON.parse(waited.content ?? "{}") as {
          agents?: { agent_id: string; status: string }[]
        }
        if (
          !settled.agents?.some(
            (agent) => agent.agent_id === started.agent_id && agent.status === "done"
          ) ||
          !workerFact
        )
          throw new Error(`Actual worker result was not collected: ${waited.content}`)
        const fact = workerFact
        const written = input.messages.findLast(
          (message) => message.role === "tool" && message.tool_call_id === "nuwa-write"
        )
        if (!written) {
          call(
            response,
            input,
            "nuwa-write",
            {
              file_path: "review-notes.md",
              content: "# Review notes\n\nThe conclusion is limited to the measured input.\n",
            },
            "write"
          )
          return
        }
        if (written.content?.startsWith("error:"))
          throw new Error(`Artifact write failed: ${written.content}`)
        if (!result("nuwa-artifact")) {
          call(response, input, "nuwa-artifact", {
            kind: "artifact",
            title: "Review notes",
            summary: "A result ready for independent review",
            status: "reported",
            depends_on: [fact.id],
            artifact_path: "review-notes.md",
            detail: "# Review notes\n\nThe conclusion is limited to the measured input.",
          })
          return
        }
        if (!result("nuwa-task-done")) {
          call(response, input, "nuwa-task-done", {
            id: task.id,
            version: task.version,
            kind: "task",
            title: "Compare the evidence",
            summary: "The evidence was compared and review notes produced",
            status: "done",
          })
          return
        }
        reply(response, input, {
          role: "assistant",
          content: "Shared work is recorded with its evidence.",
        })
      } catch (error) {
        errors.push(String(error))
        response.writeHead(400, { "Content-Type": "application/json" })
        response.end(
          JSON.stringify({ error: { message: String(error), type: "invalid_request_error" } })
        )
      }
    })
  })
  await new Promise<void>((resolve, reject) => {
    endpoint.once("error", reject)
    endpoint.listen(0, "127.0.0.1", resolve)
  })
  const address = endpoint.address()
  if (!address || typeof address === "string") throw new Error("Nuwa fixture did not listen")
  return {
    url: `http://127.0.0.1:${address.port}`,
    errors,
    workerModels,
    source: (thread: string, turn: string) => {
      threadID = thread
      sourceTurn = turn
    },
    releaseTask,
    revise: (node: { id: string; version: number }) => {
      revision = node
    },
    close: () =>
      new Promise<void>((resolve, reject) => {
        releaseTask()
        endpoint.close((error) => (error ? reject(error) : resolve()))
        endpoint.closeAllConnections()
      }),
  }
}
