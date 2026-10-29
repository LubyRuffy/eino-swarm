package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.accessibilityservice.AccessibilityService;
import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.Bundle;
import android.os.ParcelFileDescriptor;
import android.os.SystemClock;
import android.system.Os;
import android.system.OsConstants;
import android.view.accessibility.AccessibilityNodeInfo;
import android.webkit.WebView;
import androidx.test.ext.junit.runners.AndroidJUnit4;
import androidx.test.platform.app.InstrumentationRegistry;
import java.io.ByteArrayOutputStream;
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
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import org.junit.runner.RunWith;

/** Real packaged-app export and system SAF UI; only the source RPC uses the existing offline input. */
@RunWith(AndroidJUnit4.class)
public class FileSaveUiTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private final String target = instrumentation.getTargetContext().getPackageName();
    private final String basename = "zwai-export-" + UUID.randomUUID() + ".md";
    private final JSONArray actions = new JSONArray();
    private final JSONArray savedStatusObservations = new JSONArray();
    private File evidence;
    private long deadline;
    private MainActivity activity;

    @Test
    public void outputDownloadCanCancelThenSaveExactBytesToDownloads() throws Exception {
        deadline = SystemClock.uptimeMillis() + 90000;
        evidence = new File(instrumentation.getTargetContext().getExternalFilesDir(null), "file-save-" + UUID.randomUUID());
        assertTrue(evidence.mkdir());
        Bundle announced = new Bundle();
        announced.putString("file_save_evidence_directory", evidence.getAbsolutePath());
        instrumentation.sendStatus(2, announced);
        Intent launch = instrumentation.getTargetContext().getPackageManager().getLaunchIntentForPackage(target);
        assertNotNull(launch);
        launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK | Intent.FLAG_ACTIVITY_CLEAR_TASK);
        Activity started = instrumentation.startActivitySync(launch);
        assertTrue(started instanceof MainActivity);
        activity = (MainActivity) started;
        Throwable primary = null;
        try {
            // Existing public fixture URL only; never replace plugin results or mutate UI state with JS.
            instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().loadUrl("https://localhost/?mock=1&tick=0"));
            press(waitNode("Back", target, "android.widget.Button", false));
            press(waitNode("Open Sweep the unused exports", target, "android.widget.Button", false));
            press(waitNode("Outputs", target, "android.widget.Button", false));
            press(waitNode("outputs", target, "android.widget.Button", false));
            press(waitNode("report.md", target, "android.widget.Button", false));
            waitNode("Verified output", target, null, false);
            capture("preview-before-download");
            press(waitNode("Download", target, "android.widget.Button", false));
            waitFilename();
            capture("system-picker-before-cancel");
            assertTrue("Cancel the actual system picker once", ui.performGlobalAction(AccessibilityService.GLOBAL_ACTION_BACK));
            waitNode("Download", target, "android.widget.Button", false);
            assertFalse("Cancellation is not a saved acknowledgement", hasText("File saved."));
            assertFalse("Cancellation is not a failed write", hasText("The file could not be saved. Try again."));
            capture("preview-after-cancel");

            press(waitNode("Download", target, "android.widget.Button", false));
            waitFilename();
            press(waitDrawer());
            press(clickableParent(waitUnique(node -> picker(node) && named(node, "Downloads") && inRootsList(node))));
            AccessibilityNodeInfo filename = waitFilename();
            Bundle replacement = new Bundle();
            replacement.putCharSequence(AccessibilityNodeInfo.ACTION_ARGUMENT_SET_TEXT_CHARSEQUENCE, basename);
            assertTrue("Set a unique owned output name through the real native field", filename.performAction(AccessibilityNodeInfo.ACTION_SET_TEXT, replacement));
            assertEquals(basename, String.valueOf(waitFilename().getText()));
            actions.put(new JSONObject().put("action", "native_filename_set").put("value", basename));
            capture("system-picker-owned-downloads");
            press(waitSave());
            // The dynamic live-region text can be painted but absent from Android's AX tree.
            // Return to the real enabled app control, then prove the file before its status copy.
            waitNode("Download", target, "android.widget.Button", false);
            byte[] expected = "# Verified output\n\n> A finding\n\n- First result\n".getBytes(StandardCharsets.UTF_8);
            byte[] actual = readSaved(expected.length + 1);
            assertArrayEquals("The system-selected document must retain the original RPC bytes", expected, actual);
            waitSavedStatus(activity);
            capture("preview-saved-and-read-back");
            writeJson(new File(evidence, "result.json"), new JSONObject().put("saved", true).put("cancelled_first", true)
                .put("destination", "/sdcard/Download/" + basename).put("bytes", actual.length)
                .put("expected_sha256", sha(expected)).put("actual_sha256", sha(actual))
                .put("source_scope", "existing offline RPC fixture, real native export/provider UI"));
            // Keep this uniquely named output for independent readback and the owning runner's bounded cleanup.
        } catch (Exception | AssertionError failure) {
            primary = failure;
            try { capture("failure"); } catch (Exception | AssertionError secondary) { failure.addSuppressed(secondary); }
            throw failure;
        } finally {
            try { instrumentation.runOnMainSync(activity::finish); }
            catch (RuntimeException cleanup) { if (primary != null) primary.addSuppressed(cleanup); else throw cleanup; }
        }
    }

    private AccessibilityNodeInfo waitFilename() {
        return waitUnique(node -> picker(node) && "android.widget.EditText".equals(String.valueOf(node.getClassName())) && node.isEditable());
    }

    private AccessibilityNodeInfo waitDrawer() {
        return waitUnique(node -> picker(node) && (named(node, "Show roots") || named(node, "Show navigation drawer") || named(node, "Open navigation drawer")) && node.isClickable());
    }

    private AccessibilityNodeInfo waitSave() {
        return waitUnique(node -> picker(node) && "android.widget.Button".equals(String.valueOf(node.getClassName())) && (named(node, "Save") || named(node, "SAVE")));
    }

    private boolean picker(AccessibilityNodeInfo node) {
        String pkg = String.valueOf(node.getPackageName());
        return "com.android.documentsui".equals(pkg) || "com.google.android.documentsui".equals(pkg);
    }

    private boolean inRootsList(AccessibilityNodeInfo node) {
        boolean rootsList = false;
        for (AccessibilityNodeInfo parent = node.getParent(); parent != null; parent = parent.getParent()) {
            if (!picker(parent)) return false;
            String role = String.valueOf(parent.getClassName());
            if ("android.widget.ListView".equals(role)) rootsList = true;
            if ("androidx.drawerlayout.widget.DrawerLayout".equals(role)) return rootsList;
        }
        return false; // The toolbar and breadcrumb can expose the same visible label.
    }

    private AccessibilityNodeInfo waitNode(String label, String pkg, String role, boolean inPicker) {
        return waitUnique(node -> named(node, label) && (pkg == null || pkg.equals(String.valueOf(node.getPackageName())))
            && (role == null || role.equals(String.valueOf(node.getClassName()))) && (!inPicker || picker(node)));
    }

    private interface Match { boolean accepts(AccessibilityNodeInfo node); }

    private AccessibilityNodeInfo waitUnique(Match match) {
        long end = Math.min(deadline, SystemClock.uptimeMillis() + 10000);
        while (SystemClock.uptimeMillis() < end) {
            List<AccessibilityNodeInfo> found = new ArrayList<>();
            for (AccessibilityNodeInfo node : nodes()) {
                if (node.isVisibleToUser() && node.isEnabled() && match.accepts(node) && !found.contains(node)) found.add(node);
            }
            assertTrue("Native target must be unique, never choose an arbitrary first match", found.size() <= 1);
            if (found.size() == 1) return found.get(0);
            SystemClock.sleep(100);
        }
        throw new AssertionError("Expected unique visible native control was not reached within this case's deadline");
    }

    private AccessibilityNodeInfo clickableParent(AccessibilityNodeInfo node) {
        while (node != null && !node.isClickable()) node = node.getParent();
        assertNotNull("The selected Downloads row needs a real clickable ancestor", node);
        assertTrue(picker(node));
        return node;
    }

    private void press(AccessibilityNodeInfo node) throws Exception {
        assertTrue("No input after the original deadline", SystemClock.uptimeMillis() < deadline);
        assertTrue(node.isVisibleToUser() && node.isEnabled() && node.isClickable());
        actions.put(new JSONObject().put("action", "native_press").put("text", String.valueOf(node.getText()))
            .put("label", String.valueOf(node.getContentDescription())).put("package", String.valueOf(node.getPackageName())));
        assertTrue("A single actual native Press must acknowledge", node.performAction(AccessibilityNodeInfo.ACTION_CLICK));
    }

    private boolean hasText(String text) {
        for (AccessibilityNodeInfo node : nodes()) if (node.isVisibleToUser() && target.equals(String.valueOf(node.getPackageName())) && named(node, text)) return true;
        return false;
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.contentEquals(node.getText() == null ? "" : node.getText())
            || label.contentEquals(node.getContentDescription() == null ? "" : node.getContentDescription());
    }

    private List<AccessibilityNodeInfo> nodes() {
        List<AccessibilityNodeInfo> result = new ArrayList<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root != null) result.add(root);
        for (int i = 0; i < result.size(); i++) {
            assertTrue("AX traversal shares the original deadline", SystemClock.uptimeMillis() < deadline);
            AccessibilityNodeInfo node = result.get(i);
            for (int j = 0; j < node.getChildCount(); j++) {
                AccessibilityNodeInfo child = node.getChild(j);
                if (child != null && !result.contains(child)) result.add(child);
            }
        }
        return result;
    }

    private byte[] readSaved(int maximum) throws Exception {
        // Only this generated ASCII UUID basename is accepted in the read-only shell command.
        assertTrue(basename.matches("zwai-export-[0-9a-f-]{36}\\.md"));
        try (ParcelFileDescriptor descriptor = ui.executeShellCommand("cat /sdcard/Download/" + basename);
             InputStream input = new ParcelFileDescriptor.AutoCloseInputStream(descriptor);
             ByteArrayOutputStream bytes = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[256]; int length;
            while ((length = input.read(buffer)) != -1) {
                bytes.write(buffer, 0, length);
                assertTrue("A wrong or oversized output cannot be accepted", bytes.size() <= maximum);
            }
            return bytes.toByteArray();
        }
    }

    private void waitSavedStatus(MainActivity activity) throws Exception {
        long end = Math.min(deadline, SystemClock.uptimeMillis() + 10000);
        while (SystemClock.uptimeMillis() < end) {
            CountDownLatch received = new CountDownLatch(1);
            String[] value = new String[1];
            instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(
                "(()=>{const p=document.querySelector('[data-testid=phone-file-preview]');"
                    + "const a=p?[...p.querySelectorAll('[role=status]')]:[];"
                    + "return a.length===1&&a[0].textContent==='File saved.'&&a[0].getBoundingClientRect().width>0"
                    + "&&a[0].getBoundingClientRect().height>0&&getComputedStyle(a[0]).visibility==='visible';})()",
                result -> { value[0] = result; received.countDown(); }));
            assertTrue("Read-only status callback must arrive inside the original budget",
                received.await(Math.max(1, Math.min(1000, end - SystemClock.uptimeMillis())), TimeUnit.MILLISECONDS));
            savedStatusObservations.put(new JSONObject().put("readonly_dom_status_predicate", String.valueOf(value[0])));
            if ("true".equals(value[0])) return;
            SystemClock.sleep(100);
        }
        fail("The exact visible preview status was not present after actual saved-byte readback");
    }

    private void capture(String phase) throws Exception {
        JSONObject sync = new JSONObject().put("app_phase", phase.startsWith("preview-"));
        if (phase.startsWith("preview-")) {
            CountDownLatch presented = new CountDownLatch(1);
            instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().postVisualStateCallback(
                0, new WebView.VisualStateCallback() {
                    @Override public void onComplete(long requestId) { presented.countDown(); }
                }));
            assertTrue("Observe the normal committed WebView frame within this case's original budget",
                presented.await(Math.max(1, Math.min(2000, deadline - SystemClock.uptimeMillis())), TimeUnit.MILLISECONDS));
            sync.put("post_visual_state_callback", true);
        }
        ui.waitForIdle(300, 1500);
        AccessibilityNodeInfo current = ui.getRootInActiveWindow();
        assertNotNull(current);
        sync.put("current_window_package", String.valueOf(current.getPackageName()));
        if (phase.startsWith("preview-")) assertEquals("Capture the actual app after the picker transition", target, String.valueOf(current.getPackageName()));
        Bitmap screenshot = ui.takeScreenshot();
        assertNotNull(screenshot);
        try (FileOutputStream output = privateOutput(new File(evidence, phase + ".png"))) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, output));
        } finally { screenshot.recycle(); }
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            JSONArray ancestors = new JSONArray();
            for (AccessibilityNodeInfo parent = node.getParent(); parent != null; parent = parent.getParent()) {
                ancestors.put(String.valueOf(parent.getClassName()));
            }
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName())).put("text", String.valueOf(node.getText()))
                .put("label", String.valueOf(node.getContentDescription())).put("package", String.valueOf(node.getPackageName()))
                .put("visible", node.isVisibleToUser()).put("bounds", bounds.flattenToString()).put("ancestor_classes", ancestors));
        }
        byte[] apk;
        try (FileInputStream input = new FileInputStream(instrumentation.getTargetContext().getPackageCodePath());
             ByteArrayOutputStream bytes = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[16384]; int length;
            while ((length = input.read(buffer)) != -1) bytes.write(buffer, 0, length);
            apk = bytes.toByteArray();
        }
        writeJson(new File(evidence, phase + ".json"), new JSONObject().put("phase", phase).put("package", target)
            .put("installed_apk_sha256", sha(apk)).put("actions", actions).put("nodes", tree)
            .put("saved_status_readonly_dom", savedStatusObservations).put("passive_capture_sync", sync));
    }

    private void writeJson(File file, JSONObject value) throws Exception {
        try (FileOutputStream output = privateOutput(file)) { output.write(value.toString(2).getBytes(StandardCharsets.UTF_8)); }
    }

    private FileOutputStream privateOutput(File file) throws Exception {
        return new FileOutputStream(Os.open(file.getAbsolutePath(), OsConstants.O_WRONLY | OsConstants.O_CREAT | OsConstants.O_EXCL, 0600));
    }

    private String sha(byte[] bytes) throws Exception {
        byte[] digest = MessageDigest.getInstance("SHA-256").digest(bytes);
        StringBuilder result = new StringBuilder();
        for (byte value : digest) result.append(String.format("%02x", value & 0xff));
        return result.toString();
    }
}
