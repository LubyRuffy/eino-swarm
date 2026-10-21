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
import android.view.InputDevice;
import android.view.MotionEvent;
import android.view.accessibility.AccessibilityNodeInfo;
import android.webkit.WebView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.getcapacitor.BridgeActivity;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.List;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Actual native Stop/navigation on a disposable packaged offline fixture. */
@RunWith(AndroidJUnit4.class)
public class WorkerStopReplayTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private final JSONArray actions = new JSONArray();
    private BridgeActivity activity;
    private File evidenceDirectory;

    @Test
    public void nativeWorkerStopReplayRetainsStoppedAndFinishedOutcomes() throws Exception {
        evidenceDirectory = new File(instrumentation.getTargetContext().getExternalFilesDir(null),
            "worker-stop-replay-" + UUID.randomUUID());
        assertTrue("Each native attempt needs a new evidence directory", evidenceDirectory.mkdir());
        Bundle identity = new Bundle();
        identity.putString("evidence_directory", evidenceDirectory.getAbsolutePath());
        identity.putString("driver_identity", new JSONObject()
            .put("driver", "issue85-native-Stop-stored-watch-replay")
            .put("test_class", getClass().getName())
            .put("loaded_classloader", getClass().getClassLoader().toString())
            .put("installed_test_apk_sha256", sha256(instrumentation.getContext().getPackageCodePath()))
            .put("installed_target_apk_sha256", sha256(instrumentation.getTargetContext().getPackageCodePath()))
            .toString());
        instrumentation.sendStatus(2, identity);
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity reached = instrumentation.startActivitySync(launch);
        assertTrue(reached instanceof BridgeActivity);
        activity = (BridgeActivity) reached;
        try {
            waitButton("Stop", 20000);
            assertEquals("Trim the layout pass", dom().getString("title"));
            clickNative("Agents (2)");
            waitGroup("Active (2)");
            assertWorker("reader-1", "running");
            assertWorker("writer-2", "running");
            assertWorker("done-3", "done");
            assertWorker("failed-4", "failed");
            saveEvidence("worker-stop-live-roster");
            clickNative("Back");
            clickNative("Stop");
            waitButton("Agents (0)", 8000);

            for (int replay = 0; replay < 2; replay++) {
                clickNative("Agents (0)");
                waitGroup("Stopped (2)");
                assertTrue(dom().getJSONArray("groups").toString().contains("Finished (2)"));
                assertEquals(4, dom().getJSONArray("workers").length());
                assertWorker("reader-1", "stopped");
                assertWorker("writer-2", "stopped");
                assertWorker("done-3", "done");
                assertWorker("failed-4", "failed");
                saveEvidence("worker-stop-roster-" + replay);
                clickWorker("reader-1");
                waitTranscript("swarm: agent reader-1 cancelled: context canceled");
                assertEquals(0, dom().getInt("destructive"));
                assertEquals(0, dom().getInt("spinners"));
                saveEvidence("worker-stop-cancelled-detail-" + replay);
                clickNative("Back");
                if (replay == 0) {
                    clickWorker("done-3");
                    // The successful finish remains a roster outcome; it does
                    // not fabricate an answer block in this worker's history.
                    long detailDeadline = SystemClock.uptimeMillis() + 8000;
                    while (!"worker".equals(dom().getString("title"))
                        && SystemClock.uptimeMillis() < detailDeadline) SystemClock.sleep(100);
                    assertEquals("worker", dom().getString("title"));
                    assertEquals("", dom().getString("transcript"));
                    assertEquals(0, dom().getInt("destructive"));
                    assertEquals(0, dom().getInt("spinners"));
                    saveEvidence("worker-stop-success-detail");
                    clickNative("Back");
                    clickWorker("failed-4");
                    waitTranscript("provider unavailable");
                    assertTrue(dom().getInt("destructive") > 0);
                    saveEvidence("worker-stop-failure-detail");
                    clickNative("Back");
                    clickNative("Back");
                    clickNative("Back");
                    waitButton("Open Trim the layout pass", 8000);
                    saveEvidence("worker-stop-same-host-Home");
                    clickNative("Open Trim the layout pass");
                    waitButton("Agents (0)", 8000);
                    assertFalse(hasButton("Stop"));
                }
            }
        } catch (Exception | AssertionError failure) {
            try {
                saveEvidence("worker-stop-native-failure");
            } catch (Exception | AssertionError captureFailure) {
                failure.addSuppressed(captureFailure);
            }
            throw failure;
        }
    }

    private JSONObject dom() throws Exception {
        // Android AX may omit dynamically rendered WebView children. This reads
        // the actual packaged WebView; it never clicks, writes state or calls RPC.
        String script = "JSON.stringify((() => {const tidy=s=>(s||'').replace(/\\s+/g,' ').trim();"
            + "const root=document.querySelector('[data-testid=agent-roster]');"
            + "const log=document.querySelector('[data-testid=transcript]');"
            + "const button=b=>{const r=b.getBoundingClientRect(),s=getComputedStyle(b),cx=r.x+r.width/2,cy=r.y+r.height/2;"
            + "const visible=r.width>0&&r.height>0&&s.display!=='none'&&s.visibility==='visible'&&Number(s.opacity)>0;"
            + "const hit=cx>0&&cy>0&&cx<innerWidth&&cy<innerHeight&&b.contains(document.elementFromPoint(cx,cy));"
            + "return {label:b.getAttribute('aria-label')||tidy(b.innerText),text:tidy(b.innerText),"
            + "enabled:!b.disabled,visible,hit,x:r.x,y:r.y,width:r.width,height:r.height};};"
            + "return {url:location.href,title:tidy(document.querySelector('h1')?.textContent),"
            + "groups:root?[...root.querySelectorAll('h2')].map(e=>tidy(e.textContent)):[],"
            + "workers:root?[...root.querySelectorAll('button')].map(button):[],"
            + "buttons:[...document.querySelectorAll('button')].map(button),"
            + "transcript:tidy(log?.textContent),destructive:log?.querySelectorAll('.text-destructive').length||0,"
            + "spinners:log?.querySelectorAll('.animate-spin').length||0,viewport:{width:innerWidth,height:innerHeight}};})())";
        CountDownLatch ready = new CountDownLatch(1);
        AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView()
            .evaluateJavascript(script, result -> { value.set(result); ready.countDown(); }));
        assertTrue("Packaged WebView observation must return", ready.await(5, TimeUnit.SECONDS));
        assertNotNull(value.get());
        return new JSONObject(new JSONArray("[" + value.get() + "]").getString(0));
    }

    private boolean hasButton(String label) throws Exception {
        JSONArray buttons = dom().getJSONArray("buttons");
        for (int i = 0; i < buttons.length(); i++)
            if (label.equals(buttons.getJSONObject(i).getString("label"))) return true;
        return false;
    }

    private void waitButton(String label, long timeout) throws Exception {
        long deadline = SystemClock.uptimeMillis() + timeout;
        while (!hasButton(label) && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
        assertTrue("Native page must expose " + label, hasButton(label));
    }

    private void waitGroup(String label) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 8000;
        while (!dom().getJSONArray("groups").toString().contains(label)
            && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
        assertTrue(dom().getJSONArray("groups").toString().contains(label));
    }

    private void waitTranscript(String text) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 8000;
        while (!dom().getString("transcript").contains(text)
            && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
        assertTrue(dom().getString("transcript").contains(text));
    }

    private void assertWorker(String id, String state) throws Exception {
        JSONArray rows = dom().getJSONArray("workers");
        List<String> matches = new ArrayList<>();
        for (int i = 0; i < rows.length(); i++) {
            String label = rows.getJSONObject(i).getString("label");
            if (label.contains(id)) matches.add(label);
        }
        assertEquals("Original worker must occur once", 1, matches.size());
        assertTrue(matches.get(0), matches.get(0).endsWith(state));
    }

    private void clickWorker(String id) throws Exception {
        JSONArray rows = dom().getJSONArray("workers");
        String label = null;
        for (int i = 0; i < rows.length(); i++)
            if (rows.getJSONObject(i).getString("label").contains(id))
                label = rows.getJSONObject(i).getString("label");
        assertNotNull(label);
        clickNative(label);
    }

    private void clickNative(String label) throws Exception {
        for (AccessibilityNodeInfo node : nodes()) {
            String text = String.valueOf(node.getText()).replaceAll("\\s+", " ").trim();
            String description = String.valueOf(node.getContentDescription()).replaceAll("\\s+", " ").trim();
            if (node.isVisibleToUser() && node.isEnabled() && node.isClickable()
                && (label.equals(text) || label.equals(description))) {
                if (node.performAction(AccessibilityNodeInfo.ACTION_CLICK)) {
                    actions.put(new JSONObject().put("label", label).put("action", "native_AX_click"));
                    return;
                }
            }
        }
        JSONObject snapshot = dom();
        JSONArray buttons = snapshot.getJSONArray("buttons");
        JSONObject hit = null;
        for (int i = 0; i < buttons.length(); i++) {
            JSONObject button = buttons.getJSONObject(i);
            if (label.equals(button.getString("label")) && button.getBoolean("enabled")
                && button.getBoolean("visible") && button.getBoolean("hit")) { hit = button; break; }
        }
        assertNotNull("No visible packaged native control for " + label, hit);
        JSONObject viewport = snapshot.getJSONObject("viewport");
        double cx = hit.getDouble("x") + hit.getDouble("width") / 2;
        double cy = hit.getDouble("y") + hit.getDouble("height") / 2;
        assertTrue(hit.getDouble("width") > 0 && hit.getDouble("height") > 0);
        assertTrue(cx > 0 && cy > 0 && cx < viewport.getDouble("width") && cy < viewport.getDouble("height"));
        int[] geometry = new int[4];
        instrumentation.runOnMainSync(() -> {
            WebView view = activity.getBridge().getWebView();
            view.getLocationOnScreen(geometry); geometry[2] = view.getWidth(); geometry[3] = view.getHeight();
        });
        float x = (float) (geometry[0] + cx * geometry[2] / viewport.getDouble("width"));
        float y = (float) (geometry[1] + cy * geometry[3] / viewport.getDouble("height"));
        assertTrue(x > geometry[0] && x < geometry[0] + geometry[2] && y > geometry[1] && y < geometry[1] + geometry[3]);
        long time = SystemClock.uptimeMillis();
        MotionEvent down = MotionEvent.obtain(time, time, MotionEvent.ACTION_DOWN, x, y, 0);
        down.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        MotionEvent up = MotionEvent.obtain(time, time + 50, MotionEvent.ACTION_UP, x, y, 0);
        up.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue(ui.injectInputEvent(down, true));
        assertTrue(ui.injectInputEvent(up, true));
        down.recycle(); up.recycle();
        actions.put(new JSONObject().put("label", label).put("action", "native_touch_current_DOM_bounds")
            .put("button", hit).put("viewport", viewport).put("native_x", x).put("native_y", y));
    }

    private List<AccessibilityNodeInfo> nodes() {
        List<AccessibilityNodeInfo> all = new ArrayList<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root == null) return all;
        root.refresh(); all.add(root);
        for (int i = 0; i < all.size(); i++) {
            AccessibilityNodeInfo node = all.get(i);
            for (int j = 0; j < node.getChildCount(); j++) {
                AccessibilityNodeInfo child = node.getChild(j);
                if (child != null) all.add(child);
            }
        }
        return all;
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

    private void saveEvidence(String name) throws Exception {
        CountDownLatch rendered = new CountDownLatch(1);
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView()
            .postVisualStateCallback(SystemClock.uptimeMillis(), new WebView.VisualStateCallback() {
                @Override public void onComplete(long requestId) { rendered.countDown(); }
            }));
        assertTrue("Packaged DOM must be ready to draw", rendered.await(5, TimeUnit.SECONDS));
        instrumentation.waitForIdleSync(); ui.waitForIdle(300, 3000);
        Bitmap screenshot = ui.takeScreenshot(); assertNotNull(screenshot);
        try (FileOutputStream stream = newOutput(name + ".png")) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, stream));
        }
        screenshot.recycle();
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName()))
                .put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription()))
                .put("visible", node.isVisibleToUser()).put("clickable", node.isClickable()).put("bounds", bounds.flattenToString()));
        }
        for (String suffix : new String[] {"-accessibility.json", "-packaged-dom.json", "-native-actions.json"}) {
            String value = suffix.startsWith("-accessibility") ? tree.toString(2)
                : suffix.startsWith("-packaged") ? dom().toString(2) : actions.toString(2);
            try (FileOutputStream stream = newOutput(name + suffix)) {
                stream.write(value.getBytes(StandardCharsets.UTF_8));
            }
        }
    }

    private FileOutputStream newOutput(String name) throws Exception {
        File file = new File(evidenceDirectory, name);
        assertTrue("Do not overwrite earlier native evidence: " + name, file.createNewFile());
        return new FileOutputStream(file);
    }
}
