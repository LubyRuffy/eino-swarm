import Capacitor
import UIKit

/// The storyboard loads this instead of CAPBridgeViewController so the
/// streaming call exists before the page's first fetch. capacitorDidLoad
/// runs after the bridge exists and before the webview navigates.
@objc(BridgeViewController)
public class BridgeViewController: CAPBridgeViewController, UIGestureRecognizerDelegate {
    private let fileSavePlugin = FileSavePlugin()

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
