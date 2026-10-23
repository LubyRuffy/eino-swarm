"""Run the real resilience main with kernel/NS seams and actual private files."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import plistlib
import shutil
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent
SOURCE = ROOT / "native_macos_resilience_ax.m"
PREFIX = r'''
#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <libproc.h>
#include <sys/sysctl.h>
static NSString *fixtureMode, *fixtureExecutable, *fixtureCase;
static int fixtureActivations, fixtureAX, fixtureInputs, fixtureIdentityReads;
static double fixtureClock=10;
@interface CaseRunning:NSObject
+(instancetype)runningApplicationWithProcessIdentifier:(pid_t)pid;
@property(readonly) NSURL *bundleURL;
@property(readonly) pid_t processIdentifier;
@property(readonly) BOOL active;
-(BOOL)activateWithOptions:(NSApplicationActivationOptions)options;
-(BOOL)terminate;
@end
@implementation CaseRunning
+(instancetype)runningApplicationWithProcessIdentifier:(pid_t)pid {
    return [fixtureMode isEqual:@"unregistered"] ? nil : [self new];
}
-(NSURL *)bundleURL {
    if ([fixtureMode isEqual:@"NS-deadline"]) fixtureClock=12;
    if ([fixtureMode isEqual:@"missing-NS-bundle"]) return nil;
    NSString *path=fixtureExecutable.stringByDeletingLastPathComponent.stringByDeletingLastPathComponent.stringByDeletingLastPathComponent;
    return [NSURL fileURLWithPath:[fixtureMode isEqual:@"wrong-NS-bundle"] ? @"/private/foreign.app" : path];
}
-(pid_t)processIdentifier {return [fixtureMode isEqual:@"wrong-NS-PID"] ? 99 : 41;}
-(BOOL)active {return YES;}
-(BOOL)activateWithOptions:(NSApplicationActivationOptions)options {fixtureActivations++;return YES;}
-(BOOL)terminate {return NO;}
@end
@interface CaseClock:NSObject
+(instancetype)processInfo;
@property(readonly) double systemUptime;
@property(readonly) NSDictionary *environment;
@end
@implementation CaseClock
+(instancetype)processInfo {return [self new];}
-(double)systemUptime {return fixtureClock;}
-(NSDictionary *)environment {return NSProcessInfo.processInfo.environment;}
@end
@interface CaseLoop:NSObject
+(instancetype)mainRunLoop;
-(BOOL)runMode:(NSRunLoopMode)mode beforeDate:(NSDate *)date;
@end
@implementation CaseLoop
+(instancetype)mainRunLoop {return [self new];}
-(BOOL)runMode:(NSRunLoopMode)mode beforeDate:(NSDate *)date {fixtureClock+=.01;return YES;}
@end
@interface CaseWorkspace:NSObject
+(instancetype)sharedWorkspace;
@property(readonly) CaseWorkspace *frontmostApplication;
@property(readonly) pid_t processIdentifier;
@end
@implementation CaseWorkspace
+(instancetype)sharedWorkspace {return [self new];}
-(CaseWorkspace *)frontmostApplication {return self;}
-(pid_t)processIdentifier {return 41;}
@end
static int caseSysctlByName(const char *name,void *out,size_t *size,void *unused,size_t length) {
    *(int *)out=1;return 0;
}
static int caseProcInfo(int pid,int flavor,uint64_t arg,void *out,int size) {
    fixtureIdentityReads++;
    if ([fixtureMode isEqual:@"unreadable-kernel"]) return 0;
    struct proc_bsdinfo *info=out;memset(info,0,size);
    info->pbi_start_tvsec=1;info->pbi_start_tvusec=([fixtureMode isEqual:@"PID-reused"] && fixtureIdentityReads>2)
        || ([fixtureMode isEqual:@"PID-reused-after"] && fixtureActivations) ? 3 : 2;
    return size;
}
static int caseProcPath(int pid,void *out,uint32_t size) {
    NSString *path=[fixtureMode isEqual:@"exec-changed-after"] && fixtureActivations
        ? [fixtureExecutable stringByAppendingString:@"changed"] : fixtureExecutable;
    strlcpy(out,path.fileSystemRepresentation,size);return (int)strlen(out);
}
static int caseSysctl(int *mib,u_int count,void *out,size_t *size,void *unused,size_t length) {
    if ([fixtureMode isEqual:@"procargs-size-failed"] && !out) return -1;
    if ([fixtureMode isEqual:@"procargs-short"] && !out) {*size=1;return 0;}
    if ([fixtureMode isEqual:@"procargs-read-failed"] && out) return -1;
    if ([fixtureMode isEqual:@"procargs-read-short"] && out) {*size=1;return 0;}
    NSMutableData *data=[NSMutableData data];int argc=[fixtureMode isEqual:@"argc-zero"] ? 0 : 1;[data appendBytes:&argc length:sizeof(argc)];
    const char *path=fixtureExecutable.fileSystemRepresentation;char zero=0;
    [data appendBytes:path length:strlen(path)+1];[data appendBytes:&zero length:1];
    [data appendBytes:path length:strlen(path)+1];
    NSString *marker=[@"ZWAI_NATIVE_CASE_ID=" stringByAppendingString:[fixtureMode isEqual:@"wrong-marker"] ? @"foreign" : fixtureCase];
    [data appendBytes:marker.UTF8String length:strlen(marker.UTF8String)+1];
    if ([fixtureMode isEqual:@"no-marker"]) [data setLength:sizeof(argc)+strlen(path)*2+3];
    if ([fixtureMode isEqual:@"unterminated-executable"]) [data setLength:sizeof(argc)+strlen(path)];
    if ([fixtureMode isEqual:@"unterminated-argv"]) [data setLength:sizeof(argc)+strlen(path)*2+2];
    if ([fixtureMode isEqual:@"unterminated-environment"]) [data setLength:data.length-1];
    if (!out) {*size=data.length;return 0;}
    if ([fixtureMode isEqual:@"procargs-overflow"]) {*size+=1;return 0;}
    memcpy(out,data.bytes,data.length);*size=data.length;return 0;
}
static BOOL caseTrusted(void) {fixtureAX++;return NO;}
static AXError caseCopy(AXUIElementRef item,CFStringRef key,CFTypeRef *out) {fixtureAX++;*out=NULL;return kAXErrorFailure;}
static AXError caseAction(AXUIElementRef item,CFStringRef action) {fixtureAX++;return kAXErrorFailure;}
static void casePost(pid_t pid,CGEventRef event) {fixtureInputs++;}
#define NSRunningApplication CaseRunning
#define NSProcessInfo CaseClock
#define NSRunLoop CaseLoop
#define NSWorkspace CaseWorkspace
#define sysctlbyname caseSysctlByName
#define sysctl caseSysctl
#define proc_pidinfo caseProcInfo
#define proc_pidpath caseProcPath
#define AXIsProcessTrusted caseTrusted
#define AXUIElementCopyAttributeValue caseCopy
#define AXUIElementPerformAction caseAction
#define CGEventPostToPid casePost
#define main resilience_actual_main
'''
SUFFIX = r'''
#undef main
int main(int argc,const char **argv) {@autoreleasepool {
    if(argc!=4)return 9;
    fixtureMode=@(argv[1]);fixtureExecutable=@(argv[2]);fixtureCase=@(argv[3]);
    const char *input[]={"native","activate","41",""};
    int result=[fixtureMode isEqual:@"expired-core"]
        ? (activationCoreHash(fixtureExecutable,10) ? 0 : 2) : resilience_actual_main(4,input);
    fprintf(stderr,"\nCASE_CALLS {\"activations\":%d,\"AX\":%d,\"inputs\":%d}\n",fixtureActivations,fixtureAX,fixtureInputs);
    return result;
}}
'''


@unittest.skipUnless(platform.system() == "Darwin", "actual Objective-C main needs macOS SDK")
class NativeSuccessorIdentityTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temporary = tempfile.TemporaryDirectory(prefix="native-successor-actual-main-")
        cls.addClassCleanup(cls.temporary.cleanup)
        cls.directory = Path(cls.temporary.name).resolve()
        source = cls.directory / "actual.m"
        source.write_text(PREFIX + '#include "' + str(SOURCE) + '"\n' + SUFFIX)
        cls.binary = cls.directory / "actual"
        result = subprocess.run(["xcrun", "clang", "-fobjc-arc", "-mmacosx-version-min=13.0",
            "-framework", "AppKit", "-framework", "ApplicationServices", "-framework", "CoreGraphics",
            str(source), "-o", str(cls.binary)], capture_output=True, text=True, timeout=30)
        if result.returncode:
            raise RuntimeError(result.stderr)

    def setUp(self):
        self.case = "a" * 32
        self.round = tempfile.TemporaryDirectory(prefix="zwai-native-" + self.case + "-", dir=self.directory)
        self.addCleanup(self.round.cleanup)
        self.root = Path(self.round.name)
        self.original = self.root / "zwai.app"
        self.core(self.original).parent.mkdir(parents=True)
        self.core(self.original).write_bytes(b"same frozen core identity\n")
        self.successor = self.root / "home/Library/Caches/zwai/desktop-recovery-123/zwai.app"
        self.core(self.successor).parent.mkdir(parents=True)
        shutil.copyfile(self.core(self.original), self.core(self.successor))
        (self.successor.parent / "session").write_text("0123456789abcdef")

    @staticmethod
    def core(bundle):
        return bundle / "Contents/MacOS/zwai"

    def observe(self, bundle=None, mode="normal", executable=None, original=None, binding=None):
        environment = dict(os.environ, ZWAI_NATIVE_CASE_ID=self.case, ZWAI_NATIVE_APP=str(original or self.original))
        environment.pop("ZWAI_NATIVE_RECOVERY_IDENTITY", None)
        if binding is not None:
            environment["ZWAI_NATIVE_RECOVERY_IDENTITY"] = json.dumps(binding) if isinstance(binding, dict) else binding
        call = subprocess.run([str(self.binary), mode, str(executable or self.core(bundle or self.original)), self.case],
            env=environment, capture_output=True, text=True, timeout=3)
        prefix, metrics = call.stderr.rsplit("CASE_CALLS ", 1)
        row = json.loads(call.stdout) if call.stdout.strip() else {}
        return call.returncode, row, json.loads(metrics), prefix

    def test_original_and_same_core_recovery_copy_reach_real_activate_without_AX(self):
        for bundle in (self.original, self.successor):
            with self.subTest(bundle=bundle):
                code, row, calls, error = self.observe(bundle)
                self.assertEqual(code, 0, error)
                self.assertTrue(row["activation_only"])
                expected_hash = hashlib.sha256(self.core(self.original).read_bytes()).hexdigest()
                self.assertEqual(row["activation_identity"]["original_core_sha256"], expected_hash)
                self.assertEqual(row["activation_identity"]["target_core_sha256"], expected_hash)
                # Foundation and pathlib may choose different /var aliases of one inode.
                self.assertTrue(os.path.samefile(row["activation_identity"]["expected_target_bundle"], bundle))
                self.assertEqual(calls, {"activations": 1, "AX": 0, "inputs": 0})

    def test_other_case_layout_and_core_cannot_be_activated(self):
        invalid = [self.root / "other.app", self.root / "home/Library/Caches/zwai/other/zwai.app",
            self.directory / "zwai-native-other-case/zwai.app"]
        for bundle in invalid:
            self.core(bundle).parent.mkdir(parents=True, exist_ok=True)
            self.core(bundle).write_bytes(self.core(self.original).read_bytes())
        self.core(self.successor).write_bytes(b"different core\n")
        for bundle in [*invalid, self.successor]:
            with self.subTest(bundle=bundle):
                code, row, calls, error = self.observe(bundle)
                self.assertEqual(code, 2)
                self.assertEqual(calls, {"activations": 0, "AX": 0, "inputs": 0})

    def test_NS_identity_and_actual_process_marker_remain_required(self):
        for mode in ("unregistered", "missing-NS-bundle", "wrong-NS-bundle", "wrong-NS-PID", "wrong-marker", "NS-deadline", "unreadable-kernel"):
            with self.subTest(mode=mode):
                code, row, calls, error = self.observe(self.successor, mode)
                self.assertEqual(code, 2)
                self.assertEqual(calls, {"activations": 0, "AX": 0, "inputs": 0})
                self.assertIn("activation_identity", error)
                self.assertIn("expected_original_bundle", error)
                self.assertIn("reason", error)

    def test_PID_reuse_after_activation_is_refused(self):
        code, row, calls, error = self.observe(self.original, "PID-reused")
        self.assertEqual(code, 2)
        self.assertEqual(calls["AX"], 0)
        self.assertEqual(calls["inputs"], 0)

    def test_procargs_malformed_unreadable_or_missing_marker_never_grants_activation(self):
        for mode in ("procargs-size-failed", "procargs-short", "procargs-read-failed", "procargs-read-short",
                     "procargs-overflow", "argc-zero", "unterminated-executable",
                     "unterminated-argv", "unterminated-environment", "no-marker"):
            with self.subTest(mode=mode):
                code, row, calls, error = self.observe(mode=mode)
                self.assertEqual(code, 2, error)
                self.assertEqual(calls, {"activations": 0, "AX": 0, "inputs": 0})

    def test_session_and_core_must_exist_as_exact_nonempty_regular_files(self):
        session = self.successor.parent / "session"
        for text in ("", "short", "0123456789abcdeF", "0123456789abcdef\n"):
            session.write_text(text)
            self.assertEqual(self.observe(self.successor)[0], 2)
        session.unlink()
        self.assertEqual(self.observe(self.successor)[0], 2)
        session.write_text("0123456789abcdef")
        original = self.core(self.original)
        original.unlink()
        self.assertEqual(self.observe()[0], 2)
        original.mkdir()
        self.assertEqual(self.observe()[0], 2)
        original.rmdir()
        original.write_bytes(b"")
        self.assertEqual(self.observe()[0], 2)
        with original.open("wb") as stream:
            stream.truncate(2**32)
        self.assertEqual(self.observe()[0], 2)
        self.assertEqual(self.observe(mode="expired-core")[0], 2)

    def test_resolved_alias_retains_anchor_and_wrong_anchor_is_not_substituted(self):
        alias = self.root / "original-alias.app"
        alias.symlink_to(self.original, target_is_directory=True)
        self.assertEqual(self.observe(self.successor, original=alias)[0], 0)
        wrong = self.root / "missing.app"
        self.assertEqual(self.observe(self.successor, original=wrong)[0], 2)
        self.assertEqual(self.observe(executable=self.core(self.original).with_name("not-zwai"))[0], 2)

    def test_anchor_extension_and_case_root_are_not_inferred_from_any_app_path(self):
        invalid = [self.root / "not-an-app.bin", self.directory / "wrong-root/zwai.app"]
        for bundle in invalid:
            self.core(bundle).parent.mkdir(parents=True)
            self.core(bundle).write_bytes(b"same frozen core identity\n")
            code, row, calls, error = self.observe(bundle, original=bundle)
            self.assertEqual(code, 2, error)
            self.assertEqual(calls, {"activations": 0, "AX": 0, "inputs": 0})

    def test_birth_or_executable_change_after_request_is_not_confirmed(self):
        for mode in ("PID-reused-after", "exec-changed-after"):
            code, row, calls, error = self.observe(mode=mode)
            self.assertEqual(code, 2, error)
            self.assertEqual(calls, {"activations": 1, "AX": 0, "inputs": 0})
            self.assertFalse(row["activation_ready"])
            self.assertIn("kernel_after", row["activation_identity"])

    def test_actual_writer_signed_page_and_Info_variations_require_the_frozen_proof(self):
        spec = importlib.util.spec_from_file_location("recovery_identity", ROOT / "native_macos_recovery_identity.py")
        recovery = importlib.util.module_from_spec(spec); spec.loader.exec_module(recovery)
        source = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT.parent, text=True).strip()
        writer, _ = recovery.build_writer(ROOT.parent, self.root / "writer", source)
        fixture = self.root / "fixture.c"
        fixture.write_text("int main(void) { return 0; }\n")
        subprocess.run(["xcrun", "clang", str(fixture), "-o", str(self.core(self.original))], check=True, capture_output=True)
        (self.original / "Contents/Info.plist").write_bytes(plistlib.dumps({"CFBundleExecutable": "zwai", "CFBundleIdentifier": "app.zwai.desktop", "CFBundlePackageType": "APPL"}))
        subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", "--identifier", "app.zwai.desktop", "--pagesize", "4096", str(self.core(self.original))], check=True, capture_output=True)
        binding = recovery.prepare(writer, self.root / "writer/writer-binding.json", self.core(self.original), "1.2.3", source, self.root / "prepared", original_sha256=recovery.sha(self.core(self.original).read_bytes()))
        derived = self.root / "prepared/actual-writer"
        self.core(self.successor).write_bytes((derived / "copy1-core").read_bytes())
        (self.successor / "Contents/Info.plist").write_bytes((derived / "copy1-Info.plist").read_bytes())
        resources = self.successor / "Contents/Resources"; resources.mkdir()
        (resources / "AppIcon.icns").write_bytes((derived / "copy1-Resources-AppIcon.icns").read_bytes())
        self.assertNotEqual(self.core(self.original).read_bytes(), self.core(self.successor).read_bytes())
        self.assertEqual(self.observe(self.successor)[0], 2)
        code, row, calls, error = self.observe(self.successor, binding=binding)
        self.assertEqual(code, 0, error)
        self.assertTrue(row["activation_identity"]["proof_validated"])
        self.assertEqual(calls, {"activations": 1, "AX": 0, "inputs": 0})
        other = recovery.prepare(writer, self.root / "writer/writer-binding.json", self.core(self.original), "1.2.4", source, self.root / "other-version", original_sha256=recovery.sha(self.core(self.original).read_bytes()))
        self.core(self.successor).write_bytes((self.root / "other-version/actual-writer/copy1-core").read_bytes())
        self.assertEqual(self.observe(self.successor, binding=binding)[0], 2)
        self.assertEqual(self.observe(self.successor, binding=dict(other, version="1.2.3"))[0], 2)
        self.core(self.successor).write_bytes((derived / "copy1-core").read_bytes())
        for offset in (4096, -1):
            data = bytearray(self.core(self.successor).read_bytes()); data[offset] ^= 1
            self.core(self.successor).write_bytes(data)
            self.assertEqual(self.observe(self.successor, binding=binding)[0], 2)
            self.core(self.successor).write_bytes((derived / "copy1-core").read_bytes())
        (self.successor / "Contents/Info.plist").write_bytes(b"changed Info")
        self.assertEqual(self.observe(self.successor, binding=binding)[0], 2)
        # A caller-provided full hash cannot turn a broken original signature
        # into a trusted origin merely because the writer re-signs its copy.
        damaged = bytearray(self.core(self.original).read_bytes()); damaged[4096] ^= 1
        self.core(self.original).write_bytes(damaged)
        with self.assertRaisesRegex(RuntimeError, "original-strict-signature"):
            recovery.prepare(writer, self.root / "writer/writer-binding.json", self.core(self.original),
                             "1.2.3", source, self.root / "damaged-origin", original_sha256=recovery.sha(damaged))
        self.assertFalse((self.root / "damaged-origin/actual-writer").exists())
        self.assertTrue((self.root / "damaged-origin/original-strict-signature.stderr").exists())

    def test_bad_frozen_proof_never_falls_back_to_equal_legacy_hashes(self):
        info, icon = b"fixture info", b"fixture icon"
        (self.successor / "Contents/Info.plist").write_bytes(info)
        resources = self.successor / "Contents/Resources"; resources.mkdir()
        (resources / "AppIcon.icns").write_bytes(icon)
        digest = lambda data: hashlib.sha256(data).hexdigest()
        proof = dict(schema=1, source_sha="c"*40, version="1.2.3",
                     original_core_sha256=digest(self.core(self.original).read_bytes()),
                     derived_core_sha256=digest(self.core(self.successor).read_bytes()),
                     derived_info_sha256=digest(info), derived_icon_sha256=digest(icon), preparation_sha256="a"*64)
        path = self.root / "proof.json"
        def freeze(value):
            path.write_text(json.dumps(value))
            return dict(path=str(path), sha256=digest(path.read_bytes()), source_sha="c"*40, version="1.2.3")
        binding = freeze(proof)
        self.assertEqual(self.observe(self.successor, binding=binding)[0], 0)
        mutations = [{}, dict(proof, schema=True), dict(proof, schema=1.0), dict(proof, schema=2), dict(proof, extra=1),
                     dict(proof, original_core_sha256="b"*64), dict(proof, derived_core_sha256="b"*64),
                     dict(proof, derived_info_sha256="b"*64), dict(proof, derived_icon_sha256="b"*64),
                     dict(proof, preparation_sha256=""), dict(proof, preparation_sha256="a"*64+"\n"),
                     dict(proof, version="1.2.4"), dict(proof, source_sha="b"*40)]
        for value in mutations:
            with self.subTest(proof=value):
                code, row, calls, error = self.observe(self.successor, binding=freeze(value))
                self.assertEqual(code, 2, error)
                self.assertEqual(calls, {"activations": 0, "AX": 0, "inputs": 0})
        binding = freeze(proof)
        for value in ("bad JSON", "[]", "x"*8193, {}, dict(binding, extra=1), dict(binding, path="relative"),
                      dict(binding, sha256="b"*64), dict(binding, sha256=binding["sha256"]+"\n"),
                      dict(binding, source_sha=""), dict(binding, source_sha="c"*40+"\n"),
                      dict(binding, version=""), dict(binding, version="1.2.3\n"),
                      dict(binding, path=str(self.root / "missing"))):
            self.assertEqual(self.observe(self.successor, binding=value)[0], 2)
        path.write_text("{")
        self.assertEqual(self.observe(self.successor, binding=dict(binding, sha256=digest(path.read_bytes())))[0], 2)
        path.write_bytes(b"x"*32769)
        self.assertEqual(self.observe(self.successor, binding=binding)[0], 2)
        path.unlink(); path.symlink_to(self.core(self.original))
        self.assertEqual(self.observe(self.successor, binding=binding)[0], 2)


if __name__ == "__main__":
    unittest.main(verbosity=2)
