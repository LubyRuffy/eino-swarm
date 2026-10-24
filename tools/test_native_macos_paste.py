"""Run the production paste guards without a GUI, AX or clipboard access."""
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SOURCE = Path(__file__).with_name("native_macos_resilience_ax.m")


@unittest.skipUnless(sys.platform == "darwin", "macOS SDK required for the real Objective-C helper")
class NativePastePredicates(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        directory = Path(cls.temp.name)
        source = directory / "paste-predicates.m"
        source.write_text('#define main unusedNativeMain\n#include ' + json.dumps(str(SOURCE.resolve())) + '''
#undef main
#include <stdio.h>
#include <string.h>
#define CHECK(name, value) do { if (!(value)) { fprintf(stderr, "%s\\n", name); return 1; } cases++; } while (0)
int main(int argc, const char **argv) { @autoreleasepool {
    if (argc != 2) return 2;
    int cases = 0;
    if (!strcmp(argv[1], "clipboard")) {
        NSData *source = [NSData dataWithBytes:"abc" length:3];
        NSData *same = [NSData dataWithBytes:"abc" length:3];
        NSData *changed = [NSData dataWithBytes:"abd" length:3];
        CHECK("original bytes", validPasteboardPayload(source, YES, same, 12, 12));
        CHECK("failed write with old matching bytes", !validPasteboardPayload(source, NO, same, 12, 12));
        CHECK("missing source", !validPasteboardPayload(nil, YES, same, 12, 12));
        CHECK("empty source", !validPasteboardPayload(NSData.data, YES, NSData.data, 12, 12));
        CHECK("missing readback", !validPasteboardPayload(source, YES, nil, 12, 12));
        CHECK("same length changed bytes", !validPasteboardPayload(source, YES, changed, 12, 12));
        CHECK("changed owner", !validPasteboardPayload(source, YES, same, 12, 13));
        CHECK("unknown counter", !validPasteboardPayload(source, YES, same, -1, -1));
    } else if (!strcmp(argv[1], "labels")) {
        CHECK("Edit", matchesPasteMenu(@"AXMenuBarItem", @"Edit", YES));
        CHECK("Chinese Edit", matchesPasteMenu(@"AXMenuBarItem", @"编辑", YES));
        CHECK("Paste", matchesPasteMenu(@"AXMenuItem", @"Paste", NO));
        CHECK("Chinese Paste", matchesPasteMenu(@"AXMenuItem", @"粘贴", NO));
        CHECK("wrong Edit role", !matchesPasteMenu(@"AXMenuItem", @"Edit", YES));
        CHECK("wrong Paste role", !matchesPasteMenu(@"AXButton", @"Paste", NO));
        CHECK("other paste operation", !matchesPasteMenu(@"AXMenuItem", @"Paste and Match Style", NO));
        CHECK("missing role", !matchesPasteMenu(nil, @"Paste", NO));
        CHECK("missing title", !matchesPasteMenu(@"AXMenuItem", nil, NO));
    } else if (!strcmp(argv[1], "target")) {
        CHECK("owned enabled foreground target", validPasteMenuTarget(1, 73, 73, @YES, 73));
        CHECK("missing", !validPasteMenuTarget(0, 73, 73, @YES, 73));
        CHECK("ambiguous", !validPasteMenuTarget(2, 73, 73, @YES, 73));
        CHECK("wrong owner", !validPasteMenuTarget(1, 74, 73, @YES, 73));
        CHECK("unknown target", !validPasteMenuTarget(1, 0, 0, @YES, 0));
        CHECK("background target", !validPasteMenuTarget(1, 73, 73, @YES, 74));
        CHECK("disabled", !validPasteMenuTarget(1, 73, 73, @NO, 73));
        CHECK("missing enabled", !validPasteMenuTarget(1, 73, 73, nil, 73));
        CHECK("unknown enabled", !validPasteMenuTarget(1, 73, 73, NSNull.null, 73));
        CHECK("string enabled", !validPasteMenuTarget(1, 73, 73, @"true", 73));
    } else return 2;
    printf("{\\"group\\":\\"%s\\",\\"cases\\":%d}\\n", argv[1], cases);
    return 0;
} }
''')
        cls.binary = directory / "paste-predicates"
        command = ["/usr/bin/xcrun", "clang", "-fobjc-arc", "-framework", "AppKit",
                   "-framework", "ApplicationServices", str(source), "-o", str(cls.binary)]
        result = subprocess.run(command, capture_output=True, text=True, timeout=30)
        if result.returncode:
            raise RuntimeError("production helper predicate compilation failed:\n" + result.stderr)

    def check_group(self, group, count):
        result = subprocess.run([str(self.binary), group], capture_output=True, text=True, timeout=5)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(result.stdout), dict(group=group, cases=count))

    def test_clipboard_requires_the_acknowledged_unchanged_original_bytes(self):
        self.check_group("clipboard", 8)

    def test_menu_matches_only_the_exact_native_roles_and_paste_action(self):
        self.check_group("labels", 9)

    def test_menu_refuses_ambiguous_disabled_background_or_other_process_items(self):
        self.check_group("target", 10)


if __name__ == "__main__":
    unittest.main()
