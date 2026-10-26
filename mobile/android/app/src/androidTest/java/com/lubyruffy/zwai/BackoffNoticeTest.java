package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.Bundle;
import android.os.SystemClock;
import android.system.Os;
import android.system.OsConstants;
import android.view.accessibility.AccessibilityNodeInfo;
import android.webkit.WebView;
import android.util.Base64;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.ByteArrayOutputStream;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Uses the installed APK's unchanged assets; only its runtime URL selects offline input. */
@RunWith(AndroidJUnit4.class)
public class BackoffNoticeTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();

    @Test
    public void candidateRecoveryAndOriginCooldownStayDistinctWhenReopened() throws Exception {
        String locale = InstrumentationRegistry.getArguments().getString("backoffLocale", "en");
        assertTrue("The fixture locale must be en or zh", "en".equals(locale) || "zh".equals(locale));
        boolean chinese = "zh".equals(locale);
        String[] notices = chinese ? new String[] {
            "等待 5 秒后重试模型请求。", "等待 10 秒后重试模型请求。",
            "等待 5 秒后重试未发布答复的核对。", "答复核对暂不可用，未发布候选已保留。"
        } : new String[] {
            "Waiting 5s before retrying the model request.", "Waiting 10s before retrying the model request.",
            "Waiting 5s before checking the unpublished answer again.",
            "Answer verification is unavailable. The unpublished answer is retained."
        };
        Intent intent = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull("The installed package needs its real launcher", intent);
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity activity = instrumentation.startActivitySync(intent);
        assertTrue("The test must drive the packaged Capacitor activity", activity instanceof MainActivity);
        try {
            selectOfflineInput((MainActivity) activity, locale, "answer-backoff");
            for (String phase : new String[] { "first-open", "reopened" }) {
                waitFor(notices[3], false);
                List<AccessibilityNodeInfo> visible = nodes(ui.getRootInActiveWindow());
                for (String notice : notices) {
                    int count = 0;
                    for (AccessibilityNodeInfo node : visible) {
                        if (node.isVisibleToUser() && named(node, notice)) count++;
                    }
                    assertEquals("Each cooldown/candidate notice must appear exactly once: " + notice, 1, count);
                }
                for (AccessibilityNodeInfo node : visible) {
                    if (!node.isVisibleToUser()) continue;
                    String text = String.valueOf(node.getText()) + " " + node.getContentDescription();
                    for (String forbidden : new String[] {
                        "Waiting before retrying the model request.", "Waiting 4s before retrying the model request.",
                        "等待后重试模型请求。", "等待 4 秒后重试模型请求。",
                        "schedule-answer-policy", "private-answer", "wait_ms", "Agents (", "子 Agent ("
                    }) assertFalse("Transport bookkeeping cannot become extra chat rows or workers: " + forbidden, text.contains(forbidden));
                }
                saveEvidence(locale, phase);
                click(waitFor(chinese ? "返回" : "Back", true));
                click(waitFor((chinese ? "打开 " : "Open ") + "Trim the layout pass", true));
            }
        } catch (Exception | AssertionError failure) {
            try { saveEvidence(locale, "failure"); }
            catch (Exception evidenceFailure) { failure.addSuppressed(evidenceFailure); }
            throw failure;
        } finally {
            instrumentation.runOnMainSync(activity::finish);
        }
    }

    /** The public old APK is a control for runtime failures before new-notice assertions. */
    @Test
    public void legacyModelErrorControlKeepsItsStatusWhenReopened() throws Exception {
        String locale = InstrumentationRegistry.getArguments().getString("backoffLocale", "en");
        assertTrue("The fixture locale must be en or zh", "en".equals(locale) || "zh".equals(locale));
        boolean chinese = "zh".equals(locale);
        Intent intent = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(intent);
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity activity = instrumentation.startActivitySync(intent);
        assertTrue(activity instanceof MainActivity);
        try {
            selectOfflineInput((MainActivity) activity, locale, "api-error");
            click(waitFor(chinese ? "返回" : "Back", true));
            for (String phase : new String[] { "legacy-first-open", "legacy-reopened" }) {
                click(waitFor((chinese ? "打开 " : "Open ") + "Sweep the unused exports", true));
                waitFor("model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input", false);
                saveEvidence(locale, phase);
                click(waitFor(chinese ? "返回" : "Back", true));
            }
        } catch (Exception | AssertionError failure) {
            try { saveEvidence(locale, "legacy-failure"); }
            catch (Exception evidenceFailure) { failure.addSuppressed(evidenceFailure); }
            throw failure;
        } finally {
            instrumentation.runOnMainSync(activity::finish);
        }
    }

    private void selectOfflineInput(MainActivity activity, String locale, String history) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 20000;
        WebView[] ready = new WebView[1];
        while (SystemClock.uptimeMillis() < deadline && ready[0] == null) {
            instrumentation.runOnMainSync(() -> {
                WebView web = activity.getBridge() == null ? null : activity.getBridge().getWebView();
                if (web != null && web.getUrl() != null && web.getProgress() == 100) ready[0] = web;
            });
            if (ready[0] == null) SystemClock.sleep(100);
        }
        assertNotNull("The installed asset page must finish loading", ready[0]);
        String expectedSavedLocale = InstrumentationRegistry.getArguments().getString("expectedSavedBackoffLocale");
        if (expectedSavedLocale != null) {
            CountDownLatch observed = new CountDownLatch(1);
            String[] stored = new String[1];
            instrumentation.runOnMainSync(() -> ready[0].evaluateJavascript(
                "localStorage.getItem('zwai.phone.locale')", result -> { stored[0] = result; observed.countDown(); }));
            assertTrue("Read the existing upgrade state before selecting this case", observed.await(5, TimeUnit.SECONDS));
            assertEquals("An in-place APK upgrade must retain the previously visible locale", JSONObject.quote(expectedSavedLocale), stored[0]);
        }
        CountDownLatch selected = new CountDownLatch(1);
        instrumentation.runOnMainSync(() -> ready[0].evaluateJavascript(
            "localStorage.setItem('zwai.phone.locale'," + JSONObject.quote(locale)
                + ");location.assign(" + JSONObject.quote("/?mock=1&tick=0&history=" + history) + ");true",
            result -> selected.countDown()));
        assertTrue("Select offline input once; never repeat an unacknowledged navigation", selected.await(5, TimeUnit.SECONDS));
    }

    private AccessibilityNodeInfo waitFor(String label, boolean button) {
        long deadline = SystemClock.uptimeMillis() + 20000;
        while (SystemClock.uptimeMillis() < deadline) {
            for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
                if (node.isVisibleToUser() && named(node, label)
                    && (!button || "android.widget.Button".equals(String.valueOf(node.getClassName())))) return node;
            }
            SystemClock.sleep(100);
        }
        throw new AssertionError("The packaged app did not reach " + label);
    }

    private void click(AccessibilityNodeInfo node) {
        assertTrue("A native control must be enabled", node.isEnabled());
        assertTrue("A native click must reach the packaged handler", node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.contentEquals(node.getText() == null ? "" : node.getText())
            || label.contentEquals(node.getContentDescription() == null ? "" : node.getContentDescription());
    }

    private List<AccessibilityNodeInfo> nodes(AccessibilityNodeInfo root) {
        List<AccessibilityNodeInfo> result = new ArrayList<>();
        if (root != null) result.add(root);
        for (int i = 0; i < result.size(); i++) {
            AccessibilityNodeInfo node = result.get(i);
            for (int j = 0; j < node.getChildCount(); j++) {
                AccessibilityNodeInfo child = node.getChild(j);
                if (child != null) result.add(child);
            }
        }
        return result;
    }

    private void saveEvidence(String locale, String phase) throws Exception {
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "backoff-notice-" + locale);
        assertTrue(directory.isDirectory() || directory.mkdirs());
        Bitmap screenshot = ui.takeScreenshot();
        assertNotNull("The actual reached screen must be captured", screenshot);
        try (FileOutputStream out = privateOutput(new File(directory, phase + ".png"))) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, out));
        } finally { screenshot.recycle(); }
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName()))
                .put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription()))
                .put("visible", node.isVisibleToUser()).put("bounds", bounds.flattenToString()));
        }
        JSONObject assets = new JSONObject();
        hashAssets("public", assets);
        String apk = instrumentation.getTargetContext().getApplicationInfo().sourceDir;
        JSONObject receipt = new JSONObject().put("locale", locale).put("phase", phase)
            .put("apk_sha256", hash(new FileInputStream(apk))).put("bundle_sha256", assets).put("nodes", tree);
        try (FileOutputStream out = privateOutput(new File(directory, phase + ".json"))) {
            out.write(receipt.toString(2).getBytes(StandardCharsets.UTF_8));
        }
        if ("1".equals(InstrumentationRegistry.getArguments().getString("exportBackoffEvidence"))) {
            // Release packages cannot use run-as. Export from their actual
            // application UID without making private evidence world-readable.
            for (String extension : new String[] { ".png", ".json" }) {
                File file = new File(directory, phase + extension);
                ByteArrayOutputStream bytes = new ByteArrayOutputStream();
                try (FileInputStream input = new FileInputStream(file)) {
                    byte[] buffer = new byte[16384]; int length;
                    while ((length = input.read(buffer)) != -1) bytes.write(buffer, 0, length);
                }
                Bundle exported = new Bundle();
                exported.putString("backoff_evidence_name", file.getName());
                exported.putString("backoff_evidence_locale", locale);
                exported.putString("backoff_evidence_base64", Base64.encodeToString(bytes.toByteArray(), Base64.NO_WRAP));
                instrumentation.sendStatus(2, exported);
            }
        }
    }

    private FileOutputStream privateOutput(File file) throws Exception {
        return new FileOutputStream(Os.open(file.getAbsolutePath(), OsConstants.O_WRONLY | OsConstants.O_CREAT | OsConstants.O_TRUNC, 0600));
    }

    private void hashAssets(String path, JSONObject hashes) throws Exception {
        String[] children = instrumentation.getTargetContext().getAssets().list(path);
        if (children != null && children.length > 0) {
            for (String child : children) hashAssets(path + "/" + child, hashes);
        } else hashes.put(path, hash(instrumentation.getTargetContext().getAssets().open(path)));
    }

    private String hash(InputStream input) throws Exception {
        MessageDigest sha = MessageDigest.getInstance("SHA-256");
        try (InputStream in = input) {
            byte[] buffer = new byte[16384]; int length;
            while ((length = in.read(buffer)) != -1) sha.update(buffer, 0, length);
        }
        StringBuilder result = new StringBuilder();
        for (byte b : sha.digest()) result.append(String.format("%02x", b & 0xff));
        return result.toString();
    }
}
