#!/usr/bin/env python3
"""Copy the current native app into a disposable offline layout test fixture."""
from pathlib import Path
import json
import os
import shutil
import tempfile

mobile = Path(__file__).resolve().parents[1]
public = Path(os.environ.get("IOS_LAYOUT_PUBLIC", str(mobile / "ios/App/App/public")))
if not (public / "index.html").is_file():
    raise SystemExit("Run npm run build and npx cap copy ios first")
fixture = Path(tempfile.mkdtemp(prefix="zwai-ios-layout-"))
shutil.copytree(mobile / "ios", fixture / "mobile/ios")
(fixture / "mobile/node_modules").symlink_to(mobile / "node_modules", target_is_directory=True)
index = fixture / "mobile/ios/App/App/public/index.html"
if "IOS_LAYOUT_PUBLIC" in os.environ:
    shutil.rmtree(index.parent)
    shutil.copytree(public, index.parent)
query = os.environ.get("IOS_LAYOUT_QUERY", "/?mock=1&tick=0")
locale = os.environ.get("IOS_LAYOUT_LOCALE", "en")
if locale not in ("en", "zh"):
    raise SystemExit("IOS_LAYOUT_LOCALE must be en or zh")
bootstrap = ('history.replaceState(null,"",' + json.dumps(query) + ');'
             'localStorage.setItem("zwai.phone.locale",' + json.dumps(locale) + ');')
if os.environ.get("IOS_LAYOUT_PRESERVE_PUBLIC") == "1":
    # A signed IPA's web bytes can be used unchanged in the simulator. Only
    # this private native wrapper selects offline input before the first page.
    bridge = fixture / "mobile/ios/App/App/BridgeViewController.swift"
    body = bridge.read_text()
    marker = "        super.capacitorDidLoad()"
    if body.count(marker) != 1:
        raise SystemExit("The private bridge needs exactly one bootstrap boundary")
    body = body.replace("import UIKit", "import UIKit\nimport WebKit", 1)
    body = body.replace(marker, marker + '\n        webView?.configuration.userContentController.addUserScript(WKUserScript(source: '
                        + json.dumps(bootstrap) + ', injectionTime: .atDocumentStart, forMainFrameOnly: true))', 1)
    bridge.write_text(body)
else:
    index.write_text(index.read_text().replace("<head>", "<head><script>" + bootstrap + "</script>", 1))
shutil.copyfile(
    mobile / "e2e/ios-wait-layout.swift",
    fixture / "mobile/ios/App/AppUITests/BindFlowTests.swift",
)
print(fixture)
