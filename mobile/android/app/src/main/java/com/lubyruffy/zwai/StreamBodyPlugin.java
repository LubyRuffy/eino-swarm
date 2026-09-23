package com.lubyruffy.zwai;

import android.os.Handler;
import android.os.Looper;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.io.InputStream;
import java.io.OutputStream;
import java.net.HttpURLConnection;
import java.net.SocketTimeoutException;
import java.net.URL;
import java.nio.charset.StandardCharsets;
import java.util.Iterator;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * POSTs one http(s) body and emits each read. HttpURLConnection is the stack
 * CapacitorHttp already uses, except that plugin waits for the full body, so
 * a phone transcript would not move until the model stopped.
 */
@CapacitorPlugin(name = "StreamBody")
public class StreamBodyPlugin extends Plugin {

    private final Handler main = new Handler(Looper.getMainLooper());
    private final ConcurrentHashMap<String, Job> jobs = new ConcurrentHashMap<>();

    static final class Job {
        final String id;
        final PluginCall call;
        final AtomicBoolean settled = new AtomicBoolean(false);
        volatile boolean cancelled;
        volatile HttpURLConnection conn;
        volatile int status;

        Job(String id, PluginCall call) {
            this.id = id;
            this.call = call;
        }
    }

    @PluginMethod
    public void open(PluginCall call) {
        String id = call.getString("id", "");
        if (id.isEmpty() || jobs.containsKey(id)) {
            call.reject("bad-url");
            return;
        }
        URL url = allowedURL(call.getString("url", ""));
        if (url == null) {
            call.reject("bad-url");
            return;
        }
        // JS numbers arrive as doubles. getInt only accepts Integer and would
        // ignore the idle clock the page actually asked for.
        Double timeoutRaw = call.getDouble("timeoutMs", 300_000d);
        int timeoutMs = timeoutRaw == null ? 300_000 : timeoutRaw.intValue();
        if (timeoutMs < 1) timeoutMs = 1;
        String body = call.getString("body", "");
        JSObject headers = call.getObject("headers", new JSObject());
        Job job = new Job(id, call);
        jobs.put(id, job);
        int timeout = timeoutMs;
        new Thread(() -> run(job, url, headers, body, timeout), "zwai-stream").start();
    }

    @PluginMethod
    public void cancel(PluginCall call) {
        String id = call.getString("id", "");
        Job job = jobs.get(id);
        if (job != null) {
            job.cancelled = true;
            HttpURLConnection conn = job.conn;
            if (conn != null) conn.disconnect();
        }
        call.resolve();
    }

    private void run(Job job, URL url, JSObject headers, String body, int timeoutMs) {
        HttpURLConnection conn = null;
        InputStream in = null;
        try {
            conn = (HttpURLConnection) url.openConnection();
            job.conn = conn;
            // A redirect would resend the bearer token. The configured URL
            // is the endpoint; a 3xx is the error the page shows.
            conn.setInstanceFollowRedirects(false);
            conn.setConnectTimeout(timeoutMs);
            conn.setReadTimeout(timeoutMs);
            conn.setUseCaches(false);
            conn.setDoInput(true);
            conn.setRequestMethod("POST");
            byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
            conn.setDoOutput(true);
            conn.setFixedLengthStreamingMode(bytes.length);
            applyHeaders(conn, headers);
            if (conn.getRequestProperty("Accept-Encoding") == null) {
                conn.setRequestProperty("Accept-Encoding", "identity");
            }
            try (OutputStream out = conn.getOutputStream()) {
                out.write(bytes);
            }
            if (job.cancelled) {
                finish(job, result(0, true), null);
                return;
            }
            int status = conn.getResponseCode();
            job.status = status;
            emitStatus(job.id, status);
            in = status >= 400 ? conn.getErrorStream() : conn.getInputStream();
            Utf8 utf8 = new Utf8();
            if (in != null) {
                byte[] buf = new byte[8192];
                int n;
                while ((n = in.read(buf)) >= 0) {
                    if (job.cancelled) break;
                    String text = utf8.append(buf, n, false);
                    if (!text.isEmpty()) emitChunk(job.id, text);
                }
                String tail = utf8.append(new byte[0], 0, true);
                if (!tail.isEmpty()) emitChunk(job.id, tail);
            }
            finish(job, result(status, job.cancelled), null);
        } catch (Exception ex) {
            if (job.cancelled) finish(job, result(job.status, true), null);
            else if (ex instanceof SocketTimeoutException) finish(job, null, "timeout");
            else finish(job, null, "network");
        } finally {
            if (in != null) {
                try {
                    in.close();
                } catch (Exception ignored) {
                    // The socket is going away either way.
                }
            }
            if (conn != null) conn.disconnect();
        }
    }

    private static void applyHeaders(HttpURLConnection conn, JSObject headers) {
        if (headers == null) return;
        Iterator<String> keys = headers.keys();
        while (keys.hasNext()) {
            String key = keys.next();
            if (key == null || key.isEmpty() || key.indexOf('\n') >= 0 || key.indexOf('\r') >= 0) continue;
            Object raw = headers.opt(key);
            if (!(raw instanceof String)) continue;
            String value = (String) raw;
            if (value.indexOf('\n') >= 0 || value.indexOf('\r') >= 0) continue;
            conn.setRequestProperty(key, value);
        }
    }

    static URL allowedURL(String raw) {
        if (raw == null || raw.isEmpty()) return null;
        URL url;
        try {
            url = new URL(raw);
        } catch (Exception ex) {
            return null;
        }
        String protocol = url.getProtocol();
        if (!"https".equals(protocol) && !"http".equals(protocol)) return null;
        if (url.getHost() == null || url.getHost().isEmpty()) return null;
        return url;
    }

    private void emitStatus(String id, int status) {
        JSObject data = new JSObject();
        data.put("id", id);
        data.put("status", status);
        onMain(() -> notifyListeners("status", data));
    }

    private void emitChunk(String id, String text) {
        JSObject data = new JSObject();
        data.put("id", id);
        data.put("text", text);
        onMain(() -> notifyListeners("chunk", data));
    }

    private void finish(Job job, JSObject result, String error) {
        if (!job.settled.compareAndSet(false, true)) return;
        jobs.remove(job.id);
        onMain(() -> {
            if (error != null) job.call.reject(error);
            else job.call.resolve(result);
        });
    }

    private static JSObject result(int status, boolean aborted) {
        JSObject data = new JSObject();
        data.put("status", status);
        data.put("aborted", aborted);
        return data;
    }

    private void onMain(Runnable runnable) {
        if (Looper.myLooper() == Looper.getMainLooper()) runnable.run();
        else main.post(runnable);
    }

    /** Holds a split UTF-8 sequence so a chunk boundary is not a replacement char. */
    static final class Utf8 {
        private byte[] pending = new byte[0];

        String append(byte[] buf, int len, boolean end) {
            byte[] all = new byte[pending.length + len];
            System.arraycopy(pending, 0, all, 0, pending.length);
            if (len > 0) System.arraycopy(buf, 0, all, pending.length, len);
            int cut = end ? all.length : completeEnd(all);
            if (!end && cut < all.length) {
                byte[] rest = new byte[all.length - cut];
                System.arraycopy(all, cut, rest, 0, rest.length);
                pending = rest;
            } else {
                pending = new byte[0];
            }
            if (cut <= 0) return "";
            return new String(all, 0, cut, StandardCharsets.UTF_8);
        }

        static int completeEnd(byte[] data) {
            int n = data.length;
            if (n == 0) return 0;
            int i = n - 1;
            int cont = 0;
            while (i >= 0 && (data[i] & 0b1100_0000) == 0b1000_0000) {
                cont++;
                i--;
                if (cont > 3) return n;
            }
            if (i < 0) return 0;
            int lead = data[i] & 0xFF;
            int need;
            if ((lead & 0b1000_0000) == 0) need = 1;
            else if ((lead & 0b1110_0000) == 0b1100_0000) need = 2;
            else if ((lead & 0b1111_0000) == 0b1110_0000) need = 3;
            else if ((lead & 0b1111_1000) == 0b1111_0000) need = 4;
            else return n;
            if (n - i < need) return i;
            return n;
        }
    }
}
