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

/** Run against a disposable packaged app using the existing history=api-error walkthrough. */
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
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "api-error");
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
