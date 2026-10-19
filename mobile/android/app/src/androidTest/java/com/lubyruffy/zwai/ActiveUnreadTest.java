package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.SystemClock;
import android.os.Bundle;
import android.view.accessibility.AccessibilityNodeInfo;
import android.webkit.WebView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.getcapacitor.BridgeActivity;
import java.io.File;
import java.io.FileOutputStream;
import java.io.FileInputStream;
import java.security.MessageDigest;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Native Home walkthrough on a disposable history=active-unread packaged app. */
@RunWith(AndroidJUnit4.class)
public class ActiveUnreadTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private BridgeActivity activity;

    @Test
    public void nativeActiveUnreadHomeProjectionKeepsOldWatermarks() throws Exception {
        JSONObject identity = new JSONObject()
            .put("driver", "issue83-packaged-webview-read-only-v3")
            .put("test_class", getClass().getName())
            .put("loaded_classloader", getClass().getClassLoader().toString())
            .put("installed_test_apk", instrumentation.getContext().getPackageCodePath())
            .put("installed_test_apk_sha256", sha256(instrumentation.getContext().getPackageCodePath()))
            .put("installed_target_apk_sha256", sha256(instrumentation.getTargetContext().getPackageCodePath()));
        Bundle status = new Bundle();
        status.putString("driver_identity", identity.toString());
        instrumentation.sendStatus(2, status);
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity reached = instrumentation.startActivitySync(launch);
        assertTrue(reached instanceof BridgeActivity);
        activity = (BridgeActivity) reached;
        try {
            assertTrue(waitRows("Shared task", 1, 20000));
            assertLive();
            assertEquals(0, rows("Pending answer"));
            assertEquals(0, rows("Scheduled task"));
            assertFalse(dom().getBoolean("back"));
            saveEvidence("active-unread-first-page");
            clickFirstMore();
            assertTrue(waitRows("Completed item 2", 1, 8000));
            // Observe a later normal inbox poll without changing host state.
            SystemClock.sleep(2200);
            assertLive();
            assertEquals(1, rows("Shared task"));
            assertEquals(0, rows("Pending answer"));
            assertEquals(0, rows("Scheduled task"));
            saveEvidence("active-unread-expanded-live");
            clickFirstMore();
            assertTrue("Old unread watermarks must survive hiding without a read ACK",
                waitRows("Shared task", 2, 20000));
            assertEquals(1, rows("Pending answer"));
            assertEquals(1, rows("Scheduled task"));
            JSONObject idle = dom();
            assertEquals(0, idle.getJSONArray("live").length());
            assertFalse("The test must remain on Home", idle.getBoolean("back"));
            assertFalse(idle.getBoolean("more"));
            JSONArray attention = idle.getJSONArray("attention");
            assertEquals(8, attention.length());
            for (int i = 0; i < attention.length(); i++)
                assertEquals("Unread completion", attention.getJSONObject(i).getString("state"));
            assertEquals(1, rows("Completed item 4"));
            saveEvidence("active-unread-idle-watermarks-retained");
        } catch (Exception | AssertionError failure) {
            saveEvidence("active-unread-failure");
            throw failure;
        }
    }

    private void assertLive() throws Exception {
        JSONArray live = dom().getJSONArray("live");
        assertEquals(3, live.length());
        assertEquals("Open Shared task", live.getJSONObject(0).getString("label"));
        assertEquals("Open Pending answer", live.getJSONObject(1).getString("label"));
        assertEquals("Open Scheduled task", live.getJSONObject(2).getString("label"));
        assertEquals("Running", live.getJSONObject(0).getString("state"));
        assertEquals("Waiting for an answer", live.getJSONObject(1).getString("state"));
        assertEquals("Waiting", live.getJSONObject(2).getString("state"));
    }

    private int rows(String title) throws Exception {
        int count = 0;
        JSONArray attention = dom().getJSONArray("attention");
        for (int i = 0; i < attention.length(); i++)
            if (("Open " + title).equals(attention.getJSONObject(i).getString("label"))) count++;
        return count;
    }

    private boolean waitRows(String title, int count, long timeout) throws Exception {
        long deadline = SystemClock.uptimeMillis() + timeout;
        do { if (rows(title) == count) return true; SystemClock.sleep(100); }
        while (SystemClock.uptimeMillis() < deadline);
        return false;
    }

    private JSONObject dom() throws Exception {
        // Android WebView's AX snapshot omitted the polled roster even while the
        // native screenshot showed it. Read only the rendered packaged WebView;
        // all interaction remains a native accessibility click, never JS/RPC.
        String script = "JSON.stringify((() => {"
            + "const all = [...document.querySelectorAll('section')];"
            + "const attention = document.querySelector('[data-testid=completion-attention]');"
            + "const live = all.find(e => e.querySelector('h2')?.textContent?.trim().toLowerCase() === 'in progress');"
            + "const rows = e => e ? [...e.querySelectorAll('button[aria-label^=\"Open \"]')].map(b => ({"
            + "label:b.getAttribute('aria-label'),state:b.querySelector('[data-testid=row-state]')?.textContent?.trim() ?? ''})) : [];"
            + "const more = attention && [...attention.querySelectorAll('button')].find(b => b.textContent.trim() === 'More');"
            + "const r = more?.getBoundingClientRect();"
            + "return {url:location.href,attention:rows(attention),live:rows(live),more:!!more,"
            + "moreBounds:r ? {x:r.x,y:r.y,width:r.width,height:r.height}:null,viewport:{width:innerWidth,height:innerHeight},"
            + "back:!!document.querySelector('button[aria-label=Back]')};})())";
        CountDownLatch done = new CountDownLatch(1);
        AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView()
            .evaluateJavascript(script, result -> { value.set(result); done.countDown(); }));
        assertTrue("Packaged WebView observation must return", done.await(5, TimeUnit.SECONDS));
        assertNotNull(value.get());
        return new JSONObject(new JSONArray("[" + value.get() + "]").getString(0));
    }

    private String sha256(String path) throws Exception {
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        try (FileInputStream stream = new FileInputStream(path)) {
            byte[] buffer = new byte[65536];
            for (int size; (size = stream.read(buffer)) != -1;) digest.update(buffer, 0, size);
        }
        StringBuilder hex = new StringBuilder();
        for (byte value : digest.digest()) hex.append(String.format("%02x", value & 255));
        return hex.toString();
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.equalsIgnoreCase(String.valueOf(node.getText()))
            || label.equalsIgnoreCase(String.valueOf(node.getContentDescription()));
    }

    private void clickFirstMore() {
        AccessibilityNodeInfo first = null;
        int top = Integer.MAX_VALUE;
        for (AccessibilityNodeInfo node : nodes()) {
            if (!named(node, "More") || !node.isVisibleToUser() || !node.isClickable()) continue;
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            if (!bounds.isEmpty() && bounds.top < top) { first = node; top = bounds.top; }
        }
        assertNotNull("Attention More must remain usable", first);
        assertTrue(first.isEnabled());
        assertTrue(first.performAction(AccessibilityNodeInfo.ACTION_CLICK));
    }

    private List<AccessibilityNodeInfo> nodes() {
        List<AccessibilityNodeInfo> all = new ArrayList<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root == null) return all;
        all.add(root);
        for (int i = 0; i < all.size(); i++) {
            AccessibilityNodeInfo node = all.get(i);
            for (int j = 0; j < node.getChildCount(); j++) {
                AccessibilityNodeInfo child = node.getChild(j);
                if (child != null) all.add(child);
            }
        }
        return all;
    }

    private void saveEvidence(String name) throws Exception {
        // DOM observation can precede the compositor's next displayed frame.
        // Wait for the native WebView visual-state barrier before screenshots.
        CountDownLatch rendered = new CountDownLatch(1);
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView()
            .postVisualStateCallback(SystemClock.uptimeMillis(), new WebView.VisualStateCallback() {
                @Override public void onComplete(long requestId) { rendered.countDown(); }
            }));
        assertTrue("The packaged WebView must prepare the observed DOM for drawing",
            rendered.await(5, TimeUnit.SECONDS));
        instrumentation.waitForIdleSync();
        ui.waitForIdle(300, 3000);
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "active-unread-v3");
        assertTrue(directory.isDirectory() || directory.mkdirs());
        Bitmap screenshot = ui.takeScreenshot(); assertNotNull(screenshot);
        try (FileOutputStream stream = new FileOutputStream(new File(directory, name + ".png"))) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, stream));
        }
        screenshot.recycle();
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName()))
                .put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription()))
                .put("enabled", node.isEnabled()).put("clickable", node.isClickable())
                .put("visible", node.isVisibleToUser()).put("bounds", bounds.flattenToString()));
        }
        try (FileOutputStream stream = new FileOutputStream(new File(directory, name + "-accessibility.json"))) {
            stream.write(tree.toString(2).getBytes(StandardCharsets.UTF_8));
        }
        try (FileOutputStream stream = new FileOutputStream(new File(directory, name + "-packaged-dom.json"))) {
            stream.write(dom().toString(2).getBytes(StandardCharsets.UTF_8));
        }
    }
}
