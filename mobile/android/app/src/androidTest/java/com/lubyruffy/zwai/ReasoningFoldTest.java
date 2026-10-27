package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.net.Uri;
import android.os.Bundle;
import android.os.SystemClock;
import android.system.Os;
import android.system.OsConstants;
import android.view.InputDevice;
import android.view.MotionEvent;
import android.view.Choreographer;
import android.view.accessibility.AccessibilityNodeInfo;
import android.webkit.WebView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import com.getcapacitor.BridgeActivity;
import java.io.File;
import java.io.FileInputStream;
import java.io.FileOutputStream;
import java.io.InputStream;
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

/** Native nested-fold input on unchanged packaged assets, not a live PC/model test. */
@RunWith(AndroidJUnit4.class)
public class ReasoningFoldTest {
    private static final String THOUGHT = "Reading the current shape before touching it.";
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private BridgeActivity activity;
    private File evidenceDirectory;

    @Test
    public void reasoningStaysCollapsedUntilItsOwnToggleIsPressed() throws Exception {
        String locale = InstrumentationRegistry.getArguments().getString("reasoningLocale", "en");
        assertTrue("Select one existing locale", "en".equals(locale) || "zh".equals(locale));
        evidenceDirectory = new File(instrumentation.getTargetContext().getFilesDir(), "reasoning-fold-" + locale + "-" + UUID.randomUUID());
        assertTrue("Each invocation needs fresh evidence", evidenceDirectory.mkdir());
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity reached = instrumentation.startActivitySync(launch);
        assertTrue(reached instanceof BridgeActivity);
        activity = (BridgeActivity) reached;
        try {
            writeIdentity(locale);
            selectOfflineInput();
            JSONObject initial = state();
            boolean chinese = initial.getBoolean("chinese");
            tapButton(chinese ? "返回" : "Back");
            if (chinese != "zh".equals(locale)) {
                tapButton(chinese ? "菜单" : "Menu");
                tapButton(chinese ? "EN" : "中文");
                awaitState(() -> locale.equals(state().getString("storedLocale")));
            }
            tapButton(("zh".equals(locale) ? "打开 " : "Open ") + "Sweep the unused exports");
            awaitState(() -> state().getString("transcript").contains("Here is what changed:"));
            assertEquals(1, state().getInt("foldCount"));
            assertEquals("false", state().getString("outerExpanded"));
            tapSelector("[data-testid=work-fold]", "open-work");
            awaitState(() -> state().getInt("thoughtToggleCount") == 1);
            JSONObject closed = state();
            assertEquals("true", closed.getString("outerExpanded"));
            assertEquals("false", closed.getString("thoughtExpanded"));
            assertFalse("Opening work must not reveal its reasoning body", closed.getBoolean("bodyVisible"));
            assertFalse(closed.getString("transcript").contains(THOUGHT));
            assertFalse(closed.getString("controls").isEmpty());
            assertUnchangedSurroundings(closed);
            saveEvidence("work-open-reasoning-closed");

            tapSelector("[data-testid=phone-thought-toggle]", "open-reasoning");
            awaitState(() -> state().getBoolean("bodyVisible"));
            JSONObject opened = state();
            assertEquals("true", opened.getString("thoughtExpanded"));
            assertTrue("The revealed thought must be in the captured viewport", opened.getBoolean("bodyOnScreen"));
            assertEquals("The complete stored thought must be preserved", THOUGHT, opened.getString("bodyText"));
            assertEquals(closed.getString("controls"), opened.getString("controls"));
            assertEquals(opened.getString("controls"), opened.getString("bodyId"));
            assertUnchangedSurroundings(opened);
            saveEvidence("reasoning-open");

            tapSelector("[data-testid=phone-thought-toggle]", "close-reasoning");
            awaitState(() -> !state().getBoolean("bodyVisible"));
            JSONObject reclosed = state();
            assertEquals("false", reclosed.getString("thoughtExpanded"));
            assertEquals("true", reclosed.getString("outerExpanded"));
            assertFalse(reclosed.getString("transcript").contains(THOUGHT));
            assertUnchangedSurroundings(reclosed);
            saveEvidence("reasoning-closed-again");
        } catch (Exception | AssertionError failure) {
            try { saveEvidence("failure"); }
            catch (Exception | AssertionError evidenceFailure) { failure.addSuppressed(evidenceFailure); }
            throw failure;
        } finally {
            instrumentation.runOnMainSync(activity::finish);
        }
    }

    private void assertUnchangedSurroundings(JSONObject observed) throws Exception {
        assertTrue(observed.getString("transcript").contains("Here is what changed:"));
        assertTrue("The read tool must remain in the expanded work", observed.getString("transcript").contains("read"));
        assertTrue("The exec tool must remain in the expanded work", observed.getString("transcript").contains("exec"));
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
            .appendQueryParameter("mock", "1").appendQueryParameter("tick", "0").build().toString();
        writeJson("offline-navigation", new JSONObject().put("actual_origin", origin.get()).put("offline_url", offline));
        // Navigation selects the existing packaged walkthrough. All later JS only observes.
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().loadUrl(offline));
        awaitState(() -> state().getBoolean("hasBack"));
    }

    private JSONObject state() throws Exception {
        return observe("JSON.stringify((()=>{const r=document.querySelector('[data-testid=transcript]');"
            + "const f=r?.querySelectorAll('[data-testid=work-fold]')??[];"
            + "const t=r?.querySelectorAll('[data-testid=phone-thought-toggle]')??[];"
            + "const body=r?.querySelector('[data-testid=phone-thought]');"
            + "const visible=!!body&&body.getClientRects().length>0&&getComputedStyle(body).visibility!=='hidden';"
            + "const br=body?.getBoundingClientRect();const bodyOnScreen=visible&&br.width>0&&br.height>0&&br.top>=0&&br.bottom<=innerHeight;"
            + "const labels=[...document.querySelectorAll('button')].map(b=>b.getAttribute('aria-label')||b.textContent.trim());"
            + "return {url:location.href,hasBack:labels.includes('Back')||labels.includes('返回'),chinese:labels.includes('返回'),storedLocale:localStorage.getItem('zwai.phone.locale')??'',"
            + "transcript:r?.innerText??'',foldCount:f.length,outerExpanded:f[0]?.getAttribute('aria-expanded')??'',"
            + "thoughtToggleCount:t.length,thoughtExpanded:t[0]?.getAttribute('aria-expanded')??'',"
            + "controls:t[0]?.getAttribute('aria-controls')??'',bodyVisible:visible,bodyOnScreen,bodyText:body?.textContent??'',bodyId:body?.id??''}})())");
    }

    private void tapButton(String label) throws Exception {
        tapSelector("button[aria-label=" + JSONObject.quote(label) + "],button:not([aria-label])", label, label);
    }

    private void tapSelector(String selector, String action) throws Exception {
        tapSelector(selector, action, null);
    }

    private void tapSelector(String selector, String action, String exactLabel) throws Exception {
        JSONObject rect = null;
        long until = SystemClock.uptimeMillis() + 15000;
        do {
            JSONObject observed = observe("JSON.stringify((()=>{const rows=[...document.querySelectorAll(" + JSONObject.quote(selector) + ")]"
                + (exactLabel == null ? "" : ".filter(p=>(p.getAttribute('aria-label')||p.textContent.trim())===" + JSONObject.quote(exactLabel) + ")")
                + ";if(rows.length!==1||rows[0].disabled)return {};const p=rows[0],r=p.getBoundingClientRect();"
                + "return r.width>0&&r.height>0&&r.top>=0&&r.bottom<=innerHeight?"
                + "{x:r.left+r.width/2,y:r.top+r.height/2,width:innerWidth,label:p.getAttribute('aria-label')||p.textContent.trim()}:{};})())");
            if (observed.has("x")) { rect = observed; break; }
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < until);
        assertNotNull("A unique visible enabled packaged control is required: " + action, rect);
        int[] location = new int[2];
        AtomicReference<Float> scale = new AtomicReference<>();
        JSONObject target = rect;
        instrumentation.runOnMainSync(() -> {
            activity.getBridge().getWebView().getLocationOnScreen(location);
            scale.set(activity.getBridge().getWebView().getWidth() / (float) target.optDouble("width"));
        });
        int x = Math.round(location[0] + (float) rect.getDouble("x") * scale.get());
        int y = Math.round(location[1] + (float) rect.getDouble("y") * scale.get());
        writeJson("native-action-" + action, new JSONObject().put("readonly_dom_target", rect).put("screen_x", x).put("screen_y", y));
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            String label = rect.getString("label");
            if (node.isVisibleToUser() && node.isEnabled() && node.isClickable() && bounds.contains(x, y)
                && (label.contentEquals(node.getText() == null ? "" : node.getText())
                    || label.contentEquals(node.getContentDescription() == null ? "" : node.getContentDescription()))) {
                assertTrue("The native accessibility click must reach the packaged handler", node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
                return;
            }
        }
        long down = SystemClock.uptimeMillis();
        MotionEvent press = MotionEvent.obtain(down, down, MotionEvent.ACTION_DOWN, x, y, 0);
        press.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue(ui.injectInputEvent(press, true)); press.recycle();
        SystemClock.sleep(60);
        MotionEvent release = MotionEvent.obtain(down, SystemClock.uptimeMillis(), MotionEvent.ACTION_UP, x, y, 0);
        release.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue(ui.injectInputEvent(release, true)); release.recycle();
    }

    private interface Check { boolean ready() throws Exception; }

    private void awaitState(Check check) throws Exception {
        long until = SystemClock.uptimeMillis() + 15000;
        do {
            if (check.ready()) return;
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < until);
        throw new AssertionError("The packaged nested fold did not reach the expected state");
    }

    private JSONObject observe(String script) throws Exception {
        CountDownLatch ready = new CountDownLatch(1);
        AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(script, result -> {
            value.set(result); ready.countDown();
        }));
        assertTrue("Read-only WebView observation timed out", ready.await(5, TimeUnit.SECONDS));
        assertNotNull(value.get());
        return new JSONObject(new JSONArray("[" + value.get() + "]").getString(0));
    }

    private List<AccessibilityNodeInfo> nodes() {
        List<AccessibilityNodeInfo> rows = new ArrayList<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root != null) rows.add(root);
        for (int i = 0; i < rows.size(); i++) {
            for (int child = 0; child < rows.get(i).getChildCount(); child++) {
                AccessibilityNodeInfo next = rows.get(i).getChild(child);
                if (next != null) rows.add(next);
            }
        }
        return rows;
    }

    private void saveEvidence(String phase) throws Exception {
        CountDownLatch frame = new CountDownLatch(1);
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().postVisualStateCallback(
            SystemClock.uptimeMillis(), new WebView.VisualStateCallback() {
                @Override public void onComplete(long id) { frame.countDown(); }
            }));
        assertTrue("The observed DOM must be ready for its next native draw", frame.await(5, TimeUnit.SECONDS));
        ui.waitForIdle(200, 3000);
        Bitmap png = stableNativeFrame(phase);
        try (FileOutputStream file = privateOutput(new File(evidenceDirectory, phase + ".png"))) {
            assertTrue(png.compress(Bitmap.CompressFormat.PNG, 100, file));
        } finally { png.recycle(); }
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName()))
                .put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription()))
                .put("visible", node.isVisibleToUser()).put("enabled", node.isEnabled())
                .put("clickable", node.isClickable()).put("bounds", bounds.flattenToString()));
        }
        writeJson(phase, new JSONObject().put("rendered_webview", state()).put("accessibility", tree));
    }

    private Bitmap stableNativeFrame(String phase) throws Exception {
        long started = SystemClock.uptimeMillis(), until = started + 3000;
        JSONArray observations = new JSONArray();
        Bitmap previous = null;
        int stable = 0;
        try {
            while (SystemClock.uptimeMillis() < until) {
                long sampledAt = SystemClock.uptimeMillis();
                JSONArray vsync = naturalVsyncs(until);
                Bitmap current = ui.takeScreenshot(); assertNotNull(current);
                boolean same = previous != null && current.sameAs(previous);
                stable = same ? stable + 1 : 0;
                if (previous != null) previous.recycle();
                previous = current;
                String name = phase + "-frame-" + observations.length();
                try (FileOutputStream file = privateOutput(new File(evidenceDirectory, name + ".png"))) {
                    assertTrue(current.compress(Bitmap.CompressFormat.PNG, 100, file));
                }
                long elapsed = SystemClock.uptimeMillis() - started;
                observations.put(new JSONObject().put("png", name + ".png").put("elapsed_ms", elapsed)
                    .put("natural_vsync_nanos", vsync).put("same_as_previous", same).put("rendered_webview", state()));
                writeJson(phase + "-frame-observations", new JSONObject().put("frames", observations)
                    .put("minimum_observation_ms", 1000).put("budget_ms", 3000));
                // Stable screen pixels still require visual review; DOM and VSYNC are not that proof.
                if (SystemClock.uptimeMillis() > until) break;
                if (elapsed >= 1000 && stable >= 2) {
                    Bitmap selected = previous; previous = null;
                    return selected;
                }
                SystemClock.sleep(Math.max(0, Math.min(250 - (SystemClock.uptimeMillis() - sampledAt), until - SystemClock.uptimeMillis())));
            }
            throw new AssertionError("Native pixels did not stabilize inside the original three-second observation budget");
        } finally { if (previous != null) previous.recycle(); }
    }

    private JSONArray naturalVsyncs(long until) throws Exception {
        CountDownLatch ready = new CountDownLatch(1);
        long[] frames = new long[2];
        Choreographer.FrameCallback callback = new Choreographer.FrameCallback() {
            private int count;
            @Override public void doFrame(long nanos) {
                frames[count++] = nanos;
                if (count == 2) ready.countDown();
                else Choreographer.getInstance().postFrameCallback(this);
            }
        };
        instrumentation.runOnMainSync(() -> Choreographer.getInstance().postFrameCallback(callback));
        try {
            assertTrue("Two natural native VSYNC observations are required", ready.await(Math.max(1, until - SystemClock.uptimeMillis()), TimeUnit.MILLISECONDS));
            return new JSONArray().put(frames[0]).put(frames[1]);
        } finally {
            instrumentation.runOnMainSync(() -> Choreographer.getInstance().removeFrameCallback(callback));
        }
    }

    private void writeIdentity(String locale) throws Exception {
        JSONObject assets = new JSONObject(); hashAssets("public", assets);
        JSONObject identity = new JSONObject().put("test_class", getClass().getName()).put("locale", locale)
            .put("installed_target_apk_sha256", hash(new FileInputStream(instrumentation.getTargetContext().getPackageCodePath())))
            .put("installed_test_apk_sha256", hash(new FileInputStream(instrumentation.getContext().getPackageCodePath())))
            .put("installed_public_assets_sha256", assets);
        writeJson("identity", identity);
        Bundle status = new Bundle(); status.putString("driver_identity", identity.toString());
        status.putString("evidence_directory", evidenceDirectory.getAbsolutePath());
        instrumentation.sendStatus(2, status);
    }

    private void hashAssets(String path, JSONObject hashes) throws Exception {
        String[] children = instrumentation.getTargetContext().getAssets().list(path);
        if (children != null && children.length > 0) {
            for (String child : children) hashAssets(path + "/" + child, hashes);
        } else hashes.put(path, hash(instrumentation.getTargetContext().getAssets().open(path)));
    }

    private String hash(InputStream stream) throws Exception {
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        try (InputStream input = stream) {
            byte[] buffer = new byte[16384]; int read;
            while ((read = input.read(buffer)) != -1) digest.update(buffer, 0, read);
        }
        StringBuilder value = new StringBuilder();
        for (byte item : digest.digest()) value.append(String.format("%02x", item & 255));
        return value.toString();
    }

    private FileOutputStream privateOutput(File path) throws Exception {
        return new FileOutputStream(Os.open(path.getAbsolutePath(), OsConstants.O_WRONLY | OsConstants.O_CREAT | OsConstants.O_TRUNC, 0600));
    }

    private void writeJson(String name, JSONObject value) throws Exception {
        try (FileOutputStream file = privateOutput(new File(evidenceDirectory, name + ".json"))) {
            file.write(value.toString(2).getBytes(StandardCharsets.UTF_8));
        }
    }
}
