package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.net.Uri;
import android.os.SystemClock;
import android.system.Os;
import android.system.OsConstants;
import android.view.InputDevice;
import android.view.MotionEvent;
import android.view.accessibility.AccessibilityNodeInfo;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.getcapacitor.BridgeActivity;
import java.io.File;
import java.io.FileOutputStream;
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

/** Inspects the packaged WebView through native navigation, with offline lifecycle evidence. */
@RunWith(AndroidJUnit4.class)
public class GenerationInterruptionTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private BridgeActivity activity;

    @Test
    public void interruptedGenerationRetainsReasonAndRunningWorkerAfterReopen() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity reached = instrumentation.startActivitySync(launch);
        assertTrue(reached instanceof BridgeActivity);
        activity = (BridgeActivity) reached;
        try {
            selectOfflineInput();
            for (String phase : new String[] {"first-open", "reopened"}) {
                click(waitFor("Agents (1)", true));
                AccessibilityNodeInfo worker = waitForWorker();
                String row = String.valueOf(worker.getText()) + " " + worker.getContentDescription();
                assertFalse("Recovery must not mark the worker failed", row.toLowerCase().contains("failed"));
                assertFalse("Recovery must not mark the worker stopped", row.toLowerCase().contains("stopped"));
                click(worker);
                JSONObject state = waitForNotice();
                assertEquals("Exactly one interruption remains after recovery", 1, state.getJSONArray("notices").length());
                String notice = state.getJSONArray("notices").getString(0);
                assertTrue(notice.contains("Generation interrupted: engine restart."));
                assertTrue(notice.contains("Execution resumed; the interrupted output remains incomplete."));
                assertFalse(notice.contains("Recovery pending."));
                JSONArray folds = state.getJSONArray("folds");
                assertTrue("Old and new outputs must remain distinct", folds.length() >= 2);
                assertEquals("Old thought must stop showing Thinking", "Thought", folds.getJSONObject(0).getString("text"));
                assertEquals("Old thought must have no animated content", 0, folds.getJSONObject(0).getInt("animated"));
                click(waitFor("Thought", true));
                openThoughtBody();
                waitFor("Interrupted thought before restart.", false);
                saveEvidence(phase);
                click(waitFor("Back", true));
                waitForWorker();
                click(waitFor("Back", true));
            }
        } catch (Exception | AssertionError failure) {
            saveEvidence("failure");
            throw failure;
        } finally {
            instrumentation.runOnMainSync(() -> activity.finish());
        }
    }

    private void selectOfflineInput() throws Exception {
        AtomicReference<String> origin = new AtomicReference<>();
        long until = SystemClock.uptimeMillis() + 15000;
        do {
            instrumentation.runOnMainSync(() -> {
                if (activity.getBridge() != null && activity.getBridge().getWebView() != null) {
                    origin.set(activity.getBridge().getWebView().getUrl());
                }
            });
            if (origin.get() != null) {
                Uri observed = Uri.parse(origin.get());
                if ("https".equals(observed.getScheme()) && "localhost".equals(observed.getHost())) break;
            }
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < until);
        assertNotNull("The actual packaged origin must exist", origin.get());
        Uri current = Uri.parse(origin.get());
        assertEquals("https", current.getScheme());
        assertEquals("localhost", current.getHost());
        String offline = current.buildUpon().path("/").clearQuery().fragment(null)
            .appendQueryParameter("mock", "1").appendQueryParameter("tick", "0")
            .appendQueryParameter("history", "generation-interruption").build().toString();
        // Only choose the existing packaged fixture; no state or resource injection.
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().loadUrl(offline));
    }

    private AccessibilityNodeInfo waitForWorker() throws Exception {
        long deadline = SystemClock.uptimeMillis() + 15000;
        while (SystemClock.uptimeMillis() < deadline) {
            for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
                String row = String.valueOf(node.getText()) + " " + node.getContentDescription();
                if (node.isVisibleToUser() && node.isClickable() && row.contains("worker-1")) return node;
            }
            SystemClock.sleep(100);
        }
        throw new AssertionError("The same active worker was not visible");
    }

    private JSONObject waitForNotice() throws Exception {
        long deadline = SystemClock.uptimeMillis() + 15000;
        JSONObject state;
        do {
            state = dom();
            if (state.getJSONArray("notices").length() > 0 && state.getJSONArray("folds").length() >= 2) return state;
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < deadline);
        throw new AssertionError("The packaged app did not render generation lifecycle evidence: " + state);
    }

    private JSONObject dom() throws Exception {
        String script = "JSON.stringify({notices:[...document.querySelectorAll('[data-testid=generation-notice]')].map(e=>e.textContent),"
            + "folds:[...document.querySelectorAll('[data-testid=work-fold]')].map(e=>({text:e.textContent.trim(),animated:e.querySelectorAll('[class*=animate-]').length}))})";
        CountDownLatch ready = new CountDownLatch(1);
        AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(script, result -> {
            value.set(result); ready.countDown();
        }));
        assertTrue("The actual WebView DOM must answer the read-only inspection", ready.await(5, TimeUnit.SECONDS));
        return new JSONObject(new JSONArray("[" + value.get() + "]").getString(0));
    }

    private void openThoughtBody() throws Exception {
        String script = "JSON.stringify((()=>{const rows=document.querySelectorAll('[data-testid=phone-thought-toggle]');"
            + "if(rows.length!==1)return {};const p=rows[0];if(p.disabled)return {};const r=p.getBoundingClientRect();"
            + "return r.width>0&&r.height>0&&r.top>=0&&r.bottom<=innerHeight?"
            + "{x:r.left+r.width/2,y:r.top+r.height/2,width:innerWidth,expanded:p.getAttribute('aria-expanded')}:{};})())";
        JSONObject target = null;
        long until = SystemClock.uptimeMillis() + 10000;
        do {
            CountDownLatch ready = new CountDownLatch(1);
            AtomicReference<String> result = new AtomicReference<>();
            instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(script, value -> {
                result.set(value); ready.countDown();
            }));
            assertTrue("Read the actual child control before native input", ready.await(5, TimeUnit.SECONDS));
            JSONObject observed = new JSONObject(new JSONArray("[" + result.get() + "]").getString(0));
            if (observed.has("x")) { target = observed; break; }
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < until);
        assertNotNull("Opening work must expose its own reasoning toggle", target);
        assertEquals("Reasoning starts collapsed inside expanded work", "false", target.getString("expanded"));
        int[] location = new int[2];
        AtomicReference<Float> scale = new AtomicReference<>();
        JSONObject rect = target;
        instrumentation.runOnMainSync(() -> {
            activity.getBridge().getWebView().getLocationOnScreen(location);
            scale.set(activity.getBridge().getWebView().getWidth() / (float) rect.optDouble("width"));
        });
        int x = Math.round(location[0] + (float) target.getDouble("x") * scale.get());
        int y = Math.round(location[1] + (float) target.getDouble("y") * scale.get());
        long down = SystemClock.uptimeMillis();
        MotionEvent press = MotionEvent.obtain(down, down, MotionEvent.ACTION_DOWN, x, y, 0);
        press.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue(ui.injectInputEvent(press, true)); press.recycle();
        SystemClock.sleep(60);
        MotionEvent release = MotionEvent.obtain(down, SystemClock.uptimeMillis(), MotionEvent.ACTION_UP, x, y, 0);
        release.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue(ui.injectInputEvent(release, true)); release.recycle();
    }

    private AccessibilityNodeInfo waitFor(String label, boolean button) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 20000;
        while (SystemClock.uptimeMillis() < deadline) {
            for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
                if (node.isVisibleToUser() && named(node, label)
                    && (!button || "android.widget.Button".equals(String.valueOf(node.getClassName())))) return node;
            }
            SystemClock.sleep(100);
        }
        throw new AssertionError("The native app did not reach accessible " + label);
    }

    private void click(AccessibilityNodeInfo node) {
        assertTrue("The native control must be enabled", node.isEnabled());
        assertTrue("The native control must be clickable", node.isClickable());
        assertTrue("The accessibility click must reach the packaged-app handler", node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.contentEquals(node.getText() == null ? "" : node.getText())
            || label.contentEquals(node.getContentDescription() == null ? "" : node.getContentDescription());
    }

    private List<AccessibilityNodeInfo> nodes(AccessibilityNodeInfo root) {
        List<AccessibilityNodeInfo> result = new ArrayList<>();
        if (root == null) return result;
        result.add(root);
        for (int i = 0; i < result.size(); i++) {
            AccessibilityNodeInfo node = result.get(i);
            for (int child = 0; child < node.getChildCount(); child++) {
                AccessibilityNodeInfo next = node.getChild(child);
                if (next != null) result.add(next);
            }
        }
        return result;
    }

    private void saveEvidence(String name) throws Exception {
        saveEvidence("generation-interruption", name);
    }

    private void saveEvidence(String directoryName, String name) throws Exception {
        File directory = new File(instrumentation.getTargetContext().getFilesDir(), directoryName);
        assertTrue("Native evidence directory must be writable", directory.isDirectory() || directory.mkdirs());
        Bitmap screenshot = ui.takeScreenshot();
        assertNotNull("The reached native screen must be captured", screenshot);
        try (FileOutputStream stream = privateOutput(new File(directory, name + ".png"))) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, stream));
        }
        screenshot.recycle();
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
            Rect bounds = new Rect();
            node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName()))
                .put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription()))
                .put("enabled", node.isEnabled()).put("clickable", node.isClickable())
                .put("visible", node.isVisibleToUser()).put("bounds", bounds.flattenToString()));
        }
        try (FileOutputStream stream = privateOutput(new File(directory, name + ".json"))) {
            stream.write(tree.toString(2).getBytes(StandardCharsets.UTF_8));
        }
    }

    private FileOutputStream privateOutput(File path) throws Exception {
        return new FileOutputStream(Os.open(path.getAbsolutePath(), OsConstants.O_WRONLY | OsConstants.O_CREAT | OsConstants.O_TRUNC, 0600));
    }
}
