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
import org.junit.Rule;
import org.junit.rules.TestWatcher;
import org.junit.runner.Description;
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
    @Rule public final TestWatcher failureEvidence = new TestWatcher() {
        @Override protected void failed(Throwable failure, Description description) {
            if (evidence == null) return;
            try {
                write("failure-fixture-state.json", state().toString(2));
                if (secretOnScreen) write("failure-phase.json", "{\"secret_screen_capture_omitted\":true}");
                else if (!new File(evidence, "failure.png").exists()) save("failure");
            } catch (Exception | AssertionError observation) { failure.addSuppressed(observation); }
        }
    };

    @Test
    public void preparePreviousAnonymousAndPersonalData() throws Exception {
        beginCase("native-upgrade-prepare");
        launch();
        waitFor("Sign in with NOSEC");
        assertFalse("Previous public launch needs no Gateway URL", present("Server address"));
        click("Connect a model");
        addPersonalModel();
        click("Menu");
        click("Add a PC");
        bindAnonymous();
        save("01-public51-personal-and-anonymous-PC");
        assertEquals("Preparation redeems one real anonymous capability", 1, state().getJSONObject("counts").optInt("anonymous_origin_redeem"));
        write("fixture-state.json", state().toString(2));
    }

    @Test
    public void accountLoginReviewModelsPCAndAnonymousPairing() throws Exception {
        beginCase("native-account");
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
            launch();
            boolean upgraded = "true".equals(InstrumentationRegistry.getArguments().getString("upgrade_prepared"));
            write("upgrade-qr-counter-baseline.json", new JSONObject().put("upgraded", upgraded).put("qr_redeem", count("qr_redeem")).toString(2));
            if (upgraded) {
                waitContaining("Native PC conversation");
                assertEquals("Upgrade reconnects the saved ticket without redeeming another QR", 1, state().getJSONObject("counts").optInt("anonymous_origin_redeem"));
                click("Menu"); click("Models");
                waitContaining("Native personal");
                save("01-upgrade-preserved-personal-provider");
                click("Close"); click("Menu"); click("User center");
            } else {
                waitFor("Sign in with NOSEC");
                assertFalse("Fresh launch must not require a server URL", present("Server address"));
                assertFalse("Fresh launch must not require a Key", present("API Key"));
                save("01-default-no-configuration");
                click("Sign in with NOSEC");
            }
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
            write("managed-connect-counters.json", connected.toString(2));
            assertEquals("Only the selected PC is connected", 1, connected.optInt("connect"));
            assertEquals("Managed connection must not redeem a QR", upgraded ? 1 : 0, connected.optInt("qr_redeem"));
            assertTrue("The real Hub must have forwarded encrypted RPC frames", connected.optInt("peer_messages") > 0);
            if (present("Back")) click("Back");
            click("Menu");
            click("User center");
            click("Sign out");
            click("Close");
            if (!present("Pairing URI")) { click("Menu"); click("Add a PC"); }
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
            assertEquals(upgraded ? 2 : 1, counts.optInt("qr_redeem"));
            assertEquals(upgraded ? 2 : 1, counts.optInt("anonymous_origin_redeem"));
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

    @Test
    public void independentWebRoutesAndPermissionRecovery() throws Exception {
        beginCase("native-web-routing");
        launch();
        waitContaining("Native PC conversation");
        control("review=approved&model=tools&web=normal&pc=hide");
        click("Menu"); click("User center");
        click("Sign in with NOSEC");
        waitBrowser("Sign in to test NOSEC"); click("Sign in to test NOSEC");
        waitBrowser("Authorize test device"); click("Authorize test device");
        waitFor("Device authorized. Return to zwai."); foreground();
        waitFor("Phone chat Web tools");
        assertSelect("Phone chat Web tools", "auto");
        assertSelect("PC conversation Web tools", "inherit");
        choose("Phone chat Web tools", "Off: ordinary model chat", "off");
        assertSelect("PC conversation Web tools", "inherit");
        choose("PC conversation Web tools", "Run on this PC", "local");
        assertSelect("Phone chat Web tools", "off");
        choose("Phone chat Web tools", "Automatic: cloud when signed in", "auto");
        choose("PC conversation Web tools", "Follow this PC's settings", "inherit");
        save("01-independent-web-settings");
        click("Chat history");
        assertNoInlineWebRouting();
        String gatewaySource = chooseChatModel("native-fixture-model", "Server · Native gateway");
        int gatewayCalls = count("gateway_model_calls");
        int gatewayPersonalBefore = count("personal_model_calls");
        assertEquals("No personal inference precedes the Gateway stage", 0, gatewayPersonalBefore);
        int search = count("web_search"), fetch = count("web_fetch");
        send("Gateway native cloud evidence");
        waitCount("web_search", search + 1); waitCount("web_fetch", fetch + 1);
        waitFor("Native cloud evidence answer");
        waitCount("gateway_model_calls", gatewayCalls + 3);
        assertEquals("The gateway model must execute its own three-request Web loop", gatewayCalls + 3, count("gateway_model_calls"));
        assertEquals("Gateway stage must not call the personal provider", gatewayPersonalBefore, count("personal_model_calls"));
        assertEquals("Gateway stage performs exactly one search", search + 1, count("web_search"));
        assertEquals("Gateway stage performs exactly one fetch", fetch + 1, count("web_fetch"));
        write("gateway-route-counters.json", new JSONObject().put("before_personal_model_calls", gatewayPersonalBefore)
            .put("after_personal_model_calls", count("personal_model_calls"))
            .put("before_search", search).put("after_search", count("web_search"))
            .put("before_fetch", fetch).put("after_fetch", count("web_fetch"))
            .put("before_gateway_model_calls", gatewayCalls)
            .put("after_gateway_model_calls", count("gateway_model_calls"))
            .put("selected_model", dom().getString("composer_model"))
            .put("source", gatewaySource).toString(2));
        save("02-gateway-cloud-search-fetch");

        chooseChatModel("native-personal-model", "Native personal");
        int personal = count("personal_model_calls");
        send("Personal native cloud evidence");
        waitCount("web_search", search + 2); waitCount("web_fetch", fetch + 2);
        waitCount("personal_model_calls", personal + 3);
        waitFor("Native cloud evidence answer");
        assertNoInlineWebRouting();
        save("03-personal-model-cloud-search-fetch");

        control("web=denied");
        send("Denied native cloud evidence");
        waitCount("web_denied", 1);
        waitContaining("Cloud Web tool access is unavailable");
        save("04-cloud-permission-denied");
        int beforeOff = count("web_search");
        click("User center");
        choose("Phone chat Web tools", "Off: ordinary model chat", "off");
        assertSelect("PC conversation Web tools", "inherit");
        click("Close");
        waitContaining("Cloud Web tool access is unavailable");
        send("Continue without Web tools");
        waitFor("Native ordinary answer");
        assertEquals("Disabling phone tools resumes ordinary chat without another cloud call", beforeOff, count("web_search"));
        witnessTranscriptTail("Native ordinary answer", "off-answer-visibility");
        save("05-off-recovers-same-personal-chat");

        click("User center");
        choose("Phone chat Web tools", "Cloud server", "cloud");
        click("Close");
        control("web=stall");
        send("Stop native cloud work");
        waitCount("web_stall_started", 1);
        save("06-cloud-tool-running");
        click("Stop");
        waitCount("web_cancelled", 1);
        assertEquals("The stopped Web call cannot complete later", 0, count("web_completed"));
        witnessTranscriptTail("Stop native cloud work", "stopped-request-visibility");
        assertFalse("A cancelled Web call cannot leave any Stop control active", present("Stop"));
        write("stopped-ui-and-server-state.json", new JSONObject().put("stop_control_present", present("Stop"))
            .put("message_enabled", domEntry("Message").getBoolean("enabled"))
            .put("web_cancelled", count("web_cancelled")).put("web_completed", count("web_completed")).toString(2));
        save("07-cloud-tool-stopped");
        control("web=normal&pc=show");
        click("User center");
        assertSelect("Phone chat Web tools", "cloud");
        assertSelect("PC conversation Web tools", "inherit");
        click("Refresh account");
        click("Connect Native PC");
        waitFor("Open Native PC conversation");
        assertNoInlineWebRouting();
        click("Menu");
        click("User center");
        waitContaining("Next turn Web tools: Follow PC (last refreshed) · Run on this PC");
        save("08-PC-route-in-user-center");
        click("Close");
        click("Open Native PC conversation");
        waitFor("User center");
        assertNoInlineWebRouting();
        click("User center");
        waitContaining("Next turn Web tools: Follow PC (last refreshed) · Run on this PC");
        click("Close");
        send("Native PC follows its own Web tools");
        long until = SystemClock.uptimeMillis() + 8000;
        JSONObject snapshot;
        do { snapshot = state(); if (snapshot.has("pc_turn_web_execution")) break; SystemClock.sleep(100); } while (SystemClock.uptimeMillis() < until);
        assertEquals("Actual RPC must retain inherited thread execution", "", snapshot.getString("pc_thread_web_execution"));
        assertEquals("The durable PC turn captures its own auto setting", "auto", snapshot.getString("pc_turn_web_execution"));
        assertEquals("The anonymous PC keeps local execution despite the phone account", "local", snapshot.getJSONObject("pc_web_status").getString("execution"));
        assertEquals(0, count("personal_mobile_token_leak"));
        assertEquals(0, count("invalid_web_contract"));
        assertEquals(0, count("invalid_model_contract"));
        save("08-PC-follows-its-own-local-execution");
        write("fixture-state.json", snapshot.toString(2));
    }

    private void assertNoInlineWebRouting() throws Exception {
        assertFalse("The conversation must not show an inline Web settings shortcut", present("Web settings"));
        assertFalse("Routing summaries belong in settings or the model picker", dom().getBoolean("web_routing_visible"));
    }

    private void beginCase(String name) throws Exception {
        origin = InstrumentationRegistry.getArguments().getString("fixture_origin", "");
        assertTrue("Only the owned loopback fixture is permitted", origin.matches("http://127\\.0\\.0\\.1:[0-9]+"));
        evidence = new File(instrumentation.getTargetContext().getExternalFilesDir(null), name);
        assertTrue(evidence.isDirectory() || evidence.mkdirs());
        Bundle identity = new Bundle(); identity.putString("evidence_directory", evidence.getAbsolutePath()); instrumentation.sendStatus(2, identity);
    }

    private void launch() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager().getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch); launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity reached = instrumentation.startActivitySync(launch); assertTrue(reached instanceof BridgeActivity); activity = (BridgeActivity) reached;
        nativeActions.put(new JSONObject().put("action", "native_app_launch").put("pid", android.os.Process.myPid()).put("activity", activity.getClass().getName()).put("activity_instance", System.identityHashCode(activity)));
    }

    private void addPersonalModel() throws Exception {
        fill("Name", "Native personal"); fill("Base URL", origin + "/personal/v1"); fill("API key", "native-personal-key");
        click("Discover models"); assertSelect("Default model", "native-personal-model"); click("Save"); waitFor("Menu");
    }

    private void bindAnonymous() throws Exception {
        String uri = get("/fixture/anonymous-offer").getString("uri");
        secretOnScreen = true; fill("Pairing URI", uri); click("Paste and bind"); click("Native PC"); waitContaining("Native PC conversation"); secretOnScreen = false;
    }

    private String chooseChatModel(String model, String expectedSource) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 5000;
        assertTrue("Only the actual foreground app may receive a model selection", appForeground());
        if (!dom().getBoolean("model_dialog_open")) touch(stableModelControl("Model", false, deadline));
        JSONObject radio = stableModelControl(model, true, deadline);
        String source = radio.getString("model_source");
        assertEquals("The picker must identify the selected model's source", expectedSource, source);
        assertTrue("Model input targets the current unique dialog radio", appForeground());
        touch(radio);
        while (SystemClock.uptimeMillis() < deadline) {
            JSONObject observed = dom();
            if (!observed.getBoolean("model_dialog_open") && model.equals(observed.getString("composer_model"))) {
                nativeActions.put(new JSONObject().put("action", "native_model_radio_readback")
                    .put("model", model).put("radio", radio)
                    .put("composer_model", observed.getString("composer_model"))
                    .put("source", source));
                return source;
            }
            SystemClock.sleep(50);
        }
        throw new AssertionError("The actual composer did not retain the selected model: " + model);
    }

    private JSONObject stableModelControl(String label, boolean radio, long deadline) throws Exception {
        JSONObject previous = null;
        int stable = 0;
        while (SystemClock.uptimeMillis() < deadline) {
            JSONArray entries = dom().getJSONArray("entries"); JSONObject found = null;
            for (int i = 0; i < entries.length(); i++) {
                JSONObject candidate = entries.getJSONObject(i);
                if (!label.equals(candidate.getString("label")) || !candidate.getBoolean("visible") || !candidate.getBoolean("enabled")
                    || !candidate.getBoolean("hit") || candidate.getBoolean("in_model_dialog") != radio
                    || !(radio ? "radio" : "button").equals(candidate.getString("role"))) continue;
                assertNull("The actual model control must be unique", found); found = candidate;
            }
            if (found != null && previous != null
                && Math.abs(found.getDouble("x") - previous.getDouble("x")) < .5
                && Math.abs(found.getDouble("y") - previous.getDouble("y")) < .5
                && Math.abs(found.getDouble("width") - previous.getDouble("width")) < .5
                && Math.abs(found.getDouble("height") - previous.getDouble("height")) < .5) stable++;
            else stable = 0;
            if (found != null && stable >= 2) return found;
            previous = found; SystemClock.sleep(50);
        }
        throw new AssertionError("A current hittable model control was not stable: " + label);
    }
    private void witnessTranscriptTail(String label, String evidenceName) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 5000;
        int stable = 0, swipes = 0, earlierTranscriptSwipes = 0;
        for (int i = 0; i < nativeActions.length(); i++) if ("native_transcript_swipe".equals(nativeActions.getJSONObject(i).optString("action"))) earlierTranscriptSwipes++;
        JSONObject initial = dom(), initialTranscript = initial.getJSONObject("transcript_geometry");
        JSONArray initialTargets = new JSONArray(); boolean initialClear = false;
        for (int i = 0; i < initial.getJSONArray("entries").length(); i++) {
            JSONObject entry = initial.getJSONArray("entries").getJSONObject(i);
            if (label.equals(entry.getString("label")) && entry.getBoolean("in_transcript")) { initialTargets.put(entry); initialClear |= entry.getBoolean("unobscured"); }
        }
        boolean initiallyCoveredWithoutActorScroll = "Native ordinary answer".equals(label) && earlierTranscriptSwipes == 0 && !initialClear;
        write(evidenceName + "-before-native-scroll.json", new JSONObject().put("label", label)
            .put("targets", initialTargets).put("transcript", initialTranscript)
            .put("composer_top", initial.getDouble("composer_top")).put("earlier_native_transcript_swipes", earlierTranscriptSwipes)
            .put("initially_covered_without_actor_transcript_scroll", initiallyCoveredWithoutActorScroll).toString(2));
        save(evidenceName + "-before-native-scroll");
        while (SystemClock.uptimeMillis() < deadline) {
            JSONObject observed = dom(), transcript = observed.getJSONObject("transcript_geometry");
            JSONObject target = null;
            JSONArray entries = observed.getJSONArray("entries");
            for (int i = 0; i < entries.length(); i++) {
                JSONObject entry = entries.getJSONObject(i);
                if (label.equals(entry.getString("label")) && entry.getBoolean("in_transcript") && entry.getBoolean("unobscured")) target = entry;
            }
            boolean tail = transcript.getDouble("scroll_top") + transcript.getDouble("client_height") >= transcript.getDouble("scroll_height") - 2;
            if (target != null && tail && swipes > 0) {
                if (++stable >= 3) {
                    write(evidenceName + ".json", new JSONObject().put("label", label).put("target", target)
                        .put("transcript", transcript).put("composer_top", observed.getDouble("composer_top"))
                        .put("tail_reached_by_native_scroll", true).put("native_swipes", swipes)
                        .put("initial_scroll_top", initialTranscript.getDouble("scroll_top")).toString(2));
                    save(evidenceName + "-after-native-scroll");
                    nativeActions.put(new JSONObject().put("action", "observed_default_transcript_position")
                        .put("initially_covered_without_actor_scroll", initiallyCoveredWithoutActorScroll)
                        .put("no_new_auto_follow_contract_asserted", true));
                    return;
                }
                SystemClock.sleep(50);
            } else {
                stable = 0;
                nativeTranscriptSwipe(transcript);
                swipes++;
            }
        }
        throw new AssertionError("Native transcript cannot expose an unobscured tail item: " + label);
    }

    private void nativeTranscriptSwipe(JSONObject transcript) throws Exception {
        assertTrue("Only the actual foreground app can receive transcript scrolling", appForeground());
        JSONObject viewport = dom(); int[] geometry = new int[4];
        instrumentation.runOnMainSync(() -> { WebView web = activity.getBridge().getWebView(); web.getLocationOnScreen(geometry); geometry[2] = web.getWidth(); geometry[3] = web.getHeight(); });
        double cssX = transcript.getDouble("x") + transcript.getDouble("width") * .65;
        double cssStart = transcript.getDouble("bottom") - 20, cssEnd = transcript.getDouble("y") + 40;
        assertTrue("Transcript swipe stays above the composer and inside its own scroll area", cssStart > cssEnd && cssStart < viewport.getDouble("composer_top"));
        float x = (float) (geometry[0] + cssX * geometry[2] / viewport.getDouble("width"));
        float start = (float) (geometry[1] + cssStart * geometry[3] / viewport.getDouble("height"));
        float end = (float) (geometry[1] + cssEnd * geometry[3] / viewport.getDouble("height"));
        long at = SystemClock.uptimeMillis();
        MotionEvent down = MotionEvent.obtain(at, at, MotionEvent.ACTION_DOWN, x, start, 0); down.setSource(InputDevice.SOURCE_TOUCHSCREEN); assertTrue(ui.injectInputEvent(down, true)); down.recycle();
        for (int step = 1; step <= 8; step++) { MotionEvent move = MotionEvent.obtain(at, at + step * 30, MotionEvent.ACTION_MOVE, x, start + (end - start) * step / 8, 0); move.setSource(InputDevice.SOURCE_TOUCHSCREEN); assertTrue(ui.injectInputEvent(move, true)); move.recycle(); }
        MotionEvent finish = MotionEvent.obtain(at, at + 270, MotionEvent.ACTION_UP, x, end, 0); finish.setSource(InputDevice.SOURCE_TOUCHSCREEN); assertTrue(ui.injectInputEvent(finish, true)); finish.recycle();
        nativeActions.put(new JSONObject().put("action", "native_transcript_swipe").put("start_y", cssStart).put("end_y", cssEnd));
        SystemClock.sleep(150);
    }

    private void send(String text) throws Exception { fill("Message", text); click("Send"); }
    private int count(String name) throws Exception { return state().getJSONObject("counts").optInt(name); }

    private void assertSelect(String label, String expected) throws Exception {
        waitFor(label);
        long until = SystemClock.uptimeMillis() + 3000;
        while (SystemClock.uptimeMillis() < until) { JSONObject entry = domEntry(label); if (entry != null && expected.equals(entry.optString("value"))) return; SystemClock.sleep(50); }
        assertEquals("The packaged select must retain " + label, expected, domEntry(label).getString("value"));
    }

    private void choose(String label, String option, String value) throws Exception {
        click(label);
        long until = SystemClock.uptimeMillis() + 5000;
        boolean selected = false;
        while (!selected && SystemClock.uptimeMillis() < until) {
            for (AccessibilityNodeInfo node : nodes()) if (visible(node) && named(node, option)) {
                for (int parent = 0; !node.isClickable() && parent < 4 && node.getParent() != null; parent++) node = node.getParent();
                selected = node.performAction(AccessibilityNodeInfo.ACTION_CLICK); break;
            }
            if (!selected) SystemClock.sleep(100);
        }
        assertTrue("Android's native option menu must select " + option, selected);
        assertSelect(label, value);
        nativeActions.put(new JSONObject().put("action", "native_select_option").put("field", label).put("value", value));
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
        for (String label : new String[] {"Message", "Your computers", "Server models", "Scan QR", "Menu", "Close", "Other server", "Phone chat Web tools", "PC conversation Web tools"}) {
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
                reveal(label);
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
                for (int i = 0; i < entries.length(); i++) if (entries.getJSONObject(i).getBoolean("visible") && entries.getJSONObject(i).getString("label").contains(text)) return;
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
            if (current != null && (text.equals(current.optString("value")) || current.optBoolean("password_present"))) {
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
        String script = "JSON.stringify((()=>{const tidy=s=>(s||'').replace(/\\s+/g,' ').trim();const transcript=document.querySelector('[data-testid=transcript]'),t=transcript?.getBoundingClientRect(),composer=document.querySelector('textarea[aria-label=Message]')?.closest('form'),composerTop=composer?.getBoundingClientRect().top??innerHeight;return {width:innerWidth,height:innerHeight,composer_top:composerTop,transcript_geometry:t?{x:t.x,y:t.y,width:t.width,height:t.height,bottom:Math.min(t.bottom,composerTop),scroll_top:transcript.scrollTop,scroll_height:transcript.scrollHeight,client_height:transcript.clientHeight}:null,model_dialog_open:!!document.querySelector('[role=dialog][aria-label=Model]'),composer_model:tidy(document.querySelector('button[aria-label=Model]')?.textContent),web_routing_visible:[...document.querySelectorAll('[data-testid=direct-model-source],[data-testid=direct-web-status],[data-testid=pc-web-status]')].some(e=>!e.closest('[inert],[aria-hidden=true]')&&e.getClientRects().length>0),entries:[...document.querySelectorAll('button,input,textarea,select,label,h1,h2,p,span,[role=status],[role=alert]')].flatMap(e=>{const r=e.getBoundingClientRect(),s=getComputedStyle(e),x=r.x+r.width/2,y=r.y+r.height/2;if(!r.width||!r.height||s.visibility==='hidden'||s.display==='none'||e.closest('[inert],[aria-hidden=true]'))return [];return [{label:e.getAttribute('aria-label')||e.labels?.[0]?.innerText||e.placeholder||tidy(e.innerText),role:e.getAttribute('role')||e.tagName.toLowerCase(),in_model_dialog:e.closest('[role=dialog]')?.getAttribute('aria-label')==='Model',model_source:e.getAttribute('role')==='radio'?tidy(e.parentElement?.querySelector('h3')?.textContent):'',hit:e.contains(document.elementFromPoint(x,y)),in_transcript:!!e.closest('[data-testid=transcript]'),unobscured:e.contains(document.elementFromPoint(x,y))&&(!e.closest('[data-testid=transcript]')||(t&&r.top>=t.top&&r.bottom<=Math.min(t.bottom,composerTop))),editable:e.matches('input,textarea'),control:e.matches('input,textarea,select'),enabled:!e.disabled,visible:x>=0&&y>=0&&x<innerWidth&&y<innerHeight,x:r.x,y:r.y,width:r.width,height:r.height,value:e.type==='password'?'':e.matches('input,textarea,select')?e.value:'',password_present:e.type==='password'&&Boolean(e.value)}];})};})())";
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
            if (entry.getBoolean("visible") && label.equals(entry.getString("label")) && (found == null || entry.getBoolean("control"))) found = entry;
        }
        return found;
    }

    private void reveal(String label) throws Exception {
        JSONObject viewport = dom(); JSONArray entries = viewport.getJSONArray("entries"); JSONObject target = null;
        for (int i = 0; i < entries.length(); i++) { JSONObject entry = entries.getJSONObject(i); if (label.equals(entry.getString("label")) && (target == null || entry.getBoolean("control"))) target = entry; }
        if (target == null || target.getBoolean("visible")) return;
        int[] location = new int[4];
        instrumentation.runOnMainSync(() -> { WebView web = activity.getBridge().getWebView(); web.getLocationOnScreen(location); location[2] = web.getWidth(); location[3] = web.getHeight(); });
        boolean up = target.getDouble("y") > viewport.getDouble("height") / 2;
        float x = location[0] + location[2] * .75f, start = location[1] + location[3] * (up ? .78f : .3f), end = location[1] + location[3] * (up ? .3f : .78f);
        long at = SystemClock.uptimeMillis();
        MotionEvent down = MotionEvent.obtain(at, at, MotionEvent.ACTION_DOWN, x, start, 0); down.setSource(InputDevice.SOURCE_TOUCHSCREEN); assertTrue(ui.injectInputEvent(down, true)); down.recycle();
        for (int step = 1; step <= 8; step++) { MotionEvent move = MotionEvent.obtain(at, at + step * 30, MotionEvent.ACTION_MOVE, x, start + (end - start) * step / 8, 0); move.setSource(InputDevice.SOURCE_TOUCHSCREEN); assertTrue(ui.injectInputEvent(move, true)); move.recycle(); }
        MotionEvent finish = MotionEvent.obtain(at, at + 270, MotionEvent.ACTION_UP, x, end, 0); finish.setSource(InputDevice.SOURCE_TOUCHSCREEN); assertTrue(ui.injectInputEvent(finish, true)); finish.recycle();
        SystemClock.sleep(150);
        nativeActions.put(new JSONObject().put("action", "native_swipe_to_observed_control").put("label", label).put("up", up));
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
