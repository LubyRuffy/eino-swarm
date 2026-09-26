#!/usr/bin/env python3
"""Copy the current native app into a disposable offline layout test fixture."""
from pathlib import Path
import shutil
import tempfile

mobile = Path(__file__).resolve().parents[1]
if not (mobile / "ios/App/App/public/index.html").is_file():
    raise SystemExit("Run npm run build and npx cap copy ios first")
fixture = Path(tempfile.mkdtemp(prefix="zwai-ios-layout-"))
shutil.copytree(mobile / "ios", fixture / "mobile/ios")
(fixture / "mobile/node_modules").symlink_to(mobile / "node_modules", target_is_directory=True)
index = fixture / "mobile/ios/App/App/public/index.html"
index.write_text(index.read_text().replace(
    "<head>",
    '<head><script>history.replaceState(null,"","/?mock=1&tick=0");'
    'localStorage.setItem("zwai.phone.locale","en");</script>',
    1,
))
shutil.copyfile(
    mobile / "e2e/ios-wait-layout.swift",
    fixture / "mobile/ios/App/AppUITests/BindFlowTests.swift",
)
print(fixture)
