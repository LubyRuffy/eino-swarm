package com.lubyruffy.zwai;

import static org.junit.Assert.*;

import android.app.Activity;
import android.app.Instrumentation;
import android.app.UiAutomation;
import android.content.Intent;
import android.graphics.Bitmap;
import android.graphics.Rect;
import android.os.Bundle;
import android.os.SystemClock;
import android.system.Os;
import android.view.InputDevice;
import android.view.MotionEvent;
import android.view.accessibility.AccessibilityNodeInfo;
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

/** Actual packaged WebView, native input and read-only DOM observations over offline file RPCs. */
@RunWith(AndroidJUnit4.class)
public class FilePreviewControlsTest {
    private final Instrumentation instrumentation = InstrumentationRegistry.getInstrumentation();
    private final UiAutomation ui = instrumentation.getUiAutomation();
    private final JSONArray actions = new JSONArray();
    private BridgeActivity activity;
    private File evidenceDirectory;
    private long deadline;

    private interface PreviewCase { void run() throws Exception; }

    private void runPreviewCase(String name, String query, PreviewCase test) throws Exception {
        deadline = SystemClock.uptimeMillis() + 120000;
        evidenceDirectory = new File(instrumentation.getTargetContext().getExternalFilesDir(null),
            name + "-" + UUID.randomUUID());
        assertTrue(evidenceDirectory.mkdir());
        Bundle identity = new Bundle();
        identity.putString("evidence_directory", evidenceDirectory.getAbsolutePath());
        identity.putString("driver_identity", new JSONObject()
            .put("test_class", getClass().getName())
            .put("installed_test_apk_sha256", sha256(instrumentation.getContext().getPackageCodePath()))
            .put("installed_target_apk_sha256", sha256(instrumentation.getTargetContext().getPackageCodePath()))
            .put("scope", "offline file RPC input; native touch and packaged WebView")
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
            .loadUrl("https://localhost/?mock=1&tick=0" + query));
        Throwable primary = null;
        try {
            waitNativeBack();
            click("Back");
            waitButton("Open Sweep the unused exports");
            click("Open Sweep the unused exports");
            waitButton("Outputs"); click("Outputs");
            waitButton("outputs"); click("outputs");
            test.run();
        } catch (Exception | AssertionError failure) {
            primary = failure;
            try { capture("failure"); }
            catch (Exception | AssertionError capture) { failure.addSuppressed(capture); }
            throw failure;
        } finally {
            try { instrumentation.runOnMainSync(activity::finish); }
            catch (RuntimeException cleanup) { if (primary != null) primary.addSuppressed(cleanup); else throw cleanup; }
        }
    }

    @Test
    public void nativeFilePreviewControlsResizeTextAndImages() throws Exception {
        runPreviewCase("file-preview-controls", "", () -> {
            waitButton("inspection.md"); click("inspection.md");
            waitState(s -> s.getJSONObject("text").optDouble("font") == 14);
            JSONObject initial = dom().getJSONObject("text");
            assertTrue(initial.getString("content").contains("Inspection document"));
            assertTrue(initial.getString("content").contains("Paragraph 40:"));
            capture("text-default");

            click("Increase text size");
            waitState(s -> s.getJSONObject("text").optDouble("font") == 16);
            JSONObject enlarged = dom().getJSONObject("text");
            assertEquals(initial.getString("content"), enlarged.getString("content"));
            for (String part : new String[] {"heading_font", "code_font", "table_font"})
                assertTrue("Every rendered document part follows font size: " + part,
                    enlarged.getDouble(part) > initial.getDouble(part));
            for (int size = 18; size <= 28; size += 2) {
                final int expected = size;
                click("Increase text size");
                waitState(s -> s.getJSONObject("text").optDouble("font") == expected);
            }
            assertFalse(button(dom(), "Increase text size").getBoolean("enabled"));
            JSONObject largestText = dom().getJSONObject("text");
            for (String part : new String[] {"body", "heading", "code", "table"}) {
                double font = largestText.getDouble(part + "_font");
                double line = largestText.getDouble(part + "_line_height");
                assertTrue("Rendered " + part + " font must be observable", font > 0);
                assertTrue("Enlarged " + part + " lines must not overlap: " + line + " < " + font,
                    line >= font);
            }
            JSONObject beforeScroll = dom();
            swipe(beforeScroll.getJSONObject("text"), false);
            waitState(s -> s.getJSONObject("text").getDouble("scroll_top") > 0);
            assertTrue(button(dom(), "Reset text size").getBoolean("hit"));
            assertTrue(button(dom(), "Download").getBoolean("hit"));
            capture("text-enlarged-and-scrolled");
            click("Decrease text size");
            waitState(s -> s.getJSONObject("text").optDouble("font") == 26);
            click("Reset text size");
            waitState(s -> s.getJSONObject("text").optDouble("font") == 14);
            click("Decrease text size");
            waitState(s -> s.getJSONObject("text").optDouble("font") == 12);
            assertFalse(button(dom(), "Decrease text size").getBoolean("enabled"));
            click("Reset text size");
            waitState(s -> s.getJSONObject("text").optDouble("font") == 14);
            assertEquals(initial.getString("content"), dom().getJSONObject("text").getString("content"));
            capture("text-reset");

            click("Back to list");
            waitButton("landscape.png"); click("landscape.png");
            waitState(s -> s.getJSONObject("image").optInt("natural_width") == 1200
                && s.getJSONObject("image").optInt("natural_height") == 800
                && s.getJSONObject("image").optBoolean("decoded"));
            JSONObject fitWidth = dom();
            JSONObject fitted = fitWidth.getJSONObject("image");
            assertEquals(fitted.getDouble("client_width"), fitted.getDouble("width"), 1);
            assertTrue(button(fitWidth, "Fit to width").getBoolean("pressed"));
            assertFalse(button(fitWidth, "Fit to window").getBoolean("pressed"));
            capture("image-fit-width");
            click("Zoom in");
            waitState(s -> s.getJSONObject("image").optDouble("width") > fitted.getDouble("width") + 1);
            capture("image-zoomed");
            click("Actual size (100%)");
            waitState(s -> s.getJSONObject("image").optDouble("width") == 1200);
            JSONObject actual = dom().getJSONObject("image");
            assertEquals(800, actual.getDouble("height"), 0.1);
            assertTrue(actual.getDouble("scroll_width") > actual.getDouble("client_width"));
            assertTrue(actual.getDouble("scroll_height") > actual.getDouble("client_height"));
            assertEquals(fitted.getString("src"), actual.getString("src"));
            double beforeLeft = actual.getDouble("scroll_left");
            swipe(actual, true);
            waitState(s -> Math.abs(s.getJSONObject("image").getDouble("scroll_left") - beforeLeft) > 5);
            assertTrue(button(dom(), "Zoom out").getBoolean("hit"));
            assertTrue(button(dom(), "Download").getBoolean("hit"));
            capture("image-original-size-scrolled");
            click("Zoom out");
            waitState(s -> s.getJSONObject("image").optDouble("width") == 900);
            click("Fit to window");
            waitState(s -> button(s, "Fit to window").getBoolean("pressed"));
            JSONObject fitWindow = dom();
            JSONObject windowImage = fitWindow.getJSONObject("image");
            assertTrue(windowImage.getDouble("width") <= windowImage.getDouble("client_width") + 1);
            assertTrue(windowImage.getDouble("height") <= windowImage.getDouble("client_height") + 1);
            assertFalse(button(fitWindow, "Fit to width").getBoolean("pressed"));
            capture("image-fit-window");
            click("Fit to width");
            waitState(s -> button(s, "Fit to width").getBoolean("pressed"));
            assertFalse(button(dom(), "Fit to window").getBoolean("pressed"));
            capture("image-refitted-width");
            click("Fit to window");
            waitState(s -> button(s, "Fit to window").getBoolean("pressed"));
            click("Back to list");
            waitButton("landscape.png"); click("landscape.png");
            waitState(s -> s.getJSONObject("image").optBoolean("decoded")
                && Math.abs(s.getJSONObject("image").optDouble("width") - fitted.getDouble("width")) < 1);
            JSONObject reopened = dom();
            assertTrue(button(reopened, "Fit to width").getBoolean("pressed"));
            assertFalse(button(reopened, "Fit to window").getBoolean("pressed"));
            capture("image-reopened-width");
            write("result.json", new JSONObject().put("passed", true)
                .put("text_min_px", 12).put("text_max_px", 28).put("text_default_px", 14)
                .put("image_original_width", 1200).put("image_original_height", 800)
                .put("native_actions", actions).toString(2));
        });
    }

    @Test
    public void nativePinchResizesOnlyThePreviewImage() throws Exception {
        runPreviewCase("image-pinch", "&file-preview=tall-image", () -> {
            waitButton("tall.png"); click("tall.png");
            waitState(s -> s.getJSONObject("image").optBoolean("decoded")
                && button(s, "Fit to width").getBoolean("pressed"));
            JSONObject initial = dom(), image = initial.getJSONObject("image");
            assertEquals(512, image.getInt("natural_width"));
            assertEquals(4096, image.getInt("natural_height"));
            assertEquals(image.getDouble("client_width"), image.getDouble("width"), 1);
            assertEquals(0, image.getDouble("scroll_top"), 1);
            capture("pinch-initial-width");

            pinch("image", true, 0.09, 0.25);
            waitState(s -> s.getJSONObject("image").getDouble("width") > image.getDouble("width") * 1.5);
            JSONObject enlarged = dom();
            assertPinchKeepsPageStill(initial, enlarged);
            assertFalse(button(enlarged, "Fit to width").getBoolean("pressed"));
            assertFalse(button(enlarged, "Fit to window").getBoolean("pressed"));
            capture("pinch-open");

            JSONObject beforeSwipe = enlarged.getJSONObject("image");
            swipe(beforeSwipe, false);
            waitState(s -> s.getJSONObject("image").getDouble("scroll_top") > beforeSwipe.getDouble("scroll_top") + 5);
            assertPinchKeepsPageStill(initial, dom());
            capture("pinch-then-swipe");

            pinch("image", false, 0.09, 0.25);
            waitState(s -> s.getJSONObject("image").getDouble("width") < beforeSwipe.getDouble("width") * 0.75);
            assertPinchKeepsPageStill(initial, dom());
            capture("pinch-close");

            click("Fit to width");
            waitState(s -> button(s, "Fit to width").getBoolean("pressed")
                && Math.abs(s.getJSONObject("image").getDouble("width") - image.getDouble("width")) <= 1
                && s.getJSONObject("image").getDouble("scroll_top") <= 1);
            assertPinchKeepsPageStill(initial, dom());
            capture("pinch-refitted-width");
            write("result.json", new JSONObject().put("passed", true).put("native_actions", actions).toString(2));
        });
    }

    private void assertPinchKeepsPageStill(JSONObject initial, JSONObject current) throws Exception {
        assertVisualViewportStill(initial, current);
        for (String label : new String[] {"Back to list", "Fit to width", "Fit to window", "Download"}) {
            assertTrue("Preview control stays reachable: " + label, button(current, label).getBoolean("hit"));
        }
        for (String label : new String[] {"Back to list", "Download"}) {
            JSONObject before = button(initial, label), after = button(current, label);
            for (String metric : new String[] {"x", "y", "width", "height"}) {
                assertEquals("Preview control keeps its geometry: " + label + " " + metric,
                    before.getDouble(metric), after.getDouble(metric), 0.1);
            }
        }
        JSONObject originalImage = initial.getJSONObject("image"), image = current.getJSONObject("image");
        for (String metric : new String[] {"x", "y", "viewport_width", "viewport_height"}) {
            assertEquals("The image scrollport keeps its geometry: " + metric,
                originalImage.getDouble(metric), image.getDouble(metric), 0.1);
        }
        assertEquals(originalImage.getString("src"), image.getString("src"));
        assertEquals(8, image.getDouble("height") / image.getDouble("width"), 0.01);
    }

    private void assertVisualViewportStill(JSONObject initial, JSONObject current) throws Exception {
        assertEquals(initial.getInt("document_width"), current.getInt("document_width"));
        for (String metric : new String[] {"width", "height", "scale", "page_left", "page_top"}) {
            assertEquals("The page's visual viewport stays fixed: " + metric,
                initial.getJSONObject("visual_viewport").getDouble(metric),
                current.getJSONObject("visual_viewport").getDouble(metric), 0.1);
        }
    }

    @Test
    public void nativeMarkdownPinchChangesTextSize() throws Exception {
        runPreviewCase("markdown-font-pinch", "", () -> {
            waitButton("inspection.md"); click("inspection.md");
            waitState(s -> s.getJSONObject("text").optDouble("font") == 14);
            JSONObject initial = dom(), text = initial.getJSONObject("text");
            assertTrue(text.getString("content").contains("Inspection document"));
            assertTrue(text.getString("content").contains("Paragraph 40:"));
            capture("markdown-pinch-default");

            pinch("text", true, 0.13, 0.18);
            waitState(s -> s.getJSONObject("text").getDouble("font") > 14);
            JSONObject larger = dom(), largerText = larger.getJSONObject("text");
            assertVisualViewportStill(initial, larger);
            assertMarkdownFontChange(text, largerText, true);
            capture("markdown-pinch-larger");

            JSONObject beforeSwipe = dom().getJSONObject("text");
            swipe(beforeSwipe, false);
            waitState(s -> s.getJSONObject("text").getDouble("scroll_top") > beforeSwipe.getDouble("scroll_top") + 5);
            assertTrue(button(dom(), "Download").getBoolean("hit"));

            pinch("text", false, 0.13, 0.18);
            waitState(s -> s.getJSONObject("text").getDouble("font") < largerText.getDouble("font"));
            JSONObject smaller = dom(), smallerText = smaller.getJSONObject("text");
            assertVisualViewportStill(initial, smaller);
            assertMarkdownFontChange(largerText, smallerText, false);
            capture("markdown-pinch-smaller");

            double smallerSize = smallerText.getDouble("font");
            click("Increase text size");
            waitState(s -> s.getJSONObject("text").getDouble("font") > smallerSize);
            click("Reset text size");
            waitState(s -> s.getJSONObject("text").getDouble("font") == 14);
            assertEquals(text.getString("content"), dom().getJSONObject("text").getString("content"));
            capture("markdown-pinch-reset");
            write("result.json", new JSONObject().put("passed", true).put("native_actions", actions).toString(2));
        });
    }

    private void assertMarkdownFontChange(JSONObject from, JSONObject to, boolean larger) throws Exception {
        assertEquals(from.getString("content"), to.getString("content"));
        for (String metric : new String[] {"body_font", "heading_font", "code_font", "table_font"}) {
            double original = from.getDouble(metric), changed = to.getDouble(metric);
            assertTrue("Pinch must resize Markdown " + metric,
                original > 0 && changed > 0 && (larger ? changed > original : changed < original));
        }
    }

    @Test
    public void nativeEpubPreviewUsesFullReadingAreaAndTapNavigation() throws Exception {
        runPreviewCase("epub-preview", "&file-preview=epub", () -> {
            waitButton("reading-test.epub");
            click("reading-test.epub");
            waitState(s -> s.getJSONObject("epub").optBoolean("ready")
                && s.getJSONObject("epub").optBoolean("heading_in_view"));
            JSONObject first = dom().getJSONObject("epub");
            assertEquals("The arrival", first.getString("title"));
            assertEquals("reading-test.epub", first.getString("header"));
            assertEquals(18, first.getDouble("font"), 0.1);
            assertFalse("No second filename in the reading area", first.getBoolean("repeated_filename"));
            assertTrue("Body fills the preview", first.getDouble("viewport_height") >= first.getDouble("preview_height") * 0.9);
            assertFalse(first.getBoolean("last_passage_in_view"));
            assertEpubControls(false);
            capture("reading-first-page");

            tapEpub(0.12);
            assertTrue("The first page does not wrap", dom().getJSONObject("epub").getBoolean("heading_in_view"));
            tapEpub(0.88);
            waitState(s -> "The arrival".equals(s.getJSONObject("epub").optString("title"))
                && !s.getJSONObject("epub").optBoolean("heading_in_view"));
            assertEpubControls(false);
            capture("reading-next-page");
            tapEpub(0.12);
            waitState(s -> s.getJSONObject("epub").optBoolean("heading_in_view"));

            tapEpub(0.5);
            waitButton("Contents");
            waitButton("Download");
            assertEpubControls(true);
            JSONObject withControls = dom().getJSONObject("epub");
            for (String metric : new String[] {"x", "y", "viewport_width", "viewport_height"})
                assertEquals("Opening the controls does not resize the reading area: " + metric,
                    first.getDouble(metric), withControls.getDouble(metric), 0.1);
            assertTrue(withControls.getBoolean("heading_in_view"));
            capture("reading-with-controls");
            tapEpub(0.5);
            waitState(s -> !s.getJSONObject("epub").optBoolean("contents_visible"));
            assertEpubControls(false);
            assertTrue(dom().getJSONObject("epub").getBoolean("heading_in_view"));

            // A right tap after the last page reaches the next chapter. Going
            // back resumes the previous chapter at its final visible passage.
            for (int i = 0; i < 30 && !"The garden".equals(dom().getJSONObject("epub").optString("title")); i++) {
                tapEpub(0.88);
                waitState(s -> s.getJSONObject("epub").optBoolean("ready"));
            }
            waitState(s -> "The garden".equals(s.getJSONObject("epub").optString("title"))
                && s.getJSONObject("epub").optBoolean("heading_in_view"));
            capture("reading-next-chapter");
            tapEpub(0.12);
            waitState(s -> "The arrival".equals(s.getJSONObject("epub").optString("title"))
                && s.getJSONObject("epub").optBoolean("last_passage_in_view"));
            capture("reading-previous-chapter-last-page");
            click("Back to list");
            waitButton("reading-test.epub");
            click("reading-test.epub");
            waitState(s -> "The arrival".equals(s.getJSONObject("epub").optString("title"))
                && s.getJSONObject("epub").optBoolean("heading_in_view"));
            assertEpubControls(false);

            tapEpub(0.88);
            waitState(s -> !s.getJSONObject("epub").getBoolean("heading_in_view")
                && s.getJSONObject("epub").getJSONArray("visible_passages").length() > 0);
            JSONObject beforePinch = dom(), beforeReading = beforePinch.getJSONObject("epub");
            assertEquals(18, beforeReading.getDouble("font"), 0.1);
            pinch("epub", true, 0.13, 0.18);
            waitState(s -> s.getJSONObject("epub").getDouble("font") > 18);
            JSONObject larger = dom();
            assertEpubPinchPreservesReading(beforePinch, larger);
            capture("reading-pinch-larger");

            double enlargedFont = larger.getJSONObject("epub").getDouble("font");
            pinch("epub", false, 0.13, 0.18);
            waitState(s -> s.getJSONObject("epub").getDouble("font") < enlargedFont);
            JSONObject smaller = dom();
            assertEpubPinchPreservesReading(larger, smaller);
            capture("reading-pinch-smaller");

            tapEpub(0.5);
            waitButton("Contents");
            waitButton("Download");
            assertEpubControls(true);
            assertEquals(smaller.getJSONObject("epub").getJSONArray("visible_passages").toString(),
                dom().getJSONObject("epub").getJSONArray("visible_passages").toString());
            double smallerFont = smaller.getJSONObject("epub").getDouble("font");
            click("Increase text size");
            waitState(s -> s.getJSONObject("epub").getDouble("font") > smallerFont);
            click("Reset text size");
            waitState(s -> s.getJSONObject("epub").getDouble("font") == 18);
            tapEpub(0.5);
            waitState(s -> !s.getJSONObject("epub").getBoolean("contents_visible"));
            assertEpubControls(false);
            String beforeTurn = dom().getJSONObject("epub").getJSONArray("visible_passages").toString();
            tapEpub(0.88);
            waitState(s -> !s.getJSONObject("epub").getJSONArray("visible_passages").toString()
                .equals(beforeTurn));

            click("Back to list");
            waitButton("reading-test.epub"); click("reading-test.epub");
            waitState(s -> s.getJSONObject("epub").optBoolean("heading_in_view")
                && s.getJSONObject("epub").getDouble("font") == 18);
            assertEpubControls(false);
            write("result.json", new JSONObject().put("passed", true).put("native_actions", actions).toString(2));
        });
    }

    private void assertEpubPinchPreservesReading(JSONObject from, JSONObject to) throws Exception {
        assertVisualViewportStill(from, to);
        JSONObject before = from.getJSONObject("epub"), after = to.getJSONObject("epub");
        assertEquals(before.getString("title"), after.getString("title"));
        for (String metric : new String[] {"x", "y", "viewport_width", "viewport_height"}) {
            assertEquals("The reading area stays fixed: " + metric, before.getDouble(metric), after.getDouble(metric), 0.1);
        }
        JSONArray earlier = before.getJSONArray("visible_passages"), current = after.getJSONArray("visible_passages");
        boolean retained = false;
        for (int i = 0; i < earlier.length(); i++) {
            for (int j = 0; j < current.length(); j++) {
                if (earlier.getString(i).equals(current.getString(j))) retained = true;
            }
        }
        assertTrue("Font reflow must retain visible reading content: " + earlier + " → " + current, retained);
        assertEpubControls(false);
    }

    private void assertEpubControls(boolean visible) throws Exception {
        JSONObject state = dom();
        assertEquals("Contents visibility", visible, state.getJSONObject("epub").getBoolean("contents_visible"));
        JSONObject download = button(state, "Download");
        assertEquals("Download hittability", visible, download != null && download.getBoolean("hit"));
        for (String label : new String[] {"Increase text size", "Decrease text size", "Reset text size"}) {
            JSONObject control = button(state, label);
            assertEquals("Reader font control visibility: " + label, visible,
                control != null && control.getDouble("width") > 0 && control.getDouble("height") > 0);
        }
        for (String label : new String[] {"Previous", "Next"})
            assertNull("No on-screen page buttons", button(state, label));
    }

    private void tapEpub(double fraction) throws Exception {
        assertTrue("No input after the original case deadline", SystemClock.uptimeMillis() < deadline);
        JSONObject state = dom(), bounds = state.getJSONObject("epub");
        assertTrue("Read area must be drawn before native input", bounds.getBoolean("ready"));
        touch(bounds.getDouble("x") + bounds.getDouble("viewport_width") * fraction,
            bounds.getDouble("y") + bounds.getDouble("viewport_height") / 2, state.getJSONObject("viewport"));
        actions.put(new JSONObject().put("action", "native_touch_reading_area")
            .put("horizontal_fraction", fraction).put("before", bounds));
    }

    private interface StateCheck { boolean accepts(JSONObject state) throws Exception; }

    private void waitState(StateCheck check) throws Exception {
        long end = Math.min(deadline, SystemClock.uptimeMillis() + 10000);
        while (SystemClock.uptimeMillis() < end) {
            if (check.accepts(dom())) return;
            SystemClock.sleep(100);
        }
        fail("Packaged rendered state did not reach its expected result before the original deadline: " + dom());
    }

    private void waitButton(String label) throws Exception {
        waitState(s -> {
            JSONObject b = button(s, label);
            return b != null && b.getBoolean("enabled") && b.getBoolean("hit");
        });
    }

    private void waitNativeBack() {
        long end = Math.min(deadline, SystemClock.uptimeMillis() + 25000);
        while (SystemClock.uptimeMillis() < end) {
            for (AccessibilityNodeInfo node : nodes())
                if (node.isVisibleToUser() && named(node, "Back")) return;
            SystemClock.sleep(100);
        }
        fail("Initial packaged native Back control was not painted");
    }

    private JSONObject button(JSONObject snapshot, String label) throws Exception {
        JSONObject match = null;
        JSONArray buttons = snapshot.getJSONArray("buttons");
        for (int i = 0; i < buttons.length(); i++) {
            JSONObject candidate = buttons.getJSONObject(i);
            if (label.equals(candidate.getString("label"))) {
                assertNull("A native control needs a unique current DOM identity: " + label, match);
                match = candidate;
            }
        }
        return match;
    }

    private void click(String label) throws Exception {
        assertTrue("No input after the original case deadline", SystemClock.uptimeMillis() < deadline);
        List<AccessibilityNodeInfo> matches = new ArrayList<>();
        for (AccessibilityNodeInfo node : nodes())
            if (node.isVisibleToUser() && node.isEnabled() && node.isClickable() && named(node, label)) matches.add(node);
        assertTrue("Native target must be unique: " + label, matches.size() <= 1);
        if (matches.size() == 1) {
            assertTrue("One native AX press must acknowledge", matches.get(0).performAction(AccessibilityNodeInfo.ACTION_CLICK));
            actions.put(new JSONObject().put("action", "native_AX_click").put("label", label));
            return;
        }
        JSONObject snapshot = dom(), target = button(snapshot, label);
        assertNotNull("No visible native control: " + label, target);
        assertTrue(target.getBoolean("enabled") && target.getBoolean("hit"));
        touch(target.getDouble("x") + target.getDouble("width") / 2,
            target.getDouble("y") + target.getDouble("height") / 2, snapshot.getJSONObject("viewport"));
        actions.put(new JSONObject().put("action", "native_touch_current_DOM_bounds")
            .put("label", label).put("target", target));
    }

    private void touch(double cx, double cy, JSONObject viewport) throws Exception {
        assertTrue(cx > 0 && cy > 0 && cx < viewport.getDouble("width") && cy < viewport.getDouble("height"));
        int[] g = geometry();
        float x = (float) (g[0] + cx * g[2] / viewport.getDouble("width"));
        float y = (float) (g[1] + cy * g[3] / viewport.getDouble("height"));
        long time = SystemClock.uptimeMillis();
        for (int action : new int[] {MotionEvent.ACTION_DOWN, MotionEvent.ACTION_UP}) {
            MotionEvent event = MotionEvent.obtain(time, SystemClock.uptimeMillis(), action, x, y, 0);
            event.setSource(InputDevice.SOURCE_TOUCHSCREEN);
            assertTrue(ui.injectInputEvent(event, true)); event.recycle();
            if (action == MotionEvent.ACTION_DOWN) SystemClock.sleep(50);
        }
    }

    private void swipe(JSONObject rect, boolean horizontal) throws Exception {
        int[] g = geometry(); JSONObject viewport = dom().getJSONObject("viewport");
        long start = SystemClock.uptimeMillis();
        for (int i = 0; i <= 12; i++) {
            assertTrue(SystemClock.uptimeMillis() < deadline);
            double fraction = 0.8 - 0.6 * i / 12;
            double cx = rect.getDouble("x") + rect.getDouble("viewport_width") * (horizontal ? fraction : 0.5);
            double cy = rect.getDouble("y") + rect.getDouble("viewport_height") * (horizontal ? 0.5 : fraction);
            float x = (float) (g[0] + cx * g[2] / viewport.getDouble("width"));
            float y = (float) (g[1] + cy * g[3] / viewport.getDouble("height"));
            int action = i == 0 ? MotionEvent.ACTION_DOWN : i == 12 ? MotionEvent.ACTION_UP : MotionEvent.ACTION_MOVE;
            MotionEvent event = MotionEvent.obtain(start, SystemClock.uptimeMillis(), action, x, y, 0);
            event.setSource(InputDevice.SOURCE_TOUCHSCREEN);
            assertTrue(ui.injectInputEvent(event, true)); event.recycle(); SystemClock.sleep(25);
        }
        actions.put(new JSONObject().put("action", "native_swipe_current_scrollport_bounds")
            .put("horizontal", horizontal).put("before", rect));
    }

    private void pinch(String surface, boolean opening, double narrowFraction, double wideFraction) throws Exception {
        JSONObject snapshot = dom(), rect = snapshot.getJSONObject(surface);
        JSONObject viewport = snapshot.getJSONObject("viewport");
        int[] g = geometry();
        double xScale = g[2] / viewport.getDouble("width"), yScale = g[3] / viewport.getDouble("height");
        float cx = (float) (g[0] + (rect.getDouble("x") + rect.getDouble("viewport_width") * 0.5) * xScale);
        float cy = (float) (g[1] + (rect.getDouble("y") + rect.getDouble("viewport_height") * 0.4) * yScale);
        float narrow = (float) (rect.getDouble("viewport_width") * narrowFraction * xScale);
        float wide = (float) (rect.getDouble("viewport_width") * wideFraction * xScale);
        float from = opening ? narrow : wide, to = opening ? wide : narrow;
        long down = SystemClock.uptimeMillis();
        injectPinchEvent(down, MotionEvent.ACTION_DOWN, 1, cx, cy, from);
        SystemClock.sleep(30);
        injectPinchEvent(down, MotionEvent.ACTION_POINTER_DOWN | (1 << MotionEvent.ACTION_POINTER_INDEX_SHIFT), 2, cx, cy, from);
        for (int step = 1; step <= 10; step++) {
            SystemClock.sleep(25);
            injectPinchEvent(down, MotionEvent.ACTION_MOVE, 2, cx, cy, from + (to - from) * step / 10);
        }
        SystemClock.sleep(25);
        injectPinchEvent(down, MotionEvent.ACTION_POINTER_UP | (1 << MotionEvent.ACTION_POINTER_INDEX_SHIFT), 2, cx, cy, to);
        injectPinchEvent(down, MotionEvent.ACTION_UP, 1, cx, cy, to);
        actions.put(new JSONObject().put("action", "native_two_pointer_pinch")
            .put("surface", surface).put("opening", opening).put("initial_span_px", from * 2)
            .put("final_span_px", to * 2).put("before", rect));
    }

    private void injectPinchEvent(long down, int action, int count, float cx, float cy, float halfSpan) {
        assertTrue("No pinch input after the original case deadline", SystemClock.uptimeMillis() < deadline);
        MotionEvent.PointerProperties[] properties = new MotionEvent.PointerProperties[count];
        MotionEvent.PointerCoords[] coordinates = new MotionEvent.PointerCoords[count];
        for (int i = 0; i < count; i++) {
            MotionEvent.PointerProperties pointer = new MotionEvent.PointerProperties();
            pointer.id = i;
            pointer.toolType = MotionEvent.TOOL_TYPE_FINGER;
            properties[i] = pointer;
            MotionEvent.PointerCoords point = new MotionEvent.PointerCoords();
            point.x = cx + (i == 0 ? -halfSpan : halfSpan);
            point.y = cy;
            point.pressure = 1;
            point.size = 1;
            coordinates[i] = point;
        }
        MotionEvent event = MotionEvent.obtain(down, SystemClock.uptimeMillis(), action, count, properties,
            coordinates, 0, 0, 1, 1, 0, 0, InputDevice.SOURCE_TOUCHSCREEN, 0);
        try {
            assertTrue("Native pinch event must reach the packaged WebView", ui.injectInputEvent(event, true));
        } finally {
            event.recycle();
        }
    }

    private int[] geometry() {
        int[] g = new int[4];
        instrumentation.runOnMainSync(() -> { WebView view = activity.getBridge().getWebView();
            view.getLocationOnScreen(g); g[2] = view.getWidth(); g[3] = view.getHeight(); });
        return g;
    }

    private boolean named(AccessibilityNodeInfo node, String label) {
        return label.equals(String.valueOf(node.getText()).replaceAll("\\s+", " ").trim())
            || label.equals(String.valueOf(node.getContentDescription()).replaceAll("\\s+", " ").trim());
    }

    private List<AccessibilityNodeInfo> nodes() {
        List<AccessibilityNodeInfo> all = new ArrayList<>();
        AccessibilityNodeInfo root = ui.getRootInActiveWindow();
        if (root == null) return all;
        root.refresh(); all.add(root);
        for (int i = 0; i < all.size(); i++) {
            assertTrue("Native tree traversal shares the case deadline", SystemClock.uptimeMillis() < deadline);
            AccessibilityNodeInfo node = all.get(i);
            for (int j = 0; j < node.getChildCount(); j++) {
                AccessibilityNodeInfo child = node.getChild(j);
                if (child != null) all.add(child);
            }
        }
        return all;
    }

    private JSONObject dom() throws Exception {
        String script = "JSON.stringify((()=>{const tidy=s=>(s||'').replace(/\\s+/g,' ').trim();"
            + "const rect=e=>{const r=e?.getBoundingClientRect();return r?{x:r.x,y:r.y,viewport_width:r.width,viewport_height:r.height,scroll_width:e.scrollWidth,client_width:e.clientWidth,scroll_left:e.scrollLeft,scroll_top:e.scrollTop,scroll_height:e.scrollHeight,client_height:e.clientHeight}:{};};"
            + "const font=e=>e?parseFloat(getComputedStyle(e).fontSize):0,line=e=>e?parseFloat(getComputedStyle(e).lineHeight):0;const text=document.querySelector('[data-testid=phone-text-content]'),body=text?.querySelector('.md-body'),iv=document.querySelector('[data-testid=phone-image-viewport]'),img=iv?.querySelector('img');"
            + "const image={...rect(iv),width:img?.getBoundingClientRect().width||0,height:img?.getBoundingClientRect().height||0,natural_width:img?.naturalWidth||0,natural_height:img?.naturalHeight||0,src:img?.src||'',decoded:!!img?.complete&&img.naturalWidth>0};"
            + "const readable=e=>{let out='';const walker=document.createTreeWalker(e,NodeFilter.SHOW_TEXT);for(let node;node=walker.nextNode();)if(!node.parentElement.closest('[aria-hidden=true]'))out+=node.textContent;return tidy(out);};"
            + "const button=b=>{const r=b.getBoundingClientRect(),s=getComputedStyle(b),cx=r.x+r.width/2,cy=r.y+r.height/2;return{label:b.getAttribute('aria-label')||readable(b),enabled:!b.disabled,pressed:b.getAttribute('aria-pressed')==='true',hit:r.width>0&&r.height>0&&s.visibility==='visible'&&Number(s.opacity)>0&&cx>0&&cy>0&&cx<innerWidth&&cy<innerHeight&&b.contains(document.elementFromPoint(cx,cy)),x:r.x,y:r.y,width:r.width,height:r.height};};"
            + "const preview=document.querySelector('[data-testid=phone-file-preview]'),frame=preview?.querySelector('iframe'),chapter=frame?.contentDocument;"
            + "const visibleInChapter=e=>!!e&&[...e.getClientRects()].some(r=>r.right>0&&r.left<frame.clientWidth&&r.bottom>0&&r.top<frame.clientHeight);"
            + "const passages=[...chapter?.querySelectorAll('p')||[]].filter(p=>p.textContent.startsWith('Passage '));"
            + "const epub={...rect(frame),ready:!!chapter&&getComputedStyle(frame).visibility==='visible',title:frame?.title,header:document.querySelector('header h1')?.textContent,preview_height:preview?.clientHeight,repeated_filename:!!preview?.innerText.includes('reading-test.epub'),contents_visible:!!preview?.querySelector('select[aria-label=Contents]')?.getBoundingClientRect().height,font:font(chapter?.body),heading_in_view:visibleInChapter(chapter?.querySelector('h1')),last_passage_in_view:visibleInChapter(passages.find(p=>p.textContent.startsWith('Passage 35.'))),visible_passages:passages.filter(visibleInChapter).map(p=>p.textContent.split('.',1)[0])};"
            + "const v=visualViewport;const visual_viewport={width:v.width,height:v.height,scale:v.scale,page_left:v.pageLeft,page_top:v.pageTop};"
            + "return{url:location.href,viewport:{width:innerWidth,height:innerHeight},visual_viewport,document_width:document.documentElement.scrollWidth,text:{...rect(text),font:font(text),body_font:font(body),body_line_height:line(body),heading_font:font(text?.querySelector('h1')),heading_line_height:line(text?.querySelector('h1')),code_font:font(text?.querySelector('pre code')),code_line_height:line(text?.querySelector('pre code')),table_font:font(text?.querySelector('table')),table_line_height:line(text?.querySelector('table')),content:text?.textContent||''},image,epub,buttons:[...document.querySelectorAll('button,a,select')].map(button)};})())";
        CountDownLatch ready = new CountDownLatch(1); AtomicReference<String> value = new AtomicReference<>();
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView().evaluateJavascript(script,
            result -> { value.set(result); ready.countDown(); }));
        assertTrue("Read-only packaged DOM callback must return", ready.await(5, TimeUnit.SECONDS));
        assertNotNull(value.get());
        return new JSONObject(new JSONArray("[" + value.get() + "]").getString(0));
    }

    private String sha256(String path) throws Exception {
        MessageDigest digest = MessageDigest.getInstance("SHA-256");
        try (FileInputStream stream = new FileInputStream(path)) {
            byte[] buffer = new byte[65536];
            for (int count; (count = stream.read(buffer)) != -1;) digest.update(buffer, 0, count);
        }
        StringBuilder hex = new StringBuilder();
        for (byte value : digest.digest()) hex.append(String.format("%02x", value & 255));
        return hex.toString();
    }

    private void capture(String name) throws Exception {
        CountDownLatch rendered = new CountDownLatch(1);
        instrumentation.runOnMainSync(() -> activity.getBridge().getWebView()
            .postVisualStateCallback(SystemClock.uptimeMillis(), new WebView.VisualStateCallback() {
                @Override public void onComplete(long requestId) { rendered.countDown(); }
            }));
        assertTrue("Current packaged page must be ready to draw", rendered.await(5, TimeUnit.SECONDS));
        instrumentation.waitForIdleSync(); ui.waitForIdle(200, 2000);
        Bitmap screenshot = ui.takeScreenshot(); assertNotNull(screenshot);
        File png = new File(evidenceDirectory, name + ".png");
        assertTrue(png.createNewFile()); Os.chmod(png.getAbsolutePath(), 0600);
        try (FileOutputStream stream = new FileOutputStream(png)) {
            assertTrue(screenshot.compress(Bitmap.CompressFormat.PNG, 100, stream));
        }
        screenshot.recycle();
        JSONArray tree = new JSONArray();
        for (AccessibilityNodeInfo node : nodes()) {
            Rect bounds = new Rect(); node.getBoundsInScreen(bounds);
            tree.put(new JSONObject().put("class", String.valueOf(node.getClassName()))
                .put("text", String.valueOf(node.getText())).put("label", String.valueOf(node.getContentDescription()))
                .put("visible", node.isVisibleToUser()).put("clickable", node.isClickable())
                .put("bounds", bounds.flattenToString()));
        }
        write(name + "-accessibility.json", tree.toString(2));
        write(name + "-packaged-dom.json", dom().toString(2));
        write(name + "-native-actions.json", actions.toString(2));
    }

    private void write(String name, String text) throws Exception {
        File file = new File(evidenceDirectory, name);
        assertTrue("Never overwrite earlier native evidence", file.createNewFile());
        Os.chmod(file.getAbsolutePath(), 0600);
        try (FileOutputStream stream = new FileOutputStream(file)) {
            stream.write(text.getBytes(StandardCharsets.UTF_8));
        }
    }
}
