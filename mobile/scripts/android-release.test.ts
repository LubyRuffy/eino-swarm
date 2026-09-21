import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs"
import os from "node:os"
import path from "node:path"
import { describe, expect, it } from "vitest"

import {
  artifactTasks,
  gradleArgs,
  parseAndroidVersion,
  resolveJdkHome,
  resolveSigning,
  runAndroidRelease,
  versionFromEnv,
} from "./android-release.ts"

describe("Android release version", () => {
  it("turns a semver tag into Play versionCode so a store upload is not stuck at 1", () => {
    expect(parseAndroidVersion({ version: "v1.2.3" })).toEqual({
      versionName: "1.2.3",
      versionCode: 10203,
    })
    expect(parseAndroidVersion({ version: "0.1.0-5-gabcdef" }).versionCode).toBe(100)
  })

  it("lets ANDROID_VERSION_CODE override so two sideloads from the same tag still increase", () => {
    expect(parseAndroidVersion({ version: "1.2.3", versionCode: "9" }).versionCode).toBe(9)
  })

  it("refuses a git hash with no versionCode so Play cannot get a random integer", () => {
    expect(() => parseAndroidVersion({ version: "a1b2c3d" })).toThrow(/ANDROID_VERSION_CODE/)
    expect(() => parseAndroidVersion({ version: "" })).toThrow(/empty/)
    expect(() => parseAndroidVersion({ version: "1.0.0", versionCode: "0" })).toThrow(/positive/)
  })

  it("falls back to the phone package version when git describe is not a tag", () => {
    expect(versionFromEnv({ VERSION: "a1b2c3d-dirty" }, "0.1.0")).toEqual({
      versionName: "0.1.0",
      versionCode: 100,
    })
    expect(versionFromEnv({ VERSION: "v1.2.3" }, "0.1.0").versionName).toBe("1.2.3")
    expect(() => versionFromEnv({ ANDROID_VERSION: "a1b2c3d" }, "0.1.0")).toThrow(
      /ANDROID_VERSION_CODE/,
    )
  })
})

describe("Android release signing", () => {
  it("refuses a store upload with no keystore so a debug-signed APK cannot ship as release", () => {
    expect(() =>
      resolveSigning(
        {},
        { fileExists: () => false, defaultPropertiesFile: "missing.properties" },
      ),
    ).toThrow(/ANDROID_KEYSTORE/)
  })

  it("takes keystore env and never puts the password on argv", () => {
    const store = "/tmp/upload.p12"
    const signing = resolveSigning(
      {
        ANDROID_KEYSTORE: store,
        ANDROID_KEYSTORE_PASSWORD: "not-in-git",
        ANDROID_KEY_ALIAS: "upload",
      },
      { fileExists: (p) => p === store, defaultPropertiesFile: "missing.properties" },
    )
    expect(signing).toMatchObject({
      unsigned: false,
      storeFile: store,
      keyAlias: "upload",
    })
    const args = gradleArgs({
      versionName: "1.2.3",
      versionCode: 10203,
      tasks: ["assembleRelease", "bundleRelease"],
    })
    expect(args.join(" ")).toMatch(/assembleRelease/)
    expect(args.join(" ")).toMatch(/bundleRelease/)
    expect(args.join(" ")).toMatch(/-PzwaiVersionName=1\.2\.3/)
    expect(args.join(" ")).toMatch(/-PzwaiVersionCode=10203/)
    expect(args.join(" ")).not.toMatch(/not-in-git/)
    expect(args.join(" ")).not.toMatch(/PASSWORD/)
  })

  it("treats ANDROID_UNSIGNED as a sideload APK only, never a Play bundle", () => {
    expect(
      resolveSigning(
        { ANDROID_UNSIGNED: "1" },
        { fileExists: () => false, defaultPropertiesFile: "x" },
      ),
    ).toEqual({ unsigned: true })
    expect(artifactTasks("both", true)).toEqual(["assembleRelease"])
    expect(artifactTasks("aab", false)).toEqual(["bundleRelease"])
    expect(artifactTasks("apk", false)).toEqual(["assembleRelease"])
    expect(artifactTasks("both", false)).toEqual(["assembleRelease", "bundleRelease"])
  })
})

describe("Android release JDK", () => {
  it("skips JAVA_HOME 17 and uses a 21+ home so Capacitor compileOptions do not blow up", () => {
    const home = resolveJdkHome(
      { JAVA_HOME: "/opt/java17" },
      {
        javaMajor: (h) => (h === "/opt/java21" ? 21 : 17),
        javaHomeFor: (v) => (v === "21" ? "/opt/java21" : null),
        brewPrefix: () => null,
      },
    )
    expect(home).toBe("/opt/java21")
  })

  it("fails when no JDK 21 exists instead of letting Gradle print a 200-line toolchain dump", () => {
    expect(() =>
      resolveJdkHome(
        { JAVA_HOME: "/opt/java17" },
        { javaMajor: () => 17, javaHomeFor: () => null, brewPrefix: () => null },
      ),
    ).toThrow(/JDK 21/)
  })
})

describe("Android release runner", () => {
  it("runs gradlew with the injected JDK and copies the APK into bin/", async () => {
    const tmp = mkdtempSync(path.join(os.tmpdir(), "zwai-android-release-"))
    const androidDir = path.join(tmp, "android")
    const apkDir = path.join(androidDir, "app/build/outputs/apk/release")
    mkdirSync(apkDir, { recursive: true })
    const apk = path.join(apkDir, "app-release.apk")
    writeFileSync(apk, "apk")
    const repoRoot = path.join(tmp, "repo")
    mkdirSync(repoRoot)

    const calls: { cmd: string; args: string[]; env: NodeJS.ProcessEnv }[] = []
    const result = await runAndroidRelease({
      env: {
        VERSION: "1.4.0",
        ANDROID_UNSIGNED: "1",
        JAVA_HOME: "/opt/java21",
      },
      androidDir,
      repoRoot,
      exec: async (cmd, args, opts) => {
        calls.push({ cmd, args, env: opts.env })
        return { code: 0 }
      },
      probes: {
        javaMajor: () => 21,
        javaHomeFor: () => null,
        brewPrefix: () => null,
      },
      fileExists: (p) => p === apk,
    })

    expect(calls).toHaveLength(1)
    expect(calls[0].cmd).toMatch(/gradlew/)
    expect(calls[0].env.JAVA_HOME).toBe("/opt/java21")
    expect(calls[0].args).toContain("assembleRelease")
    expect(calls[0].args).not.toContain("bundleRelease")
    expect(result.artifacts).toEqual([path.join(repoRoot, "bin/zwai-1.4.0-android.apk")])
  })
})
