#!/usr/bin/env node
// Capacitor compileOptions are Java 21. Passwords stay in env / keystore.properties
// so they never land on argv (ps) or in git. Version is injected as Gradle -P.
import { spawn, spawnSync } from "node:child_process"
import { copyFileSync, existsSync, mkdirSync, readFileSync } from "node:fs"
import path from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"

const MAX_VERSION_CODE = 2_100_000_000

export type AndroidVersion = { versionName: string; versionCode: number }

export type JdkProbes = {
  javaMajor: (home: string) => number
  javaHomeFor?: (version: string) => string | null
  brewPrefix?: (formula: string) => string | null
}

export type GradleExec = (
  cmd: string,
  args: string[],
  opts: { cwd: string; env: NodeJS.ProcessEnv },
) => Promise<{ code: number }>

export type Signing =
  | { unsigned: true }
  | {
      unsigned: false
      storeFile?: string
      storePassword?: string
      keyAlias?: string
      keyPassword?: string
      propertiesFile?: string
    }

export function parseAndroidVersion({
  version,
  versionCode,
}: { version?: string; versionCode?: string } = {}): AndroidVersion {
  const versionName = String(version ?? "")
    .trim()
    .replace(/^v/i, "")
  if (!versionName) {
    throw new Error("ANDROID_VERSION or VERSION is empty")
  }
  if (versionCode != null && String(versionCode).trim() !== "") {
    const n = Number(versionCode)
    if (!Number.isInteger(n) || n < 1) {
      throw new Error("ANDROID_VERSION_CODE must be a positive integer")
    }
    return { versionName, versionCode: n }
  }
  const m = versionName.match(/^(\d+)\.(\d+)\.(\d+)/)
  if (!m) {
    throw new Error("non-semver version needs ANDROID_VERSION_CODE")
  }
  const code = Number(m[1]) * 10000 + Number(m[2]) * 100 + Number(m[3])
  if (code > MAX_VERSION_CODE) {
    throw new Error("versionCode exceeds Play max")
  }
  return { versionName, versionCode: code }
}

export function versionFromEnv(env: NodeJS.ProcessEnv, fallbackName?: string): AndroidVersion {
  const explicit = env.ANDROID_VERSION
  const version = explicit || env.VERSION || fallbackName
  try {
    return parseAndroidVersion({ version, versionCode: env.ANDROID_VERSION_CODE })
  } catch (err) {
    if (!explicit && fallbackName && version !== fallbackName) {
      return parseAndroidVersion({ version: fallbackName, versionCode: env.ANDROID_VERSION_CODE })
    }
    throw err
  }
}

function truthy(v: string | undefined): boolean {
  return v === "1" || v === "true" || v === "yes"
}

export function resolveSigning(
  env: NodeJS.ProcessEnv,
  opts: { fileExists: (p: string) => boolean; defaultPropertiesFile: string },
): Signing {
  if (truthy(env.ANDROID_UNSIGNED)) {
    return { unsigned: true }
  }
  const storeFile = env.ANDROID_KEYSTORE
  const storePassword = env.ANDROID_KEYSTORE_PASSWORD
  const keyAlias = env.ANDROID_KEY_ALIAS
  const keyPassword = env.ANDROID_KEY_PASSWORD || storePassword
  if (storeFile && storePassword && keyAlias) {
    if (!opts.fileExists(storeFile)) {
      throw new Error(`keystore not found: ${storeFile}`)
    }
    return { unsigned: false, storeFile, storePassword, keyAlias, keyPassword }
  }
  if (opts.defaultPropertiesFile && opts.fileExists(opts.defaultPropertiesFile)) {
    return { unsigned: false, propertiesFile: opts.defaultPropertiesFile }
  }
  throw new Error(
    "set ANDROID_KEYSTORE, ANDROID_KEYSTORE_PASSWORD and ANDROID_KEY_ALIAS (or ANDROID_UNSIGNED=1 for a sideload APK)",
  )
}

export function artifactTasks(kind: string | undefined, unsigned: boolean): string[] {
  if (unsigned) return ["assembleRelease"]
  if (kind === "apk") return ["assembleRelease"]
  if (kind === "aab") return ["bundleRelease"]
  return ["assembleRelease", "bundleRelease"]
}

export function gradleArgs(opts: {
  versionName: string
  versionCode: number
  tasks: string[]
}): string[] {
  return [
    "--stacktrace",
    ...opts.tasks,
    `-PzwaiVersionName=${opts.versionName}`,
    `-PzwaiVersionCode=${String(opts.versionCode)}`,
  ]
}

export function resolveJdkHome(env: NodeJS.ProcessEnv, probes: JdkProbes): string {
  const candidates: string[] = []
  if (env.JAVA_HOME) candidates.push(env.JAVA_HOME)
  const v21 = probes.javaHomeFor?.("21")
  if (v21) candidates.push(v21)
  const brew = probes.brewPrefix?.("openjdk@21")
  if (brew) candidates.push(brew)
  const viable: { home: string; major: number }[] = []
  for (const home of candidates) {
    const major = probes.javaMajor(home)
    if (major >= 21) viable.push({ home, major })
  }
  if (viable.length === 0) {
    throw new Error("Android release needs JDK 21+")
  }
  viable.sort((a, b) => Math.abs(a.major - 21) - Math.abs(b.major - 21))
  return viable[0].home
}

function existingApk(androidDir: string, fileExists: (p: string) => boolean): string {
  const dir = path.join(androidDir, "app/build/outputs/apk/release")
  for (const name of ["app-release.apk", "app-release-unsigned.apk"]) {
    const p = path.join(dir, name)
    if (fileExists(p)) return p
  }
  return path.join(dir, "app-release.apk")
}

function filenameVersion(versionName: string): string {
  return versionName.replace(/[/:]/g, "-")
}

function readPackageVersion(androidDir: string): string {
  try {
    const pkg = JSON.parse(readFileSync(path.join(androidDir, "../package.json"), "utf8")) as {
      version?: unknown
    }
    return typeof pkg.version === "string" ? pkg.version : ""
  } catch {
    return ""
  }
}

export async function runAndroidRelease(opts: {
  env: NodeJS.ProcessEnv
  androidDir: string
  repoRoot: string
  exec: GradleExec
  probes: JdkProbes
  fileExists?: (p: string) => boolean
  packageVersion?: string
}): Promise<{ artifacts: string[]; versionName: string; versionCode: number; javaHome: string }> {
  const fileExists = opts.fileExists ?? existsSync
  const { versionName, versionCode } = versionFromEnv(
    opts.env,
    opts.packageVersion ?? readPackageVersion(opts.androidDir),
  )
  const signing = resolveSigning(opts.env, {
    fileExists,
    defaultPropertiesFile: path.join(opts.androidDir, "keystore.properties"),
  })
  const javaHome = resolveJdkHome(opts.env, opts.probes)
  const kind = opts.env.ANDROID_ARTIFACT || "both"
  const tasks = artifactTasks(kind, signing.unsigned)
  const args = gradleArgs({ versionName, versionCode, tasks })
  const gradlew = path.join(
    opts.androidDir,
    process.platform === "win32" ? "gradlew.bat" : "gradlew",
  )
  const result = await opts.exec(gradlew, args, {
    cwd: opts.androidDir,
    env: { ...opts.env, JAVA_HOME: javaHome },
  })
  if (result.code !== 0) {
    throw new Error(`gradlew exited ${result.code}`)
  }
  const destDir = path.join(opts.repoRoot, "bin")
  mkdirSync(destDir, { recursive: true })
  const artifacts: string[] = []
  const tag = filenameVersion(versionName)
  if (tasks.includes("assembleRelease")) {
    const src = existingApk(opts.androidDir, fileExists)
    if (!fileExists(src)) {
      throw new Error(`release APK missing: ${src}`)
    }
    const dest = path.join(destDir, `zwai-${tag}-android.apk`)
    copyFileSync(src, dest)
    artifacts.push(dest)
  }
  if (tasks.includes("bundleRelease")) {
    const src = path.join(opts.androidDir, "app/build/outputs/bundle/release/app-release.aab")
    if (!fileExists(src)) {
      throw new Error(`release AAB missing: ${src}`)
    }
    const dest = path.join(destDir, `zwai-${tag}-android.aab`)
    copyFileSync(src, dest)
    artifacts.push(dest)
  }
  return { artifacts, versionName, versionCode, javaHome }
}

function javaMajor(home: string): number {
  const java = path.join(home, "bin", process.platform === "win32" ? "java.exe" : "java")
  const r = spawnSync(java, ["-version"], { encoding: "utf8" })
  const text = `${r.stdout ?? ""}\n${r.stderr ?? ""}`
  const m = text.match(/version "(\d+)/)
  return m ? Number(m[1]) : 0
}

export function defaultProbes(): JdkProbes {
  return {
    javaMajor,
    javaHomeFor(v) {
      const tool = "/usr/libexec/java_home"
      if (!existsSync(tool)) return null
      const r = spawnSync(tool, ["-v", v], { encoding: "utf8" })
      if (r.status !== 0) return null
      const home = (r.stdout || "").trim()
      return home || null
    },
    brewPrefix(formula) {
      const r = spawnSync("brew", ["--prefix", formula], { encoding: "utf8" })
      if (r.status !== 0) return null
      const prefix = (r.stdout || "").trim()
      return prefix || null
    },
  }
}

function execInherit(
  cmd: string,
  args: string[],
  opts: { cwd: string; env: NodeJS.ProcessEnv },
): Promise<{ code: number }> {
  return new Promise((resolve, reject) => {
    const child = spawn(cmd, args, {
      cwd: opts.cwd,
      env: { ...process.env, ...opts.env },
      stdio: "inherit",
    })
    child.on("error", reject)
    child.on("close", (code) => resolve({ code: code ?? 1 }))
  })
}

function invokedAsCli(): boolean {
  const entry = process.argv[1]
  if (!entry) return false
  return import.meta.url === pathToFileURL(path.resolve(entry)).href
}

async function main(): Promise<void> {
  const here = path.dirname(fileURLToPath(import.meta.url))
  const androidDir = path.resolve(here, "../android")
  const repoRoot = path.resolve(here, "../..")
  const out = await runAndroidRelease({
    env: process.env,
    androidDir,
    repoRoot,
    exec: execInherit,
    probes: defaultProbes(),
  })
  console.log(`Android ${out.versionName} (${out.versionCode})`)
  for (const artifact of out.artifacts) {
    console.log(artifact)
  }
}

if (invokedAsCli()) {
  main().catch((err: unknown) => {
    console.error(err instanceof Error ? err.message : err)
    process.exit(1)
  })
}
