import Capacitor
import UIKit

/// The storyboard loads this instead of CAPBridgeViewController so the
/// streaming call exists before the page's first fetch. capacitorDidLoad
/// runs after the bridge exists and before the webview navigates.
@objc(BridgeViewController)
public class BridgeViewController: CAPBridgeViewController, UIGestureRecognizerDelegate {
    private let fileSavePlugin = FileSavePlugin()
    private var bootstrapFailed = false

    override public func capacitorDidLoad() {
        super.capacitorDidLoad()
        bridge?.registerPluginInstance(StreamBodyPlugin())
        bridge?.registerPluginInstance(fileSavePlugin)

        let back = UIScreenEdgePanGestureRecognizer(target: self, action: #selector(edgeBack(_:)))
        back.edges = .left
        back.cancelsTouchesInView = false
        back.delegate = self
        webView?.addGestureRecognizer(back)
        webView?.scrollView.panGestureRecognizer.require(toFail: back)

        do {
            guard let bridge = bridge, let controller = webView?.configuration.userContentController else {
                throw IOSBootstrap.Failure.missingBridge
            }
            try IOSBootstrap.install(on: controller,
                cookiesEnabled: bridge.config.getPluginConfig("CapacitorCookies").getBoolean("enabled", false),
                httpEnabled: bridge.config.getPluginConfig("CapacitorHttp").getBoolean("enabled", false))
        } catch {
            bootstrapFailed = true
            // The original script is still registered. Drop it so a later load
            // cannot repeat the unanswered startup prompt.
            webView?.configuration.userContentController.removeAllUserScripts()
            NSLog("Unable to prepare zwai's native bootstrap: %@", String(describing: error))
        }
    }

    override public func viewDidLoad() {
        // capacitorDidLoad already ran from loadView, before this method.
        // super.viewDidLoad is what navigates. A rejected bootstrap must stay
        // on the native error and must not run the original startup script.
        guard !bootstrapFailed else {
            showBootstrapFailure()
            return
        }
        super.viewDidLoad()
    }

    private func showBootstrapFailure() {
        view.backgroundColor = .systemBackground
        let message = UILabel()
        message.text = NSLocalizedString("Unable to start zwai. Please update the app or contact support.", comment: "Native bootstrap failure")
        message.numberOfLines = 0
        message.textAlignment = .center
        message.font = .preferredFont(forTextStyle: .body)
        message.adjustsFontForContentSizeCategory = true
        message.translatesAutoresizingMaskIntoConstraints = false
        view.addSubview(message)
        NSLayoutConstraint.activate([
            message.leadingAnchor.constraint(equalTo: view.layoutMarginsGuide.leadingAnchor),
            message.trailingAnchor.constraint(equalTo: view.layoutMarginsGuide.trailingAnchor),
            message.centerYAnchor.constraint(equalTo: view.centerYAnchor)
        ])
    }

    deinit {
        fileSavePlugin.cancelForDestroy()
    }

    public func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        guard let back = gestureRecognizer as? UIScreenEdgePanGestureRecognizer else { return true }
        let velocity = back.velocity(in: view)
        return velocity.x > abs(velocity.y)
    }

    @objc private func edgeBack(_ back: UIScreenEdgePanGestureRecognizer) {
        guard back.state == .ended else { return }
        let movement = back.translation(in: view)
        guard movement.x >= max(60, view.bounds.width * 0.2), movement.x > abs(movement.y) else { return }

        // The visible overlay may temporarily own Back; resolve its current handler at completion.
        webView?.evaluateJavaScript("typeof window.__zwaiAndroidBack === 'function' && window.__zwaiAndroidBack()")
    }
}
