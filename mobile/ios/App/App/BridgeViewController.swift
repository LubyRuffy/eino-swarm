import Capacitor

/// The storyboard loads this instead of CAPBridgeViewController so the
/// streaming call exists before the page's first fetch. capacitorDidLoad
/// runs after the bridge exists and before the webview navigates.
@objc(BridgeViewController)
public class BridgeViewController: CAPBridgeViewController {
    override public func capacitorDidLoad() {
        super.capacitorDidLoad()
        bridge?.registerPluginInstance(StreamBodyPlugin())
    }
}
