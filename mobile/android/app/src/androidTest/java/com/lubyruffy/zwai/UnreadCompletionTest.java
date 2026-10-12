package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.SystemClock;
import android.view.accessibility.AccessibilityNodeInfo;
import android.view.MotionEvent;
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

/** The disposable packaged app uses the offline history=unread-completion host. */
@RunWith(AndroidJUnit4.class)
public class UnreadCompletionTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();

    @Test
    public void completionPersistsUntilExplicitVisit() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull("The packaged app must have a launch Activity", launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        instrumentation.startActivitySync(launch);
        try {
            AccessibilityNodeInfo back = waitFor("Back", true);
            waitFor("Stop", true);
            saveEvidence("completion-auto-resume-live");
            click(back);
            waitFor("Completed unread", false);
            // WebView exposes the row's Open label; its badge is captured in the screenshot.
            AccessibilityNodeInfo thread = waitFor("Open Sweep the unused exports", true);
            Rect bounds = new Rect();
            thread.getBoundsInScreen(bounds);
            assertTrue("Completion attention must be visible above project folders", !bounds.isEmpty());
            saveEvidence("completion-unread-inbox");
            click(thread);
            AccessibilityNodeInfo send = waitFor("Send", true);
            assertFalse("The completed task keeps its empty composer disabled", send.isEnabled());
            assertFalse("The completed task must not appear as running", visible("Stop"));
            saveEvidence("completion-explicit-detail");
            click(waitFor("Back", true));
            long deadline = SystemClock.uptimeMillis() + 8000;
            while (visible("Completed unread") && SystemClock.uptimeMillis() < deadline) SystemClock.sleep(100);
            assertFalse("A successful explicit read clears the completion section", visible("Completed unread"));
            waitFor("Open Sweep the unused exports", true);
            saveEvidence("completion-read-inbox");
        } catch (Exception | AssertionError failure) {
            saveEvidence("failure");
            throw failure;
        }
    }

    @Test
    public void completionPagesContinueAfterReadingTheCursorAnchor() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        instrumentation.startActivitySync(launch);
        try {
            click(waitFor("Back", true));
            waitFor("Completed unread", false);
            waitFor("Open Completed task 2", true);
            assertFalse("The next completion page starts unloaded", visible("Open Completed task 5"));
            saveEvidence("completion-page-one");
            click(waitFor("More", true));
            SystemClock.sleep(2500);
            if (!visible("Open Completed task 5")) scrollInbox();
            AccessibilityNodeInfo anchor = waitFor("Open Completed task 5", true);
            saveEvidence("completion-page-two-after-poll");
            click(anchor);
            waitFor("Send", true);
            assertFalse("A completed task is idle", visible("Stop"));
            saveEvidence("completion-page-anchor-detail");
            click(waitFor("Back", true));
            waitFor("Completed unread", false);
            assertFalse("The explicitly read anchor leaves completion attention", visible("Open Completed task 5"));
            click(waitFor("More", true));
            if (!visible("Open Completed task 7")) scrollInbox();
            waitFor("Open Completed task 7", true);
            waitFor("Open Completed task 6", true);
            saveEvidence("completion-page-final-after-anchor-read");
        } catch (Exception | AssertionError failure) {
            saveEvidence("failure");
            throw failure;
        }
    }

    private void scrollInbox() {
        Rect frame = new Rect();
        ui.getRootInActiveWindow().getBoundsInScreen(frame);
        long start = SystemClock.uptimeMillis();
        float x = frame.centerX(), top = frame.top + frame.height() * 0.3f;
        float bottom = frame.top + frame.height() * 0.75f;
        for (int i = 0; i <= 5; i++) {
            int action = i == 0 ? MotionEvent.ACTION_DOWN : i == 5 ? MotionEvent.ACTION_UP : MotionEvent.ACTION_MOVE;
            MotionEvent event = MotionEvent.obtain(start, start + i * 50, action, x, bottom + (top - bottom) * i / 5, 0);
            assertTrue("The native swipe must reach the inbox", ui.injectInputEvent(event, true));
            event.recycle();
        }
        SystemClock.sleep(300);
    }

    private AccessibilityNodeInfo waitFor(String label, boolean button) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 20000;
        while (SystemClock.uptimeMillis() < deadline) {
            for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
                if (onScreen(node) && named(node, label)
                    && (!button || "android.widget.Button".equals(String.valueOf(node.getClassName())))) return node;
            }
            SystemClock.sleep(100);
        }
        throw new AssertionError("The native app did not reach accessible " + label);
    }

    private boolean visible(String label) {
        for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
            if (onScreen(node) && named(node, label)) return true;
        }
        return false;
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.equalsIgnoreCase(node.getText() == null ? "" : node.getText().toString())
            || label.equalsIgnoreCase(node.getContentDescription() == null ? "" : node.getContentDescription().toString());
    }

    private boolean onScreen(AccessibilityNodeInfo node) {
        if (!node.isVisibleToUser()) return false;
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root == null) return false;
        Rect bounds = new Rect(), frame = new Rect();
        node.getBoundsInScreen(bounds);
        root.getBoundsInScreen(frame);
        // WebView can mark clipped, inverted off-screen bounds as visible.
        return !bounds.isEmpty() && Rect.intersects(frame, bounds);
    }

    private void click(AccessibilityNodeInfo node) {
        assertTrue("The native control must be enabled", node.isEnabled());
        assertTrue("The native control must be clickable", node.isClickable());
        assertTrue("The accessibility click must reach the packaged-app handler", node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
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
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "unread-completion");
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
