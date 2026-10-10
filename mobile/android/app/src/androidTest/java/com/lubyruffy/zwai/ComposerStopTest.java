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

/** Run against a disposable packaged app with the existing offline walkthrough enabled. */
@RunWith(AndroidJUnit4.class)
public class ComposerStopTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();

    @Test
    public void emptyLiveComposerStopsTurnThroughAccessibleBottomButton() throws Exception {
        Intent launch = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull("The packaged app must have a launch Activity", launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        instrumentation.startActivitySync(launch);
        try {
            AccessibilityNodeInfo stop = waitForButton("Stop", true);
            Rect bounds = new Rect();
            stop.getBoundsInScreen(bounds);
            int height = instrumentation.getTargetContext().getResources().getDisplayMetrics().heightPixels;
            assertTrue("Use the bottom composer Stop, not the existing header Stop", bounds.centerY() > height / 2);
            assertTrue("The empty live composer Stop must be enabled", stop.isEnabled());
            assertTrue("The composer Stop must be accessible as clickable", stop.isClickable());
            saveEvidence("before-stop");
            // A WebView virtual accessibility action reaches the same packaged-app handler as a tap.
            assertTrue("The native accessibility click must reach Stop", stop.performAction(AccessibilityNodeInfo.ACTION_CLICK));
            AccessibilityNodeInfo send = waitForButton("Send", false);
            assertFalse("Stopping returns the empty composer to disabled Send", send.isEnabled());
            assertTrue("Neither header nor composer may retain Stop after the turn ends", buttons("Stop").isEmpty());
            saveEvidence("after-stop");
        } catch (Exception | AssertionError failure) {
            saveEvidence("failure");
            throw failure;
        }
    }

    private AccessibilityNodeInfo waitForButton(String label, boolean mustBeClickable) throws Exception {
        long deadline = SystemClock.uptimeMillis() + 45000;
        while (SystemClock.uptimeMillis() < deadline) {
            List<AccessibilityNodeInfo> found = buttons(label);
            AccessibilityNodeInfo bottom = null;
            int lowest = -1;
            for (AccessibilityNodeInfo node : found) {
                Rect bounds = new Rect();
                node.getBoundsInScreen(bounds);
                int height = instrumentation.getTargetContext().getResources().getDisplayMetrics().heightPixels;
                if (bounds.centerY() > height / 2
                    && (!mustBeClickable || node.isEnabled() && node.isClickable()) && bounds.bottom > lowest) {
                    bottom = node;
                    lowest = bounds.bottom;
                }
            }
            if (bottom != null && (label.equals("Stop") || buttons("Stop").isEmpty())) return bottom;
            SystemClock.sleep(100);
        }
        throw new AssertionError("No native accessible " + label + " button reached the required turn state");
    }

    private List<AccessibilityNodeInfo> buttons(String label) {
        List<AccessibilityNodeInfo> result = new ArrayList<>();
        for (AccessibilityNodeInfo node : nodes(ui.getRootInActiveWindow())) {
            boolean named = label.contentEquals(node.getText() == null ? "" : node.getText())
                || label.contentEquals(node.getContentDescription() == null ? "" : node.getContentDescription());
            if (named && node.isVisibleToUser() && "android.widget.Button".equals(String.valueOf(node.getClassName()))) result.add(node);
        }
        return result;
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
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "composer-stop");
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
