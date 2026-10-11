package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.SystemClock;
import android.view.accessibility.AccessibilityNodeInfo;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.FileOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.ArrayList;
import java.util.List;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Run against disposable packages using history=api-error or history=model-response-error. */
@RunWith(AndroidJUnit4.class)
public class APIErrorTest {
    private static final String ERROR = "model API request failed: HTTP 502 Bad Gateway: unexpected end of JSON input";
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();

    @Test
    public void terminalModelFailureKeepsStatusAndCauseWhenReopened() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull("The packaged app must have a launch Activity", launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        instrumentation.startActivitySync(launch);
        try {
            click(waitFor("Back", true));
            for (String phase : new String[] { "first-open", "reopened" }) {
                click(waitFor("Open Sweep the unused exports", true));
                waitFor(ERROR, false);
                AccessibilityNodeInfo send = waitFor("Send", true);
                assertFalse("The empty draft must remain disabled after a terminal failure", send.isEnabled());
                for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
                    if (!node.isVisibleToUser()) continue;
                    assertFalse("The failed turn must not retain Stop", named(node, "Stop"));
                    String content = String.valueOf(node.getText()) + " " + node.getContentDescription();
                    for (String forbidden : new String[] { "a tool call", "NodeRunError", "node path:" }) {
                        assertFalse("The visible failure must describe the API instead of an SDK/tool wrapper", content.contains(forbidden));
                    }
                }
                saveEvidence(phase);
                click(waitFor("Back", true));
                waitFor("Open Sweep the unused exports", true);
            }
        } catch (Exception | AssertionError failure) {
            saveEvidence("failure");
            throw failure;
        }
    }

    @Test
    public void testModelResponseErrorRetriesRemainLocalizedWhenReopened() throws Exception {
        String locale = InstrumentationRegistry.getArguments().getString("modelResponseLocale", "en");
        assertTrue("The fixture locale must be en or zh", "en".equals(locale) || "zh".equals(locale));
        boolean chinese = "zh".equals(locale);
        String retry = chinese ? "模型服务返回的响应无法解析，正在重试。"
            : "The model service returned a response that could not be decoded. Retrying.";
        String error = chinese ? "模型服务返回的响应无法解析。"
            : "the model service returned a response that could not be decoded";
        String back = chinese ? "返回" : "Back";
        String open = (chinese ? "打开 " : "Open ") + "Sweep the unused exports";
        String send = chinese ? "发送" : "Send";
        String directory = "model-response-error-" + locale;
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull("The packaged app must have a launch Activity", launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        instrumentation.startActivitySync(launch);
        try {
            click(waitFor(back, true));
            for (String phase : new String[] { "first-open", "reopened" }) {
                click(waitFor(open, true));
                waitFor(error, false);
                int retries = 0;
                for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
                    if (!node.isVisibleToUser()) continue;
                    if (named(node, retry)) retries++;
                    for (String active : new String[] { "Stop", "停止", "Steer", "插入", "Follow-up", "跟进" }) {
                        assertFalse("The failed turn must return to the idle composer", named(node, active));
                    }
                    String content = String.valueOf(node.getText()) + " " + node.getContentDescription();
                    for (String forbidden : new String[] { "a tool call", "工具调用", "invalid_tool_json", "NodeRunError", "node path:" }) {
                        assertFalse("A response decoding failure must not blame tool arguments", content.contains(forbidden));
                    }
                    String otherLocale = chinese
                        ? "The model service returned a response that could not be decoded. Retrying."
                        : "模型服务返回的响应无法解析，正在重试。";
                    assertFalse("Retry notices must follow the fixture locale", content.contains(otherLocale));
                }
                assertEquals("Both bounded model-response retries must remain visible", 2, retries);
                assertFalse("The idle empty draft must remain disabled", waitFor(send, true).isEnabled());
                saveEvidence(directory, phase);
                click(waitFor(back, true));
                waitFor(open, true);
            }
        } catch (Exception | AssertionError failure) {
            saveEvidence(directory, "failure");
            throw failure;
        }
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
        saveEvidence("api-error", name);
    }

    private void saveEvidence(String directoryName, String name) throws Exception {
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), directoryName);
        assertTrue("Native evidence directory must be writable", directory.isDirectory() || directory.mkdirs());
        Bitmap screenshot = ui.takeScreenshot();
        assertNotNull("The reached native screen must be captured", screenshot);
        try (FileOutputStream stream = new FileOutputStream(new File(directory, name + ".png"))) {
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
        try (FileOutputStream stream = new FileOutputStream(new File(directory, name + ".json"))) {
            stream.write(tree.toString(2).getBytes(StandardCharsets.UTF_8));
        }
    }
}
