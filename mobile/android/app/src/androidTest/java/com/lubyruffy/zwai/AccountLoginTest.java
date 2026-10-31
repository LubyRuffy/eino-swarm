package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Instrumentation;
import android.app.Activity;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.net.Uri;
import android.os.Bundle;
import android.os.SystemClock;
import android.view.KeyEvent;
import android.view.MotionEvent;
import android.view.InputDevice;
import android.view.WindowInsets;
import android.view.accessibility.AccessibilityNodeInfo;
import android.view.inputmethod.InputMethodManager;
import android.webkit.WebView;
import com.getcapacitor.BridgeActivity;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.FileOutputStream;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.net.HttpURLConnection;
import java.net.InetSocketAddress;
import java.net.Socket;
import java.net.URL;
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

/** Native actions against the unchanged packaged web assets and a loopback fixture.
 * The fixture's synthetic CAS is not a production NOSEC identity or live model. */
@RunWith(AndroidJUnit4.class)
public class AccountLoginTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private String origin;
    private File evidence;
    private boolean secretOnScreen;
    private BridgeActivity activity;
    private final JSONArray nativeActions = new JSONArray();

    @Test
    public void accountLoginReviewModelsPCAndAnonymousPairing() throws Exception {
        origin = InstrumentationRegistry.getArguments().getString("fixture_origin", "");
        assertTrue("Only the owned loopback fixture is permitted", origin.matches("http://127\\.0\\.0\\.1:[0-9]+"));
        evidence = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "native-account");
        assertTrue(evidence.isDirectory() || evidence.mkdirs());
        Bundle identity = new Bundle();
        identity.putString("evidence_directory", evidence.getAbsolutePath());
        instrumentation.sendStatus(2, identity);
        try {
            assertEquals("pending", state().getString("review"));
            URL base = new URL(origin);
            try (Socket socket = new Socket()) {
                socket.connect(new InetSocketAddress(base.getHost(), base.getPort()), 3000);
                socket.setSoTimeout(3000);
                String request = "GET /fixture/loopback-ws HTTP/1.1\r\nHost: " + base.getAuthority()
                    + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n";
                socket.getOutputStream().write(request.getBytes(StandardCharsets.US_ASCII));
                String status = new BufferedReader(new InputStreamReader(socket.getInputStream(), StandardCharsets.US_ASCII)).readLine();
                assertTrue("The isolated device must complete a loopback WebSocket upgrade", status != null && status.startsWith("HTTP/1.1 101"));
            }
            write("loopback-network-proof.json", "{\"native_http\":true,\"native_websocket_upgrade\":true}");
            Intent launch = instrumentation.getTargetContext().getPackageManager().getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
            assertNotNull(launch);
            launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
            Activity reached = instrumentation.startActivitySync(launch);
            assertTrue(reached instanceof BridgeActivity);
            activity = (BridgeActivity) reached;
            nativeActions.put(new JSONObject().put("action", "native_app_launch").put("pid", android.os.Process.myPid())
                .put("activity", activity.getClass().getName()).put("activity_instance", System.identityHashCode(activity)));
            waitFor("Sign in with NOSEC");
            assertFalse("Fresh launch must not require a server URL", present("Server address"));
            assertFalse("Fresh launch must not require a Key", present("API Key"));
            save("01-default-no-configuration");
            click("Sign in with NOSEC");
            waitFor("User center");
            assertFalse("Server setup stays optional", present("Server address"));
            click("Other server");
            fill("Server address", origin);
            save("01a-native-server-form-roundtrip");
            click("Use this server");
            control("start=redirect");
            click("Sign in with NOSEC");
            waitFor("The endpoint rejected the request.");
            assertEquals("Gateway POST redirects must not reach another origin", 0, state().getJSONObject("counts").optInt("redirect_target"));
            assertEquals(1, state().getJSONObject("counts").optInt("redirect_sent"));
            assertEquals("A refused auth redirect cannot open a Browser", 0, state().getJSONObject("counts").optInt("browser_start"));
            save("01b-native-auth-redirect-refused");
            control("start=delay");
            long delayedAt = SystemClock.uptimeMillis();
            click("Sign in with NOSEC");
            waitFor("Checking account…");
            waitFor("The account operation timed out. Try again.");
            long elapsed = SystemClock.uptimeMillis() - delayedAt;
            assertTrue("The native POST must honor the 15 second deadline", elapsed >= 14000 && elapsed < 19500);
            waitCount("delay_cancelled", 1);
            assertEquals("A cancelled request cannot accept its late result", 0, state().getJSONObject("counts").optInt("delay_completed"));
            save("01c-native-auth-timeout");
            control("start=normal");
            click("Sign in with NOSEC");
            waitBrowser("Sign in to test NOSEC");
            save("02-system-browser");
            click("Sign in to test NOSEC");
            waitFor("Waiting for administrator approval");
            assertEquals(1, state().getJSONObject("counts").optInt("cas_callback"));
            foreground();
            waitFor("Waiting for administrator approval");
            assertRestricted();
            save("03-native-pending");
            assertNoBusinessBeforeReview();

            control("review=rejected");
            waitFor("Your account was rejected. Contact an administrator.");
            assertRestricted();
            save("04-native-rejected");
            assertNoBusinessBeforeReview();
            control("review=approved");
            control("catalog=redirect");
            String confirmation = state().getString("device_confirm_url");
            assertTrue("Only a public confirmation URL enters the browser", confirmation.startsWith(origin + "/mobile-login?code="));
            Intent browser = new Intent(Intent.ACTION_VIEW, Uri.parse(confirmation));
            browser.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK);
            instrumentation.getContext().startActivity(browser);
            waitBrowser("Authorize test device");
            click("Authorize test device");
            waitFor("Device authorized. Return to zwai.");
            assertEquals(1, state().getJSONObject("counts").optInt("browser_authorize"));
            foreground();
            waitFor("Could not load server models. Refresh to try again.");
            assertEquals("Managed model discovery must refuse redirects", 0, state().getJSONObject("counts").optInt("redirect_target"));
            assertTrue(state().getJSONObject("counts").optInt("catalog_redirect_sent") > 0);
            save("04b-native-catalog-redirect-refused");
            control("catalog=normal");
            click("Refresh account");
            waitFor("native-fixture-model");
            click("Chat history");
            waitFor("Message");
            control("model=redirect");
            fill("Message", "Native redirect acceptance");
            click("Send");
            waitCount("model_redirect_sent", 1);
            waitFor("307");
            assertEquals(1, state().getJSONObject("counts").optInt("model_redirect_sent"));
            assertEquals("Native model transport must refuse redirects", 0, state().getJSONObject("counts").optInt("redirect_target"));
            save("04c-native-model-redirect-refused");
            control("model=stall");
            fill("Message", "Native cancellation acceptance");
            click("Send");
            waitCount("model_stall_started", 1);
            click("Stop");
            waitCount("model_cancelled", 1);
            assertEquals("Stopped model cannot finish late", 0, state().getJSONObject("counts").optInt("model_completed"));
            save("04d-native-model-cancelled");
            control("model=normal");
            fill("Message", "Native authorization acceptance");
            click("Send");
            waitFor("Native account answer");
            save("05-server-model-without-PC");
            JSONObject beforePC = state();
            assertFalse("The model must work before any PC is offered", beforePC.getBoolean("pc_visible"));
            assertEquals(1, beforePC.getJSONObject("counts").optInt("model_successes"));
            assertEquals(0, beforePC.getJSONObject("counts").optInt("connect"));

            click("Back");
            click("Menu");
            click("User center");
            control("pc=show");
            click("Refresh account");
            waitFor("Connect Native PC");
            save("06-account-PC-selection");
            click("Connect Native PC");
            waitContaining("Native PC conversation");
            save("07-real-encrypted-PC");
            JSONObject connected = state().getJSONObject("counts");
            assertEquals("Only the selected PC is connected", 1, connected.optInt("connect"));
            assertEquals("Managed connection must not redeem a QR", 0, connected.optInt("qr_redeem"));
            assertTrue("The real Hub must have forwarded encrypted RPC frames", connected.optInt("peer_messages") > 0);
            if (present("Back")) click("Back");
            click("Menu");
            click("User center");
            click("Sign out");
            click("Close");
            waitFor("Paste and bind");
            JSONObject offer = get("/fixture/anonymous-offer");
            String uri = offer.getString("uri");
            String anonymous = state().getString("anonymous_origin");
            assertFalse("The QR fixture must use another origin", anonymous.equals(origin));
            assertTrue("Anonymous binding takes its address from the URI", uri.startsWith("pairlink:v1:" + anonymous + ":"));
            secretOnScreen = true;
            fill("Pairing URI", uri);
            click("Paste and bind");
            waitContaining("Native PC conversation");
            secretOnScreen = false;
            save("08-anonymous-real-encrypted-PC");
            JSONObject finalState = state();
            JSONObject counts = finalState.getJSONObject("counts");
            assertEquals(1, counts.optInt("qr_redeem"));
            assertEquals(1, counts.optInt("anonymous_origin_redeem"));
            assertEquals(0, counts.optInt("primary_origin_redeem"));
            assertTrue("Anonymous pairing must also dial its QR origin", counts.optInt("anonymous_origin_ws") > 0);
            assertEquals(0, counts.optInt("anonymous_authorization_leak"));
            assertEquals(0, counts.optInt("native_cookie_leak"));
            assertEquals(0, counts.optInt("invalid_pkce"));
            assertEquals(0, counts.optInt("invalid_model_contract"));
            write("fixture-state.json", finalState.toString(2));
        } catch (Exception | AssertionError failure) {
            try {
                write("failure-fixture-state.json", state().toString(2));
                CountDownLatch diagnostic = new CountDownLatch(1);
                AtomicReference<Boolean> pending = new AtomicReference<>();
                if (activity != null) activity.runOnUiThread(() -> {
                    try {
                        activity.getBridge().getWebView().evaluateJavascript("Boolean(localStorage.getItem('zwai.phone.gateway.pending'))", result -> {
                            pending.set("true".equals(result)); diagnostic.countDown();
                        });
                    } catch (RuntimeException ignored) { diagnostic.countDown(); }
                });
                diagnostic.await(2, TimeUnit.SECONDS);
                JSONObject boundary = new JSONObject().put("app_foreground", appForeground()).put("pending_observed", pending.get() != null);
                if (pending.get() != null) boundary.put("gateway_pending_present", pending.get());
                write("failure-native-boundary.json", boundary.toString(2));
                if (secretOnScreen) write("failure-phase.json", "{\"phase\":\"anonymous-redemption\",\"secret_screen_capture_omitted\":true}");
                else save("failure");
            } catch (Exception | AssertionError evidenceFailure) { failure.addSuppressed(evidenceFailure); }
            throw failure;
        }
    }

    private void foreground() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager().getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_REORDER_TO_FRONT | Intent.FLAG_ACTIVITY_SINGLE_TOP);
        instrumentation.getTargetContext().startActivity(launch);
        long until = SystemClock.uptimeMillis() + 5000;
        while (!appForeground() && SystemClock.uptimeMillis() < until) SystemClock.sleep(50);
        assertTrue("The actual zwai window must be foreground before native assertions", appForeground());
        assertFalse("System return must preserve the actual App Activity", activity.isDestroyed());
        nativeActions.put(new JSONObject().put("action", "system_launch_intent_return").put("pid", android.os.Process.myPid())
            .put("activity", activity.getClass().getName()).put("activity_instance", System.identityHashCode(activity)).put("app_foreground", true));
    }

    private void assertRestricted() throws Exception {
        for (String label : new String[] {"Message", "Your computers", "Server models", "Scan QR", "Menu", "Close", "Other server"}) {
            assertFalse("Unreviewed accounts cannot expose " + label, present(label));
        }
    }

    private void assertNoBusinessBeforeReview() throws Exception {
        JSONObject counts = state().getJSONObject("counts");
        for (String name : new String[] {"bootstrap", "models", "inference", "connect", "heartbeat"}) assertEquals("Pending native app must not call " + name, 0, counts.optInt(name));
    }

    private JSONObject control(String query) throws Exception { return get("/fixture/control?" + query); }
    private JSONObject state() throws Exception { return get("/fixture/state"); }

    private void waitCount(String name, int count) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 8000;
        while (SystemClock.uptimeMillis() < deadline) {
            if (state().getJSONObject("counts").optInt(name) >= count) return;
            SystemClock.sleep(100);
        }
        throw new AssertionError("Native request did not reach expected fixture phase: " + name);
    }

    private JSONObject get(String path) throws Exception {
        HttpURLConnection connection = (HttpURLConnection) new URL(origin + path).openConnection();
        connection.setConnectTimeout(5000); connection.setReadTimeout(5000);
        try {
            assertEquals("The loopback fixture must respond", 200, connection.getResponseCode());
            return new JSONObject(new String(connection.getInputStream().readAllBytes(), StandardCharsets.UTF_8)).getJSONObject("data");
        } finally { connection.disconnect(); }
    }

    private void waitBrowser(String label) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 25000;
        while (SystemClock.uptimeMillis() < deadline) {
            if (present(label)) return;
            for (String firstRun : new String[] {"Accept & continue", "Use without an account", "No thanks"}) {
                if (present(firstRun)) click(firstRun);
            }
            SystemClock.sleep(100);
        }
        throw new AssertionError("System browser did not reach " + label);
    }

    private AccessibilityNodeInfo waitFor(String label) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 25000;
        while (SystemClock.uptimeMillis() < deadline) {
            if (appForeground()) {
                if (domEntry(label) != null) return null;
            } else for (AccessibilityNodeInfo node : nodes()) if (visible(node) && named(node, label)) return node;
            SystemClock.sleep(100);
        }
        throw new AssertionError("Native screen did not reach " + label);
    }

    private void waitContaining(String text) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 25000;
        while (SystemClock.uptimeMillis() < deadline) {
            if (appForeground()) {
                JSONArray entries = dom().getJSONArray("entries");
                for (int i = 0; i < entries.length(); i++) if (entries.getJSONObject(i).getString("label").contains(text)) return;
            } else for (AccessibilityNodeInfo node : nodes()) if (visible(node) && (String.valueOf(node.getText()) + " " + node.getContentDescription()).contains(text)) return;
            SystemClock.sleep(100);
        }
        throw new AssertionError("Native screen did not contain " + text);
    }

    private boolean present(String label) throws Exception {
        if (appForeground()) return domEntry(label) != null;
        for (AccessibilityNodeInfo node : nodes()) if (visible(node) && named(node, label)) return true;
        return false;
    }

    private void click(String label) throws Exception {
        if (appForeground()) {
            waitFor(label);
            JSONObject entry = domEntry(label);
            assertNotNull("Current packaged DOM must expose the actual control", entry);
            assertTrue("Current packaged control must be enabled", entry.getBoolean("enabled"));
            touch(entry);
            return;
        }
        AccessibilityNodeInfo node = waitFor(label);
        for (int parent = 0; !node.isClickable() && parent < 4 && node.getParent() != null; parent++) node = node.getParent();
        assertTrue("The reached native control must be enabled: " + label, node.isEnabled());
        Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
        assertFalse("The reached native control needs visible bounds", bounds.isEmpty());
        assertTrue("The actual system browser must accept its semantic button action", node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
        nativeActions.put(new JSONObject().put("action", "native_browser_AX_click").put("label", label).put("bounds", bounds.toShortString()));
    }

    private void fill(String label, String text) throws Exception {
        waitFor(label);
        assertTrue("Only the known foreground zwai window can receive test input", appForeground());
        JSONObject entry = domEntry(label);
        assertNotNull("The real packaged form must expose this input", entry);
        assertTrue("The witnessed control must be an editable input", entry.getBoolean("editable"));
        touch(entry);
        instrumentation.sendKeySync(new KeyEvent(0, 0, KeyEvent.ACTION_DOWN, KeyEvent.KEYCODE_A, 0, KeyEvent.META_CTRL_ON));
        instrumentation.sendKeySync(new KeyEvent(0, 0, KeyEvent.ACTION_UP, KeyEvent.KEYCODE_A, 0, KeyEvent.META_CTRL_ON));
        instrumentation.sendStringSync(text);
        instrumentation.runOnMainSync(() -> ((InputMethodManager) activity.getSystemService(Activity.INPUT_METHOD_SERVICE)).hideSoftInputFromWindow(activity.getBridge().getWebView().getWindowToken(), 0));
        long until = SystemClock.uptimeMillis() + 3000;
        while (SystemClock.uptimeMillis() < until) {
            JSONObject current = domEntry(label);
            if (current != null && text.equals(current.optString("value"))) {
                boolean[] keyboard = {true};
                long keyboardDeadline = SystemClock.uptimeMillis() + 3000;
                do {
                    instrumentation.runOnMainSync(() -> {
                        WindowInsets insets = activity.getBridge().getWebView().getRootWindowInsets();
                        keyboard[0] = insets != null && insets.isVisible(WindowInsets.Type.ime());
                    });
                    if (!keyboard[0]) break;
                    SystemClock.sleep(50);
                } while (SystemClock.uptimeMillis() < keyboardDeadline);
                assertFalse("Native keyboard must be dismissed before the next physical touch", keyboard[0]);
                ui.waitForIdle(300, 2500);
                nativeActions.put(new JSONObject().put("action", "native_keyboard_roundtrip").put("field", label).put("matched", true));
                return;
            }
            SystemClock.sleep(100);
        }
        throw new AssertionError("Native keyboard input did not update the actual packaged field");
    }

    private boolean appForeground() {
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        return root != null && root.getPackageName() != null && instrumentation.getTargetContext().getPackageName().contentEquals(root.getPackageName());
    }

    /** Read-only observations compensate for API33's stale virtual AX subtree.
     * No storage, auth state, browser transition, fetch or DOM mutation occurs here. */
    private JSONObject dom() throws Exception {
        String script = "JSON.stringify((()=>{const tidy=s=>(s||'').replace(/\\s+/g,' ').trim();return {width:innerWidth,height:innerHeight,entries:[...document.querySelectorAll('button,input,textarea,label,h1,h2,p,span,[role=status],[role=alert]')].flatMap(e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e),x=r.x+r.width/2,y=r.y+r.height/2;if(!r.width||!r.height||x<0||y<0||x>=innerWidth||y>=innerHeight||s.visibility==='hidden'||s.display==='none'||e.closest('[inert],[aria-hidden=true]'))return [];return [{label:e.getAttribute('aria-label')||e.labels?.[0]?.innerText||e.placeholder||tidy(e.innerText),editable:e.matches('input,textarea'),enabled:!e.disabled,x:r.x,y:r.y,width:r.width,height:r.height,value:e.matches('input,textarea')?e.value:''}];})};})())";
        CountDownLatch ready = new CountDownLatch(1); AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(script, result -> { value.set(result); ready.countDown(); }));
        assertTrue("Read-only packaged DOM observation must return", ready.await(5, TimeUnit.SECONDS));
        return new JSONObject(new JSONArray("[" + value.get() + "]").getString(0));
    }

    private JSONObject domEntry(String label) throws Exception {
        JSONArray entries = dom().getJSONArray("entries");
        JSONObject found = null;
        for (int i = 0; i < entries.length(); i++) {
            JSONObject entry = entries.getJSONObject(i);
            if (label.equals(entry.getString("label")) && (found == null || entry.getBoolean("editable"))) found = entry;
        }
        return found;
    }

    private void touch(JSONObject entry) throws Exception {
        JSONObject viewport = dom();
        int[] geometry = new int[4];
        instrumentation.runOnMainSync(() -> { WebView web = activity.getBridge().getWebView(); web.getLocationOnScreen(geometry); geometry[2] = web.getWidth(); geometry[3] = web.getHeight(); });
        float x = (float) (geometry[0] + (entry.getDouble("x") + entry.getDouble("width") / 2) * geometry[2] / viewport.getDouble("width"));
        float y = (float) (geometry[1] + (entry.getDouble("y") + entry.getDouble("height") / 2) * geometry[3] / viewport.getDouble("height"));
        long at = SystemClock.uptimeMillis();
        MotionEvent down = MotionEvent.obtain(at, at, MotionEvent.ACTION_DOWN, x, y, 0), up = MotionEvent.obtain(at, at + 60, MotionEvent.ACTION_UP, x, y, 0);
        down.setSource(InputDevice.SOURCE_TOUCHSCREEN); up.setSource(InputDevice.SOURCE_TOUCHSCREEN);
        assertTrue(ui.injectInputEvent(down, true)); assertTrue(ui.injectInputEvent(up, true)); down.recycle(); up.recycle();
        nativeActions.put(new JSONObject().put("action", "native_touch_current_DOM_bounds").put("label", entry.getString("label"))
            .put("native_x", x).put("native_y", y).put("viewport_width", viewport.getDouble("width")).put("viewport_height", viewport.getDouble("height"))
            .put("webview_width", geometry[2]).put("webview_height", geometry[3]));
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.contentEquals(node.getText() == null ? "" : node.getText()) || label.contentEquals(node.getContentDescription() == null ? "" : node.getContentDescription());
    }

    private boolean visible(AccessibilityNodeInfo node) { return node.isVisibleToUser(); }

    private List<AccessibilityNodeInfo> nodes() {
        List<AccessibilityNodeInfo> all = new ArrayList<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root == null) return all;
        root.refresh();
        all.add(root);
        for (int i = 0; i < all.size(); i++) for (int j = 0; j < all.get(i).getChildCount(); j++) {
            AccessibilityNodeInfo child = all.get(i).getChild(j); if (child != null && child.refresh()) all.add(child);
        }
        return all;
    }

    private void save(String phase) throws Exception {
        if (appForeground()) {
            CountDownLatch rendered = new CountDownLatch(1);
            instrumentation.runOnMainSync(() -> activity.getBridge().getWebView()
                .postVisualStateCallback(SystemClock.uptimeMillis(), new WebView.VisualStateCallback() {
                    @Override public void onComplete(long requestId) { rendered.countDown(); }
                }));
            assertTrue("The observed packaged DOM must reach the real display before capture", rendered.await(5, TimeUnit.SECONDS));
            instrumentation.waitForIdleSync();
        }
        ui.waitForIdle(300, 3000);
        Bitmap screenshot = ui.takeScreenshot();
        assertNotNull("Native pixels must be captured", screenshot);
        try (FileOutputStream stream = new FileOutputStream(new File(evidence, phase + ".png"))) { assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, stream)); }
        screenshot.recycle();
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName())).put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription())).put("enabled", node.isEnabled()).put("visible", node.isVisibleToUser()).put("bounds", bounds.toShortString()));
        }
        write(phase + ".json", tree.toString(2));
        write(phase + "-native-actions.json", nativeActions.toString(2));
    }

    private void write(String name, String value) throws Exception {
        try (FileOutputStream stream = new FileOutputStream(new File(evidence, name))) { stream.write(value.getBytes(StandardCharsets.UTF_8)); }
    }
}
