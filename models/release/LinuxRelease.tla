----------------------------- MODULE LinuxRelease -----------------------------
EXTENDS TLC

(* Two architecture artifacts. Public bytes are immutable. Every new upload
   needs a runtime receipt for the same name/architecture/source/bytes,
   including the managed host's crash/reboot and paired recovery checks. *)
CONSTANTS
    ArmOnly,
    IgnoreMachine,
    IgnoreReceipt,
    RebuildPublic

VARIABLES phase, arch, machine, receipt, public, bytes, uploaded, accepted
vars == <<phase, arch, machine, receipt, public, bytes, uploaded, accepted>>

Init ==
    /\ phase = "idle" /\ arch \in {"arm64", "amd64"}
    /\ machine \in {"arm64", "amd64"}
    /\ receipt \in {"matching", "missing", "wrong-arch", "wrong-source", "wrong-bytes", "missing-recovery"}
    /\ public \in BOOLEAN /\ bytes = "original"
    /\ uploaded = FALSE /\ accepted = FALSE

Prepare ==
    /\ phase = "idle"
    /\ phase' = IF ArmOnly /\ arch = "amd64" THEN "unsupported" ELSE "prepared"
    /\ bytes' = IF public /\ RebuildPublic THEN "rebuilt" ELSE bytes
    /\ UNCHANGED <<arch, machine, receipt, public, uploaded, accepted>>

Validate ==
    /\ phase = "prepared"
    /\ accepted' = (machine = arch \/ IgnoreMachine) /\ (public \/ receipt = "matching" \/ IgnoreReceipt)
    /\ phase' = "validated"
    /\ UNCHANGED <<arch, machine, receipt, public, bytes, uploaded>>

Upload ==
    /\ phase = "validated" /\ accepted
    /\ phase' = "done" /\ uploaded' = TRUE
    /\ UNCHANGED <<arch, machine, receipt, public, bytes, accepted>>

Next == Prepare \/ Validate \/ Upload
Spec == Init /\ [][Next]_vars
TypeOK ==
    /\ phase \in {"idle", "unsupported", "prepared", "validated", "done"}
    /\ arch \in {"arm64", "amd64"} /\ machine \in {"arm64", "amd64"}
    /\ receipt \in {"matching", "missing", "wrong-arch", "wrong-source", "wrong-bytes", "missing-recovery"}
    /\ public \in BOOLEAN /\ bytes \in {"original", "rebuilt"}
    /\ uploaded \in BOOLEAN /\ accepted \in BOOLEAN
BothArchitecturesSupported == phase # "unsupported"
MachineMatchesName == uploaded => machine = arch
ReceiptMatchesNewUpload == uploaded /\ ~public => receipt = "matching"
PublicBytesPreserved == public => bytes = "original"
NeverAMD64Uploaded == ~(uploaded /\ arch = "amd64" /\ ~public)
NeverARM64Uploaded == ~(uploaded /\ arch = "arm64" /\ ~public)
NeverPublicRetry == ~(uploaded /\ public)
=============================================================================
