package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.ClipboardManager;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.Bundle;
import android.os.ParcelFileDescriptor;
import android.os.SystemClock;
import android.view.InputDevice;
import android.view.MotionEvent;
import android.view.accessibility.AccessibilityNodeInfo;
import android.view.inputmethod.InputMethodManager;
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

/** Packaged Android rendering and native clipboard verification; no real host/model. */
@RunWith(AndroidJUnit4.class)
public class CodeHighlightTest {
    private static final String SOURCE = "// Offline sample\nconst char* title = \"signal\";\nint count = 7;\nif (count > 0) return count;\nconst char* detail = \"a deliberately long line stays inside a horizontally scrollable code panel\";";
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private final JSONArray actions = new JSONArray();
    private BridgeActivity activity;
    private File evidenceDirectory;

    @Test
    public void nativeCodeHighlightPreservesSourceCopyAndSystemThemes() throws Exception {
        evidenceDirectory = new File(instrumentation.getTargetContext().getExternalFilesDir(null),
            "code-highlight-" + UUID.randomUUID());
        assertTrue(evidenceDirectory.mkdir());
        Bundle identity = new Bundle();
        identity.putString("evidence_directory", evidenceDirectory.getAbsolutePath());
        identity.putString("driver_identity", new JSONObject()
            .put("driver", "issue91-packaged-native-code-highlight")
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
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView()
            .loadUrl("https://localhost/?mock=1&tick=0"));
        try {
            waitNativeBackControl(20000);
            waitButton("Back", 20000);
            clickNative("Back");
            waitButton("Open Sweep the unused exports", 10000);
            clickNative("Open Sweep the unused exports");
            waitButton("Send", 10000);
            setNativeComposer("```cpp\n" + SOURCE + "\n```");
            waitButton("Send", 8000);
            clickNative("Send");
            instrumentation.runOnMainSync(() -> ((InputMethodManager) activity
                .getSystemService(Activity.INPUT_METHOD_SERVICE))
                .hideSoftInputFromWindow(activity.getBridge().getWebView().getWindowToken(), 0));
            waitCode(10000);
            revealCode();
            setSystemNight(false);
            JSONObject light = assertCode(false);
            saveEvidence("cpp-light-source-and-colors");
            clickNative("Copy code");
            assertClipboardSource();
            saveEvidence("cpp-light-copy");
            setSystemNight(true);
            revealCode();
            JSONObject dark = assertCode(true);
            assertNotEquals("System themes must actually alter syntax colors",
                light.getJSONObject("colors").getString("keyword"),
                dark.getJSONObject("colors").getString("keyword"));
            saveEvidence("cpp-dark-source-and-colors");
            clickNative("Copy code");
            assertClipboardSource();
            saveEvidence("cpp-dark-copy");
        } catch (Exception | AssertionError failure) {
            try { saveEvidence("code-highlight-native-failure"); }
            catch (Exception | AssertionError capture) { failure.addSuppressed(capture); }
            throw failure;
        } finally {
            activity.finish();
        }
    }

    private void waitNativeBackControl(long timeout) throws Exception {
        // loadUrl navigates asynchronously. An AX control proves the packaged
        // document is painted before its first DOM callback is interpreted.
        long deadline = SystemClock.uptimeMillis() + timeout;
        while (SystemClock.uptimeMillis() < deadline) {
            for (AccessibilityNodeInfo node : nodes()) {
                if (node.isVisibleToUser() && ("Back".contentEquals(String.valueOf(node.getText()))
                    || "Back".contentEquals(String.valueOf(node.getContentDescription())))) return;
            }
            SystemClock.sleep(100);
        }
        fail("Packaged native Back control did not become visible after navigation");
    }

    private void setNativeComposer(String text) throws Exception {
        for (AccessibilityNodeInfo node : nodes()) {
            if (node.isEditable() && node.isVisibleToUser() && node.isEnabled()) {
                Bundle args = new Bundle();
                args.putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, text);
                assertTrue("Actual WebView editor must accept native text input",
                    node.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, args));
                actions.put(new JSONObject().put("action", "native_AX_set_text").put("text", text));
                long deadline = SystemClock.uptimeMillis() + 5000;
                while (!text.equals(dom().optString("composer")) && SystemClock.uptimeMillis() < deadline)
                    SystemClock.sleep(100);
                assertEquals(text, dom().getString("composer"));
                return;
            }
        }
        fail("No visible native composer editor");
    }

    private void waitCode(long timeout) throws Exception {
        long deadline = SystemClock.uptimeMillis() + timeout;
        while (!SOURCE.equals(dom().optString("source")) && SystemClock.uptimeMillis() < deadline)
            SystemClock.sleep(100);
        assertEquals(SOURCE, dom().getString("source"));
    }

    private void setSystemNight(boolean dark) throws Exception {
        String command = "cmd uimode night " + (dark ? "yes" : "no");
        try (ParcelFileDescriptor fd = ui.executeShellCommand(command);
             FileInputStream input = new FileInputStream(fd.getFileDescriptor())) {
            byte[] output = input.readAllBytes();
            actions.put(new JSONObject().put("action", "owned_emulator_system_theme")
                .put("command", command).put("response", new String(output, StandardCharsets.UTF_8)));
        }
        long deadline = SystemClock.uptimeMillis() + 8000;
        while (dom().getBoolean("dark") != dark && SystemClock.uptimeMillis() < deadline)
            SystemClock.sleep(100);
        assertEquals("Actual packaged system theme must match", dark, dom().getBoolean("dark"));
    }

    private JSONObject assertCode(boolean dark) throws Exception {
        JSONObject state = dom();
        assertEquals(SOURCE, state.getString("source"));
        assertEquals(dark, state.getBoolean("dark"));
        JSONObject colors = state.getJSONObject("colors");
        for (String kind : new String[] {"keyword", "string", "comment", "operator"}) {
            assertTrue("Actual nonempty syntax token for " + kind, state.getJSONArray("kinds").toString().contains(kind));
            assertNotEquals("Syntax token must differ from plain code: " + kind,
                colors.getString("base"), colors.getString(kind));
        }
        assertNotEquals(colors.getString("keyword"), colors.getString("string"));
        double width = state.getJSONObject("viewport").getDouble("width");
        assertTrue("Actual phone viewport must be narrow", width >= 320 && width <= 430);
        assertTrue("The page must not overflow horizontally", state.getDouble("document_width") <= width + 1);
        JSONObject pre = state.getJSONObject("pre");
        assertTrue(pre.getDouble("x") >= -1 && pre.getDouble("x") + pre.getDouble("width") <= width + 1);
        assertTrue("Long code scrolls inside its panel", pre.getDouble("scroll_width") > pre.getDouble("client_width"));
        assertTrue("Highlighted code must be visible in the native screenshot", state.getBoolean("code_visible"));
        return state;
    }

    private void assertClipboardSource() throws Exception {
        AtomicReference<String> text = new AtomicReference<>();
        JSONArray observations = new JSONArray();
        long started = SystemClock.uptimeMillis();
        long deadline = started + 5000;
        do {
            AtomicReference<Boolean> focused = new AtomicReference<>();
            AtomicReference<Boolean> hasClip = new AtomicReference<>();
            text.set(null);
            instrumentation.runOnMainSync(() -> {
                focused.set(activity.hasWindowFocus());
                ClipboardManager clipboard = (ClipboardManager) activity.getSystemService(Activity.CLIPBOARD_SERVICE);
                hasClip.set(clipboard.hasPrimaryClip());
                if (clipboard.hasPrimaryClip() && clipboard.getPrimaryClip().getItemCount() > 0)
                    text.set(clipboard.getPrimaryClip().getItemAt(0).coerceToText(activity).toString());
            });
            observations.put(new JSONObject().put("elapsed_ms", SystemClock.uptimeMillis() - started)
                .put("activity_focused", focused.get()).put("has_primary_clip", hasClip.get())
                .put("exact_source", SOURCE.equals(text.get())));
            if (SOURCE.equals(text.get())) break;
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < deadline);
        actions.put(new JSONObject().put("action", "bounded_native_clipboard_observation")
            .put("observations", observations).put("buttons_after_copy", dom().getJSONArray("buttons")));
        assertEquals("Native clipboard must retain exact original source", SOURCE, text.get());
        actions.put(new JSONObject().put("action", "native_clipboard_read").put("exact_source", text.get()));
    }

    private void revealCode() throws Exception {
        for (int i = 0; i < 6; i++) {
            JSONObject state = dom();
            if (state.getBoolean("code_visible")) return;
            JSONObject scroll = state.getJSONObject("transcript");
            double x = scroll.getDouble("x") + scroll.getDouble("width") - 12;
            double top = scroll.getDouble("y") + 24;
            double bottom = scroll.getDouble("y") + scroll.getDouble("height") - 24;
            boolean above = state.getJSONObject("pre").getDouble("y") < top;
            swipeCSS(x, above ? top : bottom, x, above ? bottom : top, state.getJSONObject("viewport"));
        }
        assertTrue("Code panel must be visible after bounded native swipes", dom().getBoolean("code_visible"));
    }

    private void swipeCSS(double x1, double y1, double x2, double y2, JSONObject viewport) throws Exception {
        int[] g = new int[4];
        instrumentation.runOnMainSync(() -> { WebView v = activity.getBridge().getWebView();
            v.getLocationOnScreen(g); g[2] = v.getWidth(); g[3] = v.getHeight(); });
        JSONObject before = dom().getJSONObject("transcript");
        long now = SystemClock.uptimeMillis();
        for (int i = 0; i <= 12; i++) {
            if (i > 0) SystemClock.sleep(25);
            float x = (float) (g[0] + (x1 + (x2 - x1) * i / 12) * g[2] / viewport.getDouble("width"));
            float y = (float) (g[1] + (y1 + (y2 - y1) * i / 12) * g[3] / viewport.getDouble("height"));
            int action = i == 0 ? MotionEvent.ACTION_DOWN : i == 12 ? MotionEvent.ACTION_UP : MotionEvent.ACTION_MOVE;
            MotionEvent event = MotionEvent.obtain(now, SystemClock.uptimeMillis(), action, x, y, 0);
            event.setSource(InputDevice.SOURCE_TOUCHSCREEN); assertTrue(ui.injectInputEvent(event, true)); event.recycle();
        }
        ui.waitForIdle(300, 3000);
        actions.put(new JSONObject().put("action", "native_swipe_current_transcript_bounds")
            .put("CSS_x1", x1).put("CSS_y1", y1).put("CSS_x2", x2).put("CSS_y2", y2)
            .put("duration_ms", SystemClock.uptimeMillis() - now).put("before", before)
            .put("after", dom().getJSONObject("transcript")));
    }

    private JSONObject dom() throws Exception {
        String script = "JSON.stringify((() => {const tidy=s=>(s||'').replace(/\\s+/g,' ').trim();"
            + "const code=[...document.querySelectorAll('[data-testid=markdown-code] code')].find(e=>e.className==='language-cpp');"
            + "const pre=code?.closest('pre'),sc=document.querySelector('[data-testid=transcript]');"
            + "const rect=e=>{const r=e?.getBoundingClientRect();return r?{x:r.x,y:r.y,width:r.width,height:r.height,scroll_width:e.scrollWidth,client_width:e.clientWidth,scroll_top:e.scrollTop,scroll_height:e.scrollHeight,client_height:e.clientHeight}:{};};"
            + "const button=b=>{const r=b.getBoundingClientRect(),s=getComputedStyle(b),cx=r.x+r.width/2,cy=r.y+r.height/2;return {label:b.getAttribute('aria-label')||tidy(b.innerText),title:b.title,enabled:!b.disabled,visible:r.width>0&&r.height>0&&s.display!=='none'&&s.visibility==='visible'&&Number(s.opacity)>0,hit:cx>0&&cy>0&&cx<innerWidth&&cy<innerHeight&&b.contains(document.elementFromPoint(cx,cy)),x:r.x,y:r.y,width:r.width,height:r.height};};"
            + "const tokens=code?[...code.querySelectorAll('span')]:[],kinds=tokens.map(t=>t.className.replace('text-syntax-','')).filter(Boolean);"
            + "const colors={base:code?getComputedStyle(code).color:''};for(const kind of ['keyword','string','comment','operator']){const e=tokens.find(t=>t.classList.contains('text-syntax-'+kind));colors[kind]=e?getComputedStyle(e).color:'';}"
            + "const r=pre?.getBoundingClientRect(),sr=sc?.getBoundingClientRect();"
            + "return {url:location.href,dark:document.documentElement.classList.contains('dark'),source:code?.textContent||'',kinds,colors,pre:rect(pre),transcript:rect(sc),code_visible:!!r&&!!sr&&r.top>=sr.top&&r.bottom<=sr.bottom&&r.width>0,document_width:document.documentElement.scrollWidth,composer:document.querySelector('textarea')?.value||'',buttons:[...document.querySelectorAll('button')].map(button),viewport:{width:innerWidth,height:innerHeight},tokens:tokens.map(t=>({text:t.textContent,kind:t.className,color:getComputedStyle(t).color}))};})())";
        CountDownLatch ready = new CountDownLatch(1); AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(script, result -> {
            value.set(result); ready.countDown(); }));
        assertTrue("Read-only packaged DOM callback must return", ready.await(5, TimeUnit.SECONDS));
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
