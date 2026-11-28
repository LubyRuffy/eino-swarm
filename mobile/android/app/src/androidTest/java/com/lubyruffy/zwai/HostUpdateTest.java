package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.os.SystemClock;
import android.view.accessibility.AccessibilityNodeInfo;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.File;
import java.io.FileOutputStream;
import java.util.ArrayDeque;
import org.junit.Test;
import org.junit.runner.RunWith;

/** The disposable package selects /?mock=1&tick=0&host-update=1 before the app loads. */
@RunWith(AndroidJUnit4.class)
public class HostUpdateTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();

    @Test
    public void pairedLinuxHostCompletesOnlyAfterReconnect() throws Exception {
        Intent intent = instrumentation.getTargetContext().getPackageManager()
            .getLaunchIntentForPackage(instrumentation.getTargetContext().getPackageName());
        assertNotNull(intent);
        intent.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity activity = instrumentation.startActivitySync(intent);
        assertTrue(activity instanceof MainActivity);
        try {
            waitFor("Update available on Walkthrough PC · 1.2.3 → 1.2.4");
            click(waitFor("Update this host"));
            waitFor("Update Walkthrough PC?");
            waitFor("1.2.3 → 1.2.4");
            screenshot("confirmation");
            click(waitFor("Confirm update"));
            waitFor("Waiting for the host to finish restoring…");
            assertNull("The new version has not finished restoring", find("Host update complete"));
            screenshot("restoring");
            waitFor("Host update complete");
            // The web accessibility tree can lead SurfaceFlinger's next frame.
            ui.waitForIdle(300, 3000);
            assertNotNull("Completion must still be visible after the compositor catches up", find("Host update complete"));
            screenshot("completed");
        } catch (Exception | AssertionError failure) {
            try { screenshot("failure"); }
            catch (Exception evidenceFailure) { failure.addSuppressed(evidenceFailure); }
            throw failure;
        } finally {
            instrumentation.runOnMainSync(activity::finish);
        }
    }

    private AccessibilityNodeInfo waitFor(String label) {
        long deadline = SystemClock.uptimeMillis() + 18000;
        do {
            AccessibilityNodeInfo node = find(label);
            if (node != null) return node;
            SystemClock.sleep(100);
        } while (SystemClock.uptimeMillis() < deadline);
        throw new AssertionError("The packaged app did not reach " + label);
    }

    private AccessibilityNodeInfo find(String label) {
        ArrayDeque<AccessibilityNodeInfo> nodes = new ArrayDeque<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root != null) nodes.add(root);
        while (!nodes.isEmpty()) {
            AccessibilityNodeInfo node = nodes.remove();
            if (node.isVisibleToUser() && (label.equals(String.valueOf(node.getText())) ||
                label.equals(String.valueOf(node.getContentDescription())))) return node;
            for (int index = 0; index < node.getChildCount(); index++) {
                AccessibilityNodeInfo child = node.getChild(index);
                if (child != null) nodes.add(child);
            }
        }
        return null;
    }

    private void click(AccessibilityNodeInfo node) {
        assertTrue(node.isEnabled());
        assertTrue("The native accessibility click must reach the app", node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
    }

    private void screenshot(String phase) throws Exception {
        File directory = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "host-update");
        assertTrue(directory.isDirectory() || directory.mkdirs());
        Bitmap bitmap = ui.takeScreenshot();
        assertNotNull(bitmap);
        try (FileOutputStream stream = new FileOutputStream(new File(directory, phase + ".png"))) {
            assertTrue(bitmap.compress(Bitmap.CompressFormat.PNG, 100, stream));
        } finally {
            bitmap.recycle();
        }
    }
}
