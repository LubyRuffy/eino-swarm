package com.lubyruffy.zwai;

import android.os.Bundle;
import android.os.Handler;
import android.os.Looper;
import android.webkit.WebView;
import androidx.activity.OnBackPressedCallback;
import com.getcapacitor.BridgeActivity;

public class MainActivity extends BridgeActivity {

    private static final long BACK_RESULT_MS = 500;

    private final Handler backHandler = new Handler(Looper.getMainLooper());
    private boolean backInFlight;
    private final Runnable clearBackInFlight = () -> backInFlight = false;

    @Override
    public void onCreate(Bundle savedInstanceState) {
        // Before the bridge starts, or the page's install() never finds it.
        registerPlugin(AppUpdatePlugin.class);
        registerPlugin(StreamBodyPlugin.class);
        super.onCreate(savedInstanceState);
        // Conversation and add-PC are React state, not WebView history.
        // Activity's default back finishes the task from those screens.
        // The page hook pops one layer; false is the inbox or scan screen.
        getOnBackPressedDispatcher()
            .addCallback(
                this,
                new OnBackPressedCallback(true) {
                    @Override
                    public void handleOnBackPressed() {
                        if (backInFlight) return;
                        WebView webView = getBridge() == null ? null : getBridge().getWebView();
                        if (webView == null) {
                            leaveApp(this);
                            return;
                        }
                        // evaluateJavascript can drop the callback if the page
                        // is torn down. Without this, one lost result leaves
                        // backInFlight set and the key never works again.
                        backInFlight = true;
                        backHandler.removeCallbacks(clearBackInFlight);
                        backHandler.postDelayed(clearBackInFlight, BACK_RESULT_MS);
                        try {
                            webView.evaluateJavascript(
                                "(function(){try{var fn=window.__zwaiAndroidBack;return !!(fn&&fn())}catch(e){return false}})()",
                                value -> {
                                    backHandler.removeCallbacks(clearBackInFlight);
                                    backInFlight = false;
                                    if (!handledBack(value)) leaveApp(this);
                                }
                            );
                        } catch (RuntimeException ex) {
                            backHandler.removeCallbacks(clearBackInFlight);
                            backInFlight = false;
                            leaveApp(this);
                        }
                    }
                }
            );
    }

    @Override
    public void onDestroy() {
        backHandler.removeCallbacks(clearBackInFlight);
        super.onDestroy();
    }

    private void leaveApp(OnBackPressedCallback callback) {
        if (isFinishing()) return;
        // Disabled so a second back during teardown cannot pop a screen
        // that is already going away. Only the inbox or scan screen calls this.
        callback.setEnabled(false);
        finish();
    }

    static boolean handledBack(String value) {
        if (value == null) return false;
        String v = value.trim();
        return "true".equals(v) || "\"true\"".equals(v);
    }
}
