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
import com.getcapacitor.BridgeActivity;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Native input on a disposable mock=1&tick=0 package, not a SQLite goal acceptance. */
@RunWith(AndroidJUnit4.class)
public class QuotedCommandTest {
    private static final String REFUSAL = "The /goal and /plan commands accept text only. Remove quotations first. Your draft was kept.";
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private File evidenceDirectory;
    private BridgeActivity activity;

    @Test
    public void quotedGoalKeepsDraftUntilQuoteIsRemoved() throws Exception {
        launch("goal");
        try {
            addNativeQuote();
            String selected = state().getJSONArray("quotes").getString(0);
            assertFalse(selected.trim().isEmpty());
            String command = "/goal Inspect the assigned operation Issue111";
            setText("Message", command);
            int before = state().getInt("bubbles");
            tap("Send");
            awaitState(() -> REFUSAL.equals(state().getString("alert")));
            JSONObject refused = state();
            assertEquals(command, refused.getString("draft"));
            assertEquals(selected, refused.getJSONArray("quotes").getString(0));
            assertEquals("Refusal must not send an ordinary user message", before, refused.getInt("bubbles"));
            assertFalse(refused.getString("transcript").contains(command));
            saveEvidence("quoted-goal-refused-draft-kept");

            tap("Remove quote 1");
            awaitState(() -> state().getJSONArray("quotes").length() == 0);
            assertEquals(command, state().getString("draft"));
            assertEquals("", state().getString("alert"));
            tap("Send");
            awaitState(() -> state().getString("transcript").contains(command));
            assertEquals("", state().getString("draft"));
            assertEquals(0, state().getJSONArray("quotes").length());
            assertEquals(before + 1, state().getInt("bubbles"));
            saveEvidence("plain-goal-original-text-submitted");
        } catch (Exception | AssertionError failure) {
            preserveFailure(failure);
            throw failure;
        }
    }

    @Test
    public void ordinaryQuotedMessageStillSends() throws Exception {
        launch("ordinary");
        try {
            addNativeQuote();
            String body = "Explain the selected context Issue111";
            setText("Message", body);
            int before = state().getInt("quotedMessages");
            tap("Send");
            awaitState(() -> state().getString("transcript").contains(body));
            JSONObject sent = state();
            assertEquals(before + 1, sent.getInt("quotedMessages"));
            assertTrue(sent.getString("transcript").contains("Selected text"));
            assertEquals("", sent.getString("draft"));
            assertEquals(0, sent.getJSONArray("quotes").length());
            assertEquals("", sent.getString("alert"));
            saveEvidence("ordinary-quoted-message-submitted");
        } catch (Exception | AssertionError failure) {
            preserveFailure(failure);
            throw failure;
        }
    }

    private void launch(String suffix) throws Exception {
        evidenceDirectory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "quoted-command-" + suffix);
        assertTrue(evidenceDirectory.isDirectory() || evidenceDirectory.mkdirs());
        JSONObject identity = new JSONObject().put("test_class", getClass().getName())
            .put("installed_test_apk_sha256", sha256(instrumentation.getContext().getPackageCodePath()))
            .put("installed_target_apk_sha256", sha256(instrumentation.getTargetContext().getPackageCodePath()));
        writeJson("identity", identity);
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
    }

    private void addNativeQuote() throws Exception {
        tap("Back");
        tap("Open Sweep the unused exports");
        awaitState(() -> state().getString("transcript").contains("Here is what changed:"));
        JSONObject rect = stableSourceRect();
        writeJson("native-source-long-press-target", rect);
        saveEvidence("native-source-before-long-press");
        nativePress(screenPoint(rect), 1200);
        buttonRect("Add to chat");
        saveEvidence("native-selection-before-add-to-chat");
        tap("Add to chat");
        awaitState(() -> state().getJSONArray("quotes").length() == 1);
    }

    private void setText(String label, String value) throws Exception {
        assertEquals("The case only targets the main composer", "Message", label);
        // API 33 WebView omitted aria-label from its EditText node. Resolve
        // the actual textarea's read-only geometry, then require one native
        // editable node at that screen position. Input remains an AX action.
        JSONObject rect = observe("JSON.stringify((()=>{const p=document.querySelector('textarea[aria-label=Message]');"
            + "if(!p)return {};const r=p.getBoundingClientRect();"
            + "return {x:r.left+r.width/2,y:r.top+r.height/2,width:innerWidth}})())");
        assertTrue("The actual Message textarea must exist", rect.has("x"));
        int[] location = new int[2];
        AtomicReference<Float> scale = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> {
            activity.getBridge().getWebView().getLocationOnScreen(location);
            scale.set(activity.getBridge().getWebView().getWidth() / (float) rect.optDouble("width"));
        });
        int x = Math.round(location[0] + (float) rect.getDouble("x") * scale.get());
        int y = Math.round(location[1] + (float) rect.getDouble("y") * scale.get());
        long deadline = SystemClock.uptimeMillis() + 10000;
        do {
            List<AccessibilityNodeInfo> found = new ArrayList<>();
            for (AccessibilityNodeInfo node : nodes()) {
                Rect bounds = new Rect();
                node.getBoundsInScreen(bounds);
                if (node.isEditable() && node.isVisibleToUser() && node.isEnabled()
                    && "android.widget.EditText".equals(String.valueOf(node.getClassName()))
                    && bounds.contains(x, y)) {
                    found.add(node);
                }
            }
            assertTrue("The main composer must resolve to at most one native editable node", found.size() <= 1);
            if (found.size() == 1) {
                writeJson("main-composer-native-input-target", new JSONObject()
                    .put("readonly_dom_rect", rect).put("screen_x", x).put("screen_y", y)
                    .put("unique_native_editable_node", true));
                Bundle text = new Bundle();
                text.putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, value);
                assertTrue("Native accessibility must type the draft", found.get(0).performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, text));
                awaitState(() -> value.equals(state().getString("draft")));
                return;
            }
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < deadline);
        throw new AssertionError("No native editable field: " + label);
    }

    private void tap(String label) throws Exception {
        JSONObject rect = buttonRect(label);
        int[] point = screenPoint(rect);
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect();
            node.getBoundsInScreen(bounds);
            if (named(node, label) && node.isVisibleToUser() && node.isEnabled() && node.isClickable()
                && bounds.contains(point[0], point[1])) {
                assertTrue("Native accessibility click must reach " + label,
                    node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
                return;
            }
        }
        // Dynamic WebView buttons can be absent from API 33's AX snapshot.
        // The rendered button must still exist and be visible/enabled. Tap its
        // actual screen position once; never use a JavaScript click or retry.
        writeJson("native-pointer-" + label.replace(' ', '-'), rect);
        nativePress(point, 60);
    }

    private JSONObject buttonRect(String label) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 15000;
        do {
            JSONObject rect = observe("JSON.stringify((()=>{const p=[...document.querySelectorAll('button')]"
                + ".find(p=>(p.getAttribute('aria-label')||p.textContent.trim())===" + JSONObject.quote(label) + ");"
                + "if(!p||p.disabled)return {};const r=p.getBoundingClientRect();"
                + "return r.width>0&&r.height>0&&r.top>=0&&r.bottom<=innerHeight?"
                + "{label:" + JSONObject.quote(label) + ",x:r.left+r.width/2,y:r.top+r.height/2,width:innerWidth}:{};})())");
            if (rect.has("x")) return rect;
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < deadline);
        throw new AssertionError("No visible enabled packaged button: " + label);
    }

    private JSONObject stableSourceRect() throws Exception {
        String script = "JSON.stringify((()=>{const p=[...document.querySelectorAll('[data-quote-text] p')]"
            + ".find(p=>p.textContent==='Here is what changed:');if(!p)return {};const r=p.getBoundingClientRect();"
            + "return r.top>=0&&r.bottom<=innerHeight?"
            + "{x:r.left+Math.min(18,r.width/2),y:r.top+r.height/2,width:innerWidth}:{};})())";
        JSONObject previous = new JSONObject();
        long deadline = SystemClock.uptimeMillis() + 10000;
        do {
            JSONObject rect = observe(script);
            if (rect.has("x") && rect.toString().equals(previous.toString())) return rect;
            previous = rect;
            SystemClock.sleep(150);
        } while (SystemClock.uptimeMillis() < deadline);
        throw new AssertionError("The actual source paragraph did not settle visibly before native selection");
    }

    private int[] screenPoint(JSONObject rect) throws Exception {
        int[] location = new int[2];
        AtomicReference<Float> scale = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> {
            activity.getBridge().getWebView().getLocationOnScreen(location);
            scale.set(activity.getBridge().getWebView().getWidth() / (float) rect.optDouble("width"));
        });
        return new int[] {Math.round(location[0] + (float) rect.getDouble("x") * scale.get()),
            Math.round(location[1] + (float) rect.getDouble("y") * scale.get())};
    }

    private void nativePress(int[] point, long duration) {
        long down = SystemClock.uptimeMillis();
        MotionEvent press = MotionEvent.obtain(down, down, MotionEvent.ACTION_DOWN, point[0], point[1], 0);
        press.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue("Native pointer press must start", ui.injectInputEvent(press, true));
        press.recycle();
        SystemClock.sleep(duration);
        MotionEvent release = MotionEvent.obtain(down, SystemClock.uptimeMillis(), MotionEvent.ACTION_UP, point[0], point[1], 0);
        release.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue("Native pointer press must finish", ui.injectInputEvent(release, true));
        release.recycle();
    }

    private interface Check { boolean ready() throws Exception; }

    private void awaitState(Check check) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 15000;
        do {
            if (check.ready()) return;
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < deadline);
        throw new AssertionError("The actual packaged WebView did not reach the expected state");
    }

    private JSONObject state() throws Exception {
        // Read the actual rendered DOM because API 33 WebView can omit static
        // text from AX. This does not select text, set fields, click or mutate.
        return observe("JSON.stringify({url:location.href,transcript:document.querySelector('[data-testid=transcript]')?.innerText??'',"
            + "draft:document.querySelector('textarea[aria-label=Message]')?.value??'',"
            + "quotes:[...document.querySelectorAll('[data-testid=quote-draft]')].map(x=>x.value),"
            + "alert:document.querySelector('[role=alert]')?.innerText??'',"
            + "quotedMessages:document.querySelectorAll('[data-testid=quoted-message]').length,"
            + "bubbles:document.querySelectorAll('[data-testid=transcript] [data-quote-text].ml-auto').length})");
    }

    private JSONObject observe(String script) throws Exception {
        CountDownLatch ready = new CountDownLatch(1);
        AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(script, result -> {
            value.set(result);
            ready.countDown();
        }));
        assertTrue("Packaged WebView read-only observation timed out", ready.await(5, TimeUnit.SECONDS));
        assertNotNull(value.get());
        Object encoded = new JSONArray("[" + value.get() + "]").get(0);
        assertTrue("Read-only observation must return JSON", encoded instanceof String);
        return new JSONObject((String) encoded);
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.contentEquals(node.getText() == null ? "" : node.getText())
            || label.contentEquals(node.getContentDescription() == null ? "" : node.getContentDescription());
    }

    private List<AccessibilityNodeInfo> nodes() {
        List<AccessibilityNodeInfo> result = new ArrayList<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
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

    private void preserveFailure(Throwable failure) {
        try { saveEvidence("quoted-command-native-failure"); }
        catch (Exception | AssertionError evidenceFailure) { failure.addSuppressed(evidenceFailure); }
    }

    private void saveEvidence(String name) throws Exception {
        awaitNativeFrame();
        Bitmap screenshot = ui.takeScreenshot();
        assertNotNull(screenshot);
        try (FileOutputStream stream = new FileOutputStream(new File(evidenceDirectory, name + ".png"))) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, stream));
        } finally { screenshot.recycle(); }
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect();
            node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName()))
                .put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription()))
                .put("visible", node.isVisibleToUser()).put("enabled", node.isEnabled())
                .put("clickable", node.isClickable()).put("bounds", bounds.flattenToString()));
        }
        writeJson(name, new JSONObject().put("accessibility", tree).put("rendered_webview", state()));
    }

    private void awaitNativeFrame() throws Exception {
        CountDownLatch visual = new CountDownLatch(1);
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().postVisualStateCallback(
            SystemClock.uptimeMillis(), new WebView.VisualStateCallback() {
                @Override public void onComplete(long requestId) { visual.countDown(); }
            }));
        assertTrue("WebView must commit its observed DOM before the screenshot", visual.await(5, TimeUnit.SECONDS));
        ui.waitForIdle(200, 3000);
    }

    private void writeJson(String name, JSONObject value) throws Exception {
        try (FileOutputStream stream = new FileOutputStream(new File(evidenceDirectory, name + ".json"))) {
            stream.write(value.toString(2).getBytes(StandardCharsets.UTF_8));
        }
    }

    private String sha256(String path) throws Exception {
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        try (FileInputStream stream = new FileInputStream(path)) {
            byte[] buffer = new byte[65536];
            int count;
            while ((count = stream.read(buffer)) != -1) digest.update(buffer, 0, count);
        }
        StringBuilder result = new StringBuilder();
        for (byte value : digest.digest()) result.append(String.format("%02x", value & 0xff));
        return result.toString();
    }
}
