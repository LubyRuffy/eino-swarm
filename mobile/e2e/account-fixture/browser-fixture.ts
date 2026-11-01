import { spawn, type ChildProcess } from "node:child_process"
import { closeSync, mkdtempSync, openSync, readFileSync, rmSync, writeFileSync, writeSync } from "node:fs"
import { tmpdir } from "node:os"
import path from "node:path"
import { type TestInfo } from "@playwright/test"

function exited(child: ChildProcess, milliseconds: number): Promise<boolean> {
  if (child.exitCode !== null || child.signalCode !== null) return Promise.resolve(true)
  return new Promise((resolve) => {
    const done = () => { clearTimeout(timer); child.off("exit", done); child.off("error", done); resolve(true) }
    const timer = setTimeout(() => { child.off("exit", done); child.off("error", done); resolve(false) }, milliseconds)
    child.once("exit", done)
    child.once("error", done)
  })
}

async function stop(child: ChildProcess) {
  if (child.exitCode !== null || child.signalCode !== null || !child.pid) return
  if (process.platform === "win32") child.kill("SIGTERM")
  else if (child.pid) process.kill(-child.pid, "SIGTERM")
  if (!await exited(child, 10_000)) throw new Error("owned account fixture did not exit; private data retained")
}

/** Existing real relay/Noise/Eino fixture; only CAS is synthetic. All owned
 * processes stop before removing their private data, on both success and error. */
export async function startAccountFixture(info: TestInfo) {
  const directory = mkdtempSync(path.join(tmpdir(), "zwai-account-upgrade-"))
  const binary = path.join(directory, process.platform === "win32" ? "account-fixture.exe" : "account-fixture")
  const ready = path.join(directory, "ready.json")
  const out = openSync(info.outputPath("account-fixture.stdout.log"), "w", 0o600)
  const err = openSync(info.outputPath("account-fixture.stderr.log"), "w", 0o600)
  const owned: ChildProcess[] = []
  const launch = (command: string, args: string[]) => {
    const child = spawn(command, args, {
      cwd: path.resolve(".."), detached: process.platform !== "win32", stdio: ["ignore", out, err],
      env: { ...process.env, OPENAI_BASE_URL: "", OPENAI_API_KEY: "", OPENAI_MODEL: "" },
    })
    child.once("error", (error) => { writeSync(err, String(error) + "\n") })
    owned.push(child)
    return child
  }
  const cleanup = async () => {
    try {
      for (const child of [...owned].reverse()) await stop(child)
      rmSync(directory, { recursive: true, force: true })
      writeFileSync(info.outputPath("account-fixture-cleanup.json"), JSON.stringify({ status: "stopped", private_data_removed: true, processes: owned.map((child) => ({ pid: child.pid, exit_code: child.exitCode, signal: child.signalCode })) }), { mode: 0o600 })
    } catch (error) {
      writeFileSync(info.outputPath("account-fixture-cleanup.json"), JSON.stringify({ status: "blocked", private_data: directory, error: String(error) }), { mode: 0o600 })
      throw error
    } finally { closeSync(out); closeSync(err) }
  }
  try {
    const build = launch("go", ["build", "-o", binary, "./mobile/e2e/account-fixture"])
    if (!await exited(build, 120_000) || build.exitCode !== 0) throw new Error("account fixture build failed; inspect its preserved stderr")
    const child = launch(binary, ["--data-dir", path.join(directory, "data"), "--ready-file", ready, "--max-duration", "2m"])
    const deadline = Date.now() + 15_000
    while (Date.now() < deadline && child.exitCode === null && child.signalCode === null) {
      try {
        const state = JSON.parse(readFileSync(ready, "utf8")) as { origin: string }
        if (state.origin?.startsWith("http://127.0.0.1:")) return { origin: state.origin, cleanup }
      } catch { /* The CLI publishes after Host.Start; a partial read is retried. */ }
      await new Promise((resolve) => setTimeout(resolve, 50))
    }
    throw new Error("real account fixture never became ready; inspect its preserved stderr")
  } catch (error) {
    try { await cleanup() } catch (cleanupError) { throw new AggregateError([error, cleanupError], "account fixture failed and cleanup remains blocked") }
    throw error
  }
}
