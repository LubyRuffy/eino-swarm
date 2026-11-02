------------------------ MODULE ScheduleAuthorization ------------------------
(***************************************************************************)
(* One conversation, one existing schedule and one fresh schedule identity. *)
(* "claimed" means the stored one-shot is done while its run is running.   *)
(* Human input meaning is an explicit input to this protocol; this model    *)
(* cannot prove a language classifier's semantic accuracy.                 *)
(*                                                                         *)
(* Each action is one observable locked/transactional step in the named Go *)
(* boundary. Authorization read and write are separate, as are an inbox    *)
(* destination read and its store mutation. Oracle sequence numbers retain *)
(* human intent independently of the implementation's durable receipts.    *)
(* Existing/manual active waits are permitted under a denial: the property *)
(* forbids a NEW manager arm, rather than requiring zero active rows.       *)
(* Bounds: one fire, cancellation, allow and continuation; one fresh ID,   *)
(* at most MaxAttempts attempts and one successful manager mutation.      *)
(* This checks finite safety interleavings, not unbounded work or liveness.*)
(* All switches FALSE is the required protocol, not an assertion that the  *)
(* implementation already implements every modeled guard.                 *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
    CancelDoesNotRevoke,
    ClaimedCancelIgnored,
    ManagerCancelledMasksHuman,
    ContinueGrantsPermission,
    LateAllowOverridesCancel,
    CommitSkipsPolicyRecheck,
    RestartForgetsPolicy,
    CompactForgetsPolicy,
    CancelReceiptNotAtomic,
    CancelDestinationUnchecked,
    RetargetIgnoresStatus,
    RepeatedDeleteRevokes,
    MaxAttempts

Destinations == {"home", "other", "independent"}
Statuses == {"active", "paused", "claimed", "done", "cancelled"}
Commands == {"create", "replace", "report"}
Editable == {"active", "paused"}

VARIABLES
    status, destination, fresh, freshActor, oneShot,
    seq, requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
    allowPending, continuePending, continued, grant, forgotten,
    cancelPhase, selectedDestination, humanReceipt, ackHome, ackClaimed,
    unreceipted, cancelDestinationValid, terminalDestination,
    editPhase, editedDestination,
    managerCancelled, paused, resumed, fired, completed,
    restarted, compacted, repeated,
    armPhase, command, sampledPermission, attempts,
    armCommitted, armAuthorized, armCommand, refused,
    ghost

vars == <<status, destination, fresh, freshActor, oneShot, seq, requestedCancelAt,
          receiptAt, allowAt, classifiedAllowAt, allowPending, continuePending,
          continued, grant, forgotten, cancelPhase, selectedDestination,
          humanReceipt, ackHome, ackClaimed, unreceipted,
          cancelDestinationValid, terminalDestination, editPhase,
          editedDestination, managerCancelled, paused, resumed, fired,
          completed, restarted, compacted, repeated, armPhase, command,
          sampledPermission, attempts, armCommitted, armAuthorized,
          armCommand, refused, ghost>>

HumanPermits == requestedCancelAt = 0 \/ allowAt > requestedCancelAt
RuntimePermits ==
    /\ ~allowPending /\ ~continuePending
    /\ (grant \/ forgotten \/ receiptAt = 0 \/ classifiedAllowAt > receiptAt)

Init ==
    /\ status = "active" /\ destination \in {"home", "independent"}
    /\ fresh = "absent" /\ freshActor = "none"
    /\ oneShot \in BOOLEAN
    /\ seq = 0 /\ requestedCancelAt = 0 /\ receiptAt = 0
    /\ allowAt = 0 /\ classifiedAllowAt = 0
    /\ allowPending = FALSE /\ continuePending = FALSE /\ continued = FALSE
    /\ grant = FALSE /\ forgotten = FALSE
    /\ cancelPhase = "none" /\ selectedDestination = "home"
    /\ humanReceipt = FALSE /\ ackHome = FALSE /\ ackClaimed = FALSE
    /\ unreceipted = FALSE /\ cancelDestinationValid = TRUE
    /\ terminalDestination = "none"
    /\ editPhase = "none" /\ editedDestination = "other"
    /\ managerCancelled = FALSE /\ paused = FALSE /\ resumed = FALSE
    /\ fired = FALSE /\ completed = FALSE
    /\ restarted = FALSE /\ compacted = FALSE /\ repeated = FALSE
    /\ armPhase = "none" /\ command = "create" /\ sampledPermission = FALSE
    /\ attempts = 0 /\ armCommitted = FALSE /\ armAuthorized = TRUE
    /\ armCommand = "none" /\ refused = FALSE /\ ghost = {}

(* Original human allow/continue events are ordered before interpretation. *)
HumanAllow ==
    /\ allowAt = 0 /\ seq < 3
    /\ seq' = seq + 1 /\ allowAt' = seq + 1 /\ allowPending' = TRUE
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, requestedCancelAt,
                   receiptAt, classifiedAllowAt, continuePending, continued,
                   grant, forgotten, cancelPhase, selectedDestination,
                   humanReceipt, ackHome, ackClaimed, unreceipted,
                   cancelDestinationValid, terminalDestination, editPhase,
                   editedDestination, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

ClassifyAllow ==
    /\ allowPending
    /\ classifiedAllowAt' = allowAt /\ allowPending' = FALSE
    /\ grant' = (grant \/ (LateAllowOverridesCancel /\ allowAt < receiptAt))
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, continuePending,
                   continued, forgotten, cancelPhase, selectedDestination,
                   humanReceipt, ackHome, ackClaimed, unreceipted,
                   cancelDestinationValid, terminalDestination, editPhase,
                   editedDestination, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

HumanContinue ==
    /\ ~continued /\ seq < 3
    /\ seq' = seq + 1 /\ continued' = TRUE /\ continuePending' = TRUE
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, requestedCancelAt,
                   receiptAt, allowAt, classifiedAllowAt, allowPending,
                   grant, forgotten, cancelPhase, selectedDestination,
                   humanReceipt, ackHome, ackClaimed, unreceipted,
                   cancelDestinationValid, terminalDestination, editPhase,
                   editedDestination, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

ClassifyContinue ==
    /\ continuePending
    /\ continuePending' = FALSE
    /\ grant' = (grant \/ ContinueGrantsPermission)
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continued, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

(* Engine.CancelSchedule reads a destination before the mutation. *)
SelectHumanCancel ==
    /\ cancelPhase = "none"
    /\ cancelPhase' = "selected" /\ selectedDestination' = destination
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant, forgotten,
                   humanReceipt, ackHome, ackClaimed, unreceipted,
                   cancelDestinationValid, terminalDestination, editPhase,
                   editedDestination, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

(* Store.CancelScheduleByHuman: first human action owns a typed receipt,    *)
(* even for a consumed fire or a row the manager already cancelled.        *)
CommitHumanCancel ==
    /\ cancelPhase = "selected" /\ seq < 3
    /\ CancelDestinationUnchecked \/ selectedDestination = destination
    /\ LET here == destination = "home"
           ignored == (ClaimedCancelIgnored /\ status \in {"claimed", "done"})
                      \/ (ManagerCancelledMasksHuman /\ managerCancelled /\ status = "cancelled")
           omitted == CancelDoesNotRevoke \/ ignored
                      \/ (CancelDestinationUnchecked /\ selectedDestination = "independent")
           split == here /\ ~omitted /\ CancelReceiptNotAtomic
       IN /\ status' = IF ignored THEN status ELSE "cancelled"
          /\ seq' = IF here THEN seq + 1 ELSE seq
          /\ requestedCancelAt' = IF here THEN seq + 1 ELSE requestedCancelAt
          /\ receiptAt' = IF here /\ ~omitted /\ ~split THEN seq + 1 ELSE receiptAt
          /\ humanReceipt' = (here /\ ~omitted /\ ~split)
          /\ ackHome' = (here /\ ~split)
          /\ ackClaimed' = (here /\ status \in {"claimed", "done"})
          /\ cancelPhase' = IF split THEN "unrecorded" ELSE "done"
          /\ unreceipted' = split
          /\ grant' = IF here /\ ~omitted THEN FALSE ELSE grant
          /\ forgotten' = IF here /\ ~omitted THEN FALSE ELSE forgotten
          /\ terminalDestination' = IF ignored THEN terminalDestination ELSE destination
    /\ cancelDestinationValid' = (selectedDestination = destination)
    /\ UNCHANGED <<oneShot, destination, fresh, freshActor, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, editPhase,
                   selectedDestination, editedDestination, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

RefuseMovedCancel ==
    /\ cancelPhase = "selected" /\ selectedDestination # destination
    /\ ~CancelDestinationUnchecked
    /\ cancelPhase' = "refused" /\ ghost' = ghost \cup {"movedCancelRefused"}
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant, forgotten,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused>>

(* A rejected transaction commits neither row, event sequence nor authority. *)
(* This is its observable contract, not a proof of SQLite's implementation. *)
RefuseCancelStorage ==
    /\ cancelPhase = "selected"
    /\ cancelPhase' = "failed"
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant, forgotten,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

(* A repeated DELETE after a later allow is the same old human action. *)
RepeatHumanCancel ==
    /\ humanReceipt /\ ~repeated /\ seq < 4
    /\ repeated' = TRUE
    /\ receiptAt' = IF RepeatedDeleteRevokes THEN seq + 1 ELSE receiptAt
    /\ seq' = IF RepeatedDeleteRevokes THEN seq + 1 ELSE seq
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, requestedCancelAt,
                   allowAt, classifiedAllowAt, allowPending, continuePending,
                   continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, fired, completed, restarted, compacted,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

(* ClaimScheduledFire stores done before starting this one-shot's run. *)
ClaimFire ==
    /\ status = "active" /\ destination = "home" /\ ~fired
    /\ status' = IF oneShot THEN "claimed" ELSE "active"
    /\ fired' = TRUE
    /\ terminalDestination' = IF oneShot THEN destination ELSE terminalDestination
    /\ UNCHANGED <<oneShot, destination, fresh, freshActor, seq, requestedCancelAt,
                   receiptAt, allowAt, classifiedAllowAt, allowPending,
                   continuePending, continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

CompleteFire ==
    /\ fired /\ ~completed
    /\ status' = IF status = "claimed" THEN "done" ELSE status
    /\ completed' = TRUE
    /\ UNCHANGED <<oneShot, destination, fresh, freshActor, seq, requestedCancelAt,
                   receiptAt, allowAt, classifiedAllowAt, allowPending,
                   continuePending, continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, fired, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

ManagerCancel ==
    /\ status \in Editable /\ ~managerCancelled
    /\ status' = "cancelled" /\ managerCancelled' = TRUE
    /\ terminalDestination' = destination
    /\ UNCHANGED <<oneShot, destination, fresh, freshActor, seq, requestedCancelAt,
                   receiptAt, allowAt, classifiedAllowAt, allowPending,
                   continuePending, continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, editPhase,
                   editedDestination, paused, resumed, fired, completed,
                   restarted, compacted, repeated, armPhase, command,
                   sampledPermission, attempts, armCommitted, armAuthorized,
                   armCommand, refused, ghost>>

Pause ==
    /\ status = "active" /\ ~paused
    /\ status' = "paused" /\ paused' = TRUE
    /\ UNCHANGED <<oneShot, destination, fresh, freshActor, seq, requestedCancelAt,
                   receiptAt, allowAt, classifiedAllowAt, allowPending,
                   continuePending, continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

HumanResume ==
    /\ status = "paused" /\ ~resumed
    /\ status' = "active" /\ resumed' = TRUE
    /\ UNCHANGED <<oneShot, destination, fresh, freshActor, seq, requestedCancelAt,
                   receiptAt, allowAt, classifiedAllowAt, allowPending,
                   continuePending, continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

SelectEdit ==
    /\ editPhase = "none" /\ status \in Editable
    /\ editPhase' = "selected"
    /\ editedDestination' \in Destinations \ {destination}
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant, forgotten,
                   cancelPhase, selectedDestination, humanReceipt, ackHome,
                   ackClaimed, unreceipted, cancelDestinationValid,
                   terminalDestination, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

CommitEdit ==
    /\ editPhase = "selected"
    /\ RetargetIgnoresStatus \/ status \in Editable
    /\ destination' = editedDestination /\ editPhase' = "done"
    /\ UNCHANGED <<oneShot, status, fresh, freshActor, seq, requestedCancelAt,
                   receiptAt, allowAt, classifiedAllowAt, allowPending,
                   continuePending, continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editedDestination, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

RefuseTerminalEdit ==
    /\ editPhase = "selected" /\ status \notin Editable /\ ~RetargetIgnoresStatus
    /\ editPhase' = "refused" /\ ghost' = ghost \cup {"terminalEditRefused"}
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant, forgotten,
                   cancelPhase, selectedDestination, humanReceipt, ackHome,
                   ackClaimed, unreceipted, cancelDestinationValid,
                   terminalDestination, editedDestination, managerCancelled,
                   paused, resumed, fired, completed, restarted, compacted,
                   repeated, armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused>>

CanMutate(cmd) ==
    IF cmd = "create" THEN fresh = "absent"
    ELSE /\ destination = "home"
         /\ IF cmd = "replace" THEN status \in Editable
            ELSE fired /\ ~completed /\ status \in {"active", "claimed"}

ReadArmAuthorization ==
    /\ armPhase = "none" /\ ~armCommitted /\ attempts < MaxAttempts
    /\ command' \in Commands
    /\ (command' = "report" => (fired /\ ~completed))
    /\ sampledPermission' = RuntimePermits
    /\ attempts' = attempts + 1 /\ armPhase' = "checked"
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant, forgotten,
                   cancelPhase, selectedDestination, humanReceipt, ackHome,
                   ackClaimed, unreceipted, cancelDestinationValid,
                   terminalDestination, editPhase, editedDestination,
                   managerCancelled, paused, resumed, fired, completed,
                   restarted, compacted, repeated, armCommitted,
                   armAuthorized, armCommand, refused, ghost>>

(* All three manager mutations read policy AGAIN in the write transaction. *)
CommitArm ==
    /\ armPhase = "checked" /\ sampledPermission /\ CanMutate(command)
    /\ CommitSkipsPolicyRecheck \/ RuntimePermits
    /\ status' = IF command = "report" THEN "active" ELSE status
    /\ fresh' = IF command = "create" THEN "active" ELSE fresh
    /\ freshActor' = IF command = "create" THEN "manager" ELSE freshActor
    /\ armCommitted' = TRUE /\ armAuthorized' = HumanPermits
    /\ armCommand' = command /\ armPhase' = "none"
    /\ ghost' = ghost \cup {command}
                  \cup (IF command = "create" /\ requestedCancelAt > 0
                        THEN {"freshAfterCancel"} ELSE {})
                  \cup (IF command = "create" /\ humanReceipt /\ allowAt > requestedCancelAt
                        THEN {"allowedFreshAfterCancel"} ELSE {})
    /\ UNCHANGED <<oneShot, destination, seq, requestedCancelAt, receiptAt, allowAt,
                   classifiedAllowAt, allowPending, continuePending,
                   continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, fired, completed, restarted, compacted, repeated,
                   command, sampledPermission, attempts, refused>>

RefuseArm ==
    /\ armPhase = "checked"
    /\ (~sampledPermission \/ ~CanMutate(command)
        \/ (~CommitSkipsPolicyRecheck /\ ~RuntimePermits))
    /\ armPhase' = "none" /\ refused' = TRUE
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant, forgotten,
                   cancelPhase, selectedDestination, humanReceipt, ackHome,
                   ackClaimed, unreceipted, cancelDestinationValid,
                   terminalDestination, editPhase, editedDestination,
                   managerCancelled, paused, resumed, fired, completed,
                   restarted, compacted, repeated, command,
                   sampledPermission, attempts, armCommitted,
                   armAuthorized, armCommand, ghost>>

(* Direct human creation is not a manager-owned grant and stays available. *)
HumanCreateFresh ==
    /\ fresh = "absent"
    /\ fresh' = "active" /\ freshActor' = "human"
    /\ UNCHANGED <<oneShot, status, destination, seq, requestedCancelAt, receiptAt,
                   allowAt, classifiedAllowAt, allowPending, continuePending,
                   continued, grant, forgotten, cancelPhase,
                   selectedDestination, humanReceipt, ackHome, ackClaimed,
                   unreceipted, cancelDestinationValid, terminalDestination,
                   editPhase, editedDestination, managerCancelled, paused,
                   resumed, fired, completed, restarted, compacted, repeated,
                   armPhase, command, sampledPermission, attempts,
                   armCommitted, armAuthorized, armCommand, refused, ghost>>

Restart ==
    /\ ~restarted
    /\ restarted' = TRUE /\ forgotten' = (forgotten \/ RestartForgetsPolicy)
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant,
                   cancelPhase, selectedDestination, humanReceipt, ackHome,
                   ackClaimed, unreceipted, cancelDestinationValid,
                   terminalDestination, editPhase, editedDestination,
                   managerCancelled, paused, resumed, fired, completed,
                   compacted, repeated, armPhase, command, sampledPermission,
                   attempts, armCommitted, armAuthorized, armCommand,
                   refused, ghost>>

Compact ==
    /\ ~compacted
    /\ compacted' = TRUE /\ forgotten' = (forgotten \/ CompactForgetsPolicy)
    /\ UNCHANGED <<oneShot, status, destination, fresh, freshActor, seq,
                   requestedCancelAt, receiptAt, allowAt, classifiedAllowAt,
                   allowPending, continuePending, continued, grant,
                   cancelPhase, selectedDestination, humanReceipt, ackHome,
                   ackClaimed, unreceipted, cancelDestinationValid,
                   terminalDestination, editPhase, editedDestination,
                   managerCancelled, paused, resumed, fired, completed,
                   restarted, repeated, armPhase, command, sampledPermission,
                   attempts, armCommitted, armAuthorized, armCommand,
                   refused, ghost>>

Next == HumanAllow \/ ClassifyAllow \/ HumanContinue \/ ClassifyContinue
        \/ SelectHumanCancel \/ CommitHumanCancel \/ RefuseMovedCancel
        \/ RefuseCancelStorage \/ RepeatHumanCancel
        \/ ClaimFire \/ CompleteFire \/ ManagerCancel \/ Pause \/ HumanResume
        \/ SelectEdit \/ CommitEdit \/ RefuseTerminalEdit
        \/ ReadArmAuthorization \/ CommitArm \/ RefuseArm \/ HumanCreateFresh
        \/ Restart \/ Compact

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ oneShot \in BOOLEAN
    /\ status \in Statuses /\ destination \in Destinations
    /\ fresh \in {"absent", "active"} /\ freshActor \in {"none", "human", "manager"}
    /\ seq \in 0..4 /\ requestedCancelAt \in 0..3 /\ receiptAt \in 0..4
    /\ allowAt \in 0..3 /\ classifiedAllowAt \in 0..3
    /\ cancelPhase \in {"none", "selected", "unrecorded", "done", "refused", "failed"}
    /\ selectedDestination \in Destinations
    /\ terminalDestination \in Destinations \cup {"none"}
    /\ editPhase \in {"none", "selected", "done", "refused"}
    /\ editedDestination \in Destinations /\ armPhase \in {"none", "checked"}
    /\ command \in Commands /\ armCommand \in Commands \cup {"none"}
    /\ attempts \in 0..MaxAttempts
    /\ \A b \in {allowPending, continuePending, continued, grant, forgotten,
                   humanReceipt, ackHome, ackClaimed, unreceipted,
                   cancelDestinationValid, managerCancelled, paused, resumed,
                   fired, completed, restarted, compacted, repeated,
                   sampledPermission, armCommitted, armAuthorized, refused} : b \in BOOLEAN
    /\ ghost \subseteq Commands \cup {"movedCancelRefused", "terminalEditRefused",
                                      "freshAfterCancel", "allowedFreshAfterCancel"}

EveryArmAuthorized == armCommitted => armAuthorized
CancellationIsDurable == ackHome => humanReceipt
CancellationIsAtomic == ~unreceipted
TerminalDestinationStable ==
    status \in {"claimed", "done", "cancelled"} => destination = terminalDestination
CancellationKeepsSelectedTarget == cancelDestinationValid
ClaimedCancellationStopsReport ==
    (ackClaimed /\ ~HumanPermits /\ armCommitted /\ armCommand = "report") => status # "active"
DuplicateDeleteKeepsLaterAllowance ==
    (repeated /\ classifiedAllowAt > requestedCancelAt /\ requestedCancelAt > 0
     /\ ~allowPending /\ ~continuePending) => RuntimePermits

NeverCancelClaimed == ~ackClaimed
NeverCancelManagerCancelled == ~(humanReceipt /\ managerCancelled)
NeverFreshAfterCancellation == ~("allowedFreshAfterCancel" \in ghost)
NeverRepeatAfterAllowance == ~(repeated /\ classifiedAllowAt > requestedCancelAt /\ requestedCancelAt > 0)
NeverRefuseDeniedArm == ~(refused /\ requestedCancelAt > 0 /\ ~HumanPermits)
NeverRefuseMovedCancel == ~("movedCancelRefused" \in ghost)
NeverRefuseCancelStorage == cancelPhase # "failed"
NeverRefuseClaimedEdit == ~(editPhase = "refused" /\ status \in {"claimed", "done"})
NeverRefuseTerminalEdit == ~("terminalEditRefused" \in ghost)
NeverRetainAcrossRestartAndCompact == ~(humanReceipt /\ restarted /\ compacted /\ ~HumanPermits)
NeverHumanActiveWhileDenied == ~(freshActor = "human" /\ fresh = "active" /\ ~HumanPermits)
NeverReplace == ~("replace" \in ghost)
NeverReport == ~("report" \in ghost)

=============================================================================
