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

/** The disposable package enables history=streamed-agents, not a static replay. */
@RunWith(AndroidJUnit4.class)
public class AgentRosterTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private boolean launcherRecovered;
    private boolean systemUIWaited;

    @Test
    public void streamedManagerHistoryDoesNotRemainAboveWorkerRoster() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        instrumentation.startActivitySync(launch);
        try {
            awaitText("Manager update 2");
            awaitButton("Agents (3)");
            tap("Earlier");
            awaitText("Older manager answer");
            saveEvidence("manager-history");
            tap("Agents (3)");
            awaitButton("worker-3");
            saveEvidence("roster");
            assertRosterHasNoManager();
            tap("worker-1");
            awaitText("Worker evidence");
            assertFalse("Manager paragraphs cannot leak into a worker chat", textContains("Manager update"));
            saveEvidence("worker");
            tap("Back");
            awaitButton("worker-3");
            assertRosterHasNoManager();
            saveEvidence("roster-return");
            tap("Back");
            awaitText("Older manager answer");
            for (int i = 0; i < 3; i++) assertTrue("Manager history must survive navigation", textContains("Manager update " + i));
            assertFalse(textContains("Worker evidence"));
            saveEvidence("manager-return");
        } catch (Exception | AssertionError failure) {
            saveEvidence("failure");
            throw failure;
        }
    }

    private void assertRosterHasNoManager() {
        for (int i = 1; i <= 3; i++) assertNotNull("All three workers must remain reachable", button("worker-" + i));
        assertFalse("Streamed manager paragraphs must unmount before the roster", textContains("Manager update"));
        assertFalse("Older manager history must not appear in the roster", textContains("Older manager answer"));
        assertFalse("Manager work folds must not appear in the roster", textContains("Thought") || textContains("read_file"));
        for (AccessibilityNodeInfo node : nodes()) if (node.isVisibleToUser())
            assertFalse("Closed Work folds must not appear in the roster", labelOf(node).matches("(?s).*\\b\\d+ tools?\\b.*"));
    }

    private void tap(String label) throws Exception {
        AccessibilityNodeInfo node = awaitButton(label);
        assertTrue("The native button must be enabled: " + label, node.isEnabled());
        assertTrue("The native button must be clickable: " + label, node.isClickable());
        assertTrue("Native accessibility click failed: " + label, node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
    }

    private AccessibilityNodeInfo awaitButton(String label) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 30000;
        while (SystemClock.uptimeMillis() < deadline) {
            recoverSystemDialog();
            AccessibilityNodeInfo node = button(label);
            if (node != null) return node;
            SystemClock.sleep(100);
        }
        throw new AssertionError("Native button did not appear: " + label);
    }

    private void awaitText(String text) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 30000;
        while (SystemClock.uptimeMillis() < deadline) {
            recoverSystemDialog();
            if (textContains(text)) return;
            SystemClock.sleep(100);
        }
        throw new AssertionError("Native text did not appear: " + text);
    }

    private void recoverSystemDialog() throws Exception {
        boolean launcher = !launcherRecovered && textEquals("Pixel Launcher isn't responding");
        boolean systemUI = !systemUIWaited && textEquals("System UI isn't responding");
        if (!launcher && !systemUI) return;
        // Recover only the isolated guest's system dialogs, never the tested app's ANR.
        String name = launcher ? "launcher-anr" : "system-ui-anr";
        saveEvidence(name + "-before");
        AccessibilityNodeInfo action = button(launcher ? "Close app" : "Wait");
        assertNotNull("The exact system dialog must expose its recovery action", action);
        assertTrue(action.performAction(AccessibilityNodeInfo.ACTION_CLICK));
        if (launcher) launcherRecovered = true;
        else systemUIWaited = true;
        SystemClock.sleep(200);
        saveEvidence(name + "-after");
    }

    private boolean textEquals(String text) {
        for (AccessibilityNodeInfo node : nodes())
            if (node.isVisibleToUser() && text.equals(String.valueOf(node.getText()))) return true;
        return false;
    }

    private AccessibilityNodeInfo button(String label) {
        for (AccessibilityNodeInfo node : nodes()) {
            if (node.isVisibleToUser() && node.isEnabled() && node.isClickable()
                && "android.widget.Button".equals(String.valueOf(node.getClassName()))
                && labelOf(node).contains(label)) return node;
        }
        return null;
    }

    private boolean textContains(String text) {
        for (AccessibilityNodeInfo node : nodes()) if (node.isVisibleToUser() && labelOf(node).contains(text)) return true;
        return false;
    }

    private String labelOf(AccessibilityNodeInfo node) {
        return String.valueOf(node.getText()) + " " + String.valueOf(node.getContentDescription());
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

    private void saveEvidence(String name) throws Exception {
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "agent-roster");
        assertTrue(directory.isDirectory() || directory.mkdirs());
        Bitmap screenshot = ui.takeScreenshot();
        assertNotNull(screenshot);
        try (FileOutputStream stream = new FileOutputStream(new File(directory, name + ".png"))) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, stream));
        }
        screenshot.recycle();
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect();
            node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName())).put("label", labelOf(node))
                .put("visible", node.isVisibleToUser()).put("clickable", node.isClickable())
                .put("enabled", node.isEnabled()).put("bounds", bounds.flattenToString()));
        }
        try (FileOutputStream stream = new FileOutputStream(new File(directory, name + ".json"))) {
            stream.write(tree.toString(2).getBytes(StandardCharsets.UTF_8));
        }
    }
}
