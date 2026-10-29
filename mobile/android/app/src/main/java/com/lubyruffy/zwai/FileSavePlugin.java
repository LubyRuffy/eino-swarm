package com.lubyruffy.zwai;

import android.app.Activity;
import android.content.ContentResolver;
import android.content.Intent;
import android.net.Uri;
import android.util.Base64;
import androidx.activity.result.ActivityResult;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.ActivityCallback;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.io.IOException;
import java.io.OutputStream;
import java.util.Locale;

/** Export already-received bytes to a document explicitly chosen by the user. */
@CapacitorPlugin(name = "FileSave")
public class FileSavePlugin extends Plugin {
    static final int MAX_BYTES = 32 * 1024 * 1024;
    private final Object saveLock = new Object();
    private Pending pending;
    private boolean destroyed;

    private static final class Pending {
        final PluginCall call;
        final String callbackId;
        byte[] bytes;
        OutputStream output;
        boolean pickerLaunched;
        boolean callbackTaken;
        boolean finished;

        Pending(PluginCall call) {
            this.call = call;
            this.callbackId = call.getCallbackId();
        }
    }

    @PluginMethod
    public void saveFile(PluginCall call) {
        final Pending request;
        synchronized (saveLock) {
            if (destroyed) {
                call.reject("file_save_unavailable", "file_save_unavailable");
                return;
            }
            if (pending != null) {
                call.reject("file_save_busy", "file_save_busy");
                return;
            }
            request = new Pending(call);
            pending = request;
        }

        final String name;
        final String mime;
        try {
            Object rawName = call.getData().opt("name");
            Object rawMime = call.getData().opt("mime");
            Object rawData = call.getData().opt("data");
            if (!(rawName instanceof String) || !(rawMime instanceof String) || !(rawData instanceof String)) {
                throw new IllegalArgumentException();
            }
            name = checkedName((String) rawName);
            mime = checkedMime((String) rawMime);
            int size = checkedSize(call.getData().opt("size"));
            byte[] bytes = decodeBytes((String) rawData, size);
            synchronized (saveLock) {
                if (!current(request)) return;
                request.bytes = bytes;
            }
        } catch (IllegalArgumentException error) {
            finish(request, false, "file_save_invalid");
            return;
        } finally {
            // Capacitor retains activity-call options and may save them on rotation.
            // The bytes belong only to this pending operation, never instance state.
            call.getData().remove("data");
        }

        // Capacitor invokes plugin methods on its worker; only the picker is UI work.
        getBridge().executeOnMainThread(() -> {
            synchronized (saveLock) {
                if (!current(request)) return;
                request.pickerLaunched = true;
            }
            if (getActivity().isFinishing() || getActivity().isDestroyed()) {
                finish(request, false, "file_save_unavailable");
                return;
            }
            Intent intent = new Intent(Intent.ACTION_CREATE_DOCUMENT);
            intent.addCategory(Intent.CATEGORY_OPENABLE);
            intent.setType(mime);
            intent.putExtra(Intent.EXTRA_TITLE, name);
            try {
                startActivityForResult(request.call, intent, "documentChosen");
            } catch (RuntimeException error) {
                finish(request, false, "file_save_unavailable");
            }
        });
    }

    @ActivityCallback
    private void documentChosen(PluginCall call, ActivityResult result) {
        final Pending request;
        final boolean accepted;
        synchronized (saveLock) {
            request = pending;
            // A restored, missing or stale callback must never consume a newer save.
            accepted = request != null && current(request) && call == request.call &&
                request.callbackId.equals(call.getCallbackId()) && request.pickerLaunched && !request.callbackTaken;
            if (accepted) request.callbackTaken = true;
        }
        if (!accepted) {
            if (call != null && (request == null || call != request.call)) call.release(getBridge());
            return;
        }
        if (result.getResultCode() == Activity.RESULT_CANCELED) {
            finish(request, false, null);
            return;
        }
        Uri uri = result.getData() == null ? null : result.getData().getData();
        if (result.getResultCode() != Activity.RESULT_OK || uri == null || !"content".equals(uri.getScheme())) {
            finish(request, false, "file_save_failed");
            return;
        }
        ContentResolver resolver = getContext().getContentResolver();
        execute(() -> writeDocument(request, resolver, uri));
    }

    private void writeDocument(Pending request, ContentResolver resolver, Uri uri) {
        byte[] bytes;
        synchronized (saveLock) {
            if (!current(request)) return;
            bytes = request.bytes;
        }
        try {
            OutputStream output = resolver.openOutputStream(uri, "w");
            if (output == null) throw new IOException();
            boolean active;
            synchronized (saveLock) {
                active = current(request);
                if (active) request.output = output;
            }
            if (!active) {
                output.close();
                return;
            }
            try {
                writeAndClose(output, bytes);
            } finally {
                synchronized (saveLock) { request.output = null; }
            }
            finish(request, true, null);
        } catch (IOException | RuntimeException error) {
            // A provider URI is user-owned: never delete it to conceal a partial write.
            finish(request, false, "file_save_failed");
        }
    }

    private boolean current(Pending request) {
        return !destroyed && pending == request && !request.finished;
    }

    private void finish(Pending request, boolean saved, String error) {
        synchronized (saveLock) {
            if (request.finished) return;
            request.finished = true;
            request.bytes = null;
            if (pending == request) pending = null;
        }
        try {
            if (error != null) request.call.reject(error, error);
            else request.call.resolve(new JSObject().put("saved", saved));
        } finally {
            request.call.release(getBridge());
        }
    }

    @Override
    protected void handleOnDestroy() {
        Pending request;
        OutputStream output;
        synchronized (saveLock) {
            destroyed = true;
            request = pending;
            output = request == null ? null : request.output;
        }
        try {
            if (request != null) finish(request, false, "file_save_unavailable");
        } finally {
            if (output != null) {
                try { output.close(); } catch (IOException | RuntimeException ignored) { /* No saved acknowledgement. */ }
            }
            super.handleOnDestroy();
        }
    }

    static String checkedName(String name) {
        if (name.isEmpty() || name.equals(".") || name.equals("..")) throw new IllegalArgumentException();
        for (int i = 0; i < name.length(); i++) {
            char c = name.charAt(i);
            if (c == '/' || c == '\\' || Character.isISOControl(c)) throw new IllegalArgumentException();
        }
        return name; // Whitespace and Unicode belong to the selected filename.
    }

    static String checkedMime(String mime) {
        String type = mime.split(";", 2)[0].trim().toLowerCase(Locale.ROOT);
        if (!type.matches("[a-z0-9!#$&^_.+-]+/[a-z0-9!#$&^_.+-]+")) throw new IllegalArgumentException();
        return type;
    }

    static int checkedSize(Object size) {
        if (!(size instanceof Number)) throw new IllegalArgumentException();
        double value = ((Number) size).doubleValue();
        if (Double.isNaN(value) || Double.isInfinite(value) || value < 0 || value > MAX_BYTES || value != (int) value) {
            throw new IllegalArgumentException();
        }
        return (int) value;
    }

    static void checkBase64(String data, int size) {
        int length = data.length();
        if (size < 0 || size > MAX_BYTES || length != 4 * ((size + 2) / 3)) throw new IllegalArgumentException();
        int padding = size % 3 == 0 ? 0 : 3 - size % 3;
        for (int i = 0; i < length; i++) {
            if (i >= length - padding) {
                if (data.charAt(i) != '=') throw new IllegalArgumentException();
            } else if (base64Digit(data.charAt(i)) < 0) throw new IllegalArgumentException();
        }
        if (padding > 0 && (base64Digit(data.charAt(length - padding - 1)) & (padding == 2 ? 15 : 3)) != 0) {
            throw new IllegalArgumentException();
        }
    }

    static byte[] decodeBytes(String data, int size) {
        checkBase64(data, size);
        byte[] bytes = Base64.decode(data, Base64.NO_WRAP);
        if (bytes.length != size) throw new IllegalArgumentException();
        return bytes;
    }

    private static int base64Digit(char c) {
        if (c >= 'A' && c <= 'Z') return c - 'A';
        if (c >= 'a' && c <= 'z') return c - 'a' + 26;
        if (c >= '0' && c <= '9') return c - '0' + 52;
        if (c == '+') return 62;
        return c == '/' ? 63 : -1;
    }

    static void writeAndClose(OutputStream output, byte[] bytes) throws IOException {
        try (OutputStream target = output) {
            target.write(bytes);
            target.flush();
        }
    }
}
