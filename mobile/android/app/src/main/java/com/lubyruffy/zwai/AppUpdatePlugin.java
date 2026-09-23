package com.lubyruffy.zwai;

import android.app.Activity;
import android.content.ClipData;
import android.content.Intent;
import android.content.pm.PackageManager;
import android.content.pm.ResolveInfo;
import android.net.Uri;
import android.os.Build;
import android.provider.Settings;
import androidx.core.content.FileProvider;
import com.getcapacitor.JSObject;
import com.getcapacitor.Plugin;
import com.getcapacitor.PluginCall;
import com.getcapacitor.PluginMethod;
import com.getcapacitor.annotation.CapacitorPlugin;
import java.io.File;
import java.io.FileOutputStream;
import java.io.InputStream;
import java.net.HttpURLConnection;
import java.net.URL;
import java.util.List;
import java.util.Locale;

/**
 * Downloads one release APK and hands it to the system installer.
 * Redirects are followed by hand so a hop off GitHub never becomes an install.
 */
@CapacitorPlugin(name = "AppUpdate")
public class AppUpdatePlugin extends Plugin {

    static final long MAX_BYTES = 200L * 1024L * 1024L;
    static final int MAX_HOPS = 5;
    static final String REPO_PREFIX = "/LubyRuffy/eino-swarm/";
    static final long PROGRESS_EVERY_NS = 200_000_000L;

    /** received/total byte counts. total is negative when the server omitted a length. */
    interface ByteProgress {
        void onProgress(long received, long total);
    }

    @PluginMethod
    public void install(PluginCall call) {
        String url = allowed(call.getString("url", ""), true);
        if (url == null) {
            call.reject("bad_url");
            return;
        }
        Activity activity = getActivity();
        if (activity == null) {
            call.reject("no_activity");
            return;
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O
                && !activity.getPackageManager().canRequestPackageInstalls()) {
            Intent settings = new Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES);
            settings.setData(Uri.parse("package:" + activity.getPackageName()));
            activity.startActivity(settings);
            call.reject("install_permission");
            return;
        }
        new Thread(() -> {
            try {
                File apk = download(activity, url, (received, total) -> publishProgress(activity, received, total));
                activity.runOnUiThread(() -> {
                    if (activity.isDestroyed()) {
                        call.reject("no_activity");
                        return;
                    }
                    try {
                        openInstaller(activity, apk);
                        call.resolve();
                    } catch (RuntimeException ex) {
                        call.reject("install_failed");
                    }
                });
            } catch (Exception ex) {
                activity.runOnUiThread(() -> call.reject("download_failed"));
            }
        }).start();
    }

    static String allowed(String raw, boolean start) {
        if (raw == null || raw.isEmpty()) return null;
        URL url;
        try {
            url = new URL(raw);
        } catch (Exception ex) {
            return null;
        }
        if (!"https".equals(url.getProtocol())) return null;
        if (url.getUserInfo() != null && !url.getUserInfo().isEmpty()) return null;
        String host = url.getHost() == null ? "" : url.getHost().toLowerCase(Locale.US);
        String path = url.getPath() == null ? "" : url.getPath();
        if (host.equals("github.com")) {
            if (!path.startsWith(REPO_PREFIX)) return null;
            return url.toString();
        }
        // Only a redirect may land on GitHub's asset host. The first URL
        // has to be this repo, or a release from somewhere else installs.
        if (start) return null;
        if (host.equals("objects.githubusercontent.com")
                || host.equals("release-assets.githubusercontent.com")
                || host.equals("github-releases.githubusercontent.com")) {
            return url.toString();
        }
        return null;
    }

    private void publishProgress(Activity activity, long received, long total) {
        if (activity == null || activity.isDestroyed()) return;
        JSObject data = new JSObject();
        data.put("received", received);
        data.put("total", total > 0 ? total : 0);
        activity.runOnUiThread(() -> notifyListeners("progress", data));
    }

    static File download(Activity activity, String start, ByteProgress progress) throws Exception {
        String current = start;
        for (int hop = 0; hop <= MAX_HOPS; hop++) {
            String next = allowed(current, hop == 0);
            if (next == null) throw new IllegalArgumentException("host");
            HttpURLConnection conn = (HttpURLConnection) new URL(next).openConnection();
            conn.setInstanceFollowRedirects(false);
            conn.setConnectTimeout(15_000);
            conn.setReadTimeout(60_000);
            conn.setRequestProperty("User-Agent", "zwai-phone");
            conn.setRequestProperty("Accept", "application/octet-stream");
            int code;
            try {
                code = conn.getResponseCode();
            } catch (Exception ex) {
                conn.disconnect();
                throw ex;
            }
            if (code >= 300 && code < 400) {
                String loc = conn.getHeaderField("Location");
                conn.disconnect();
                if (loc == null || loc.isEmpty()) throw new IllegalStateException("redirect");
                current = new URL(new URL(next), loc).toString();
                continue;
            }
            if (code != HttpURLConnection.HTTP_OK) {
                conn.disconnect();
                throw new IllegalStateException("status");
            }
            long length = conn.getContentLengthLong();
            File out = new File(activity.getCacheDir(), "zwai-update.apk");
            boolean wrote = false;
            try (InputStream in = conn.getInputStream(); FileOutputStream fos = new FileOutputStream(out)) {
                byte[] buf = new byte[8192];
                byte[] magic = new byte[4];
                int have = 0;
                long total = 0;
                boolean zip = false;
                long marked = 0;
                int n;
                while ((n = in.read(buf)) >= 0) {
                    if (!zip) {
                        int take = Math.min(4 - have, n);
                        System.arraycopy(buf, 0, magic, have, take);
                        have += take;
                        if (have == 4) {
                            if (magic[0] != 'P' || magic[1] != 'K') {
                                throw new IllegalStateException("not apk");
                            }
                            zip = true;
                        }
                    }
                    total += n;
                    if (total > MAX_BYTES) throw new IllegalStateException("size");
                    fos.write(buf, 0, n);
                    long now = System.nanoTime();
                    if (progress != null && (marked == 0 || now - marked >= PROGRESS_EVERY_NS)) {
                        progress.onProgress(total, length);
                        marked = now;
                    }
                }
                if (!zip) throw new IllegalStateException("empty");
                if (progress != null) progress.onProgress(total, length);
                wrote = true;
            } finally {
                conn.disconnect();
                if (!wrote) out.delete();
            }
            return out;
        }
        throw new IllegalStateException("hops");
    }

    private void openInstaller(Activity activity, File apk) {
        Uri uri = FileProvider.getUriForFile(
                activity,
                activity.getPackageName() + ".fileprovider",
                apk);
        Intent intent = new Intent(Intent.ACTION_VIEW);
        intent.setDataAndType(uri, "application/vnd.android.package-archive");
        // Without ClipData the grant is dropped and the installer cannot read the file.
        intent.setClipData(ClipData.newRawUri("", uri));
        intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION);
        PackageManager pm = activity.getPackageManager();
        List<ResolveInfo> targets = pm.queryIntentActivities(intent, PackageManager.MATCH_DEFAULT_ONLY);
        for (ResolveInfo info : targets) {
            if (info.activityInfo == null) continue;
            activity.grantUriPermission(
                    info.activityInfo.packageName,
                    uri,
                    Intent.FLAG_GRANT_READ_URI_PERMISSION);
        }
        activity.startActivity(intent);
    }
}
