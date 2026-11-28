--------------------------- MODULE EngineLifecycle ---------------------------
(***************************************************************************)
(* One data directory, one conversation and one launching shell.           *)
(* Running and parked below mean the conversation's Registry, never one    *)
(* manager check.                                                          *)
(*                                                                         *)
(* The old engine is serving when the shell starts. If it is the shell's   *)
(* own build the shell attaches to it; otherwise it runs the C-027 handoff: *)
(* freeze and checkpoint the old engine, acknowledge, let it exit, spawn   *)
(* the replacement, which publishes its lease and restores the checkpoint. *)
(* Either engine may leave on idle (C-001) when nothing keeps it alive.    *)
(* LinuxHostRecovery checks the persistent supervisor and trial storage   *)
(* outside this C-027 handoff, including crash/boot and paired approval.   *)
(*                                                                         *)
(* Each Boolean constant puts back one earlier or plausible behaviour so   *)
(* the checker can show which invariant it breaks. All FALSE is the        *)
(* current design. tools/model_check.py maps every switch to its source.  *)
(*                                                                         *)
(* This is a model of the protocol, not of the Go code: an action here is  *)
(* one locked step there. The comments name the code each step stands for. *)
(***************************************************************************)
EXTENDS Naturals

(* Model selection is changed only in an exclusive, idle admission section.
   ModelModeAdmission below is the local refinement used by UpdateConfig;
   sampledIdle is deliberately not a sufficient commit predicate. *)
ModelModeAdmission(active, admissions, restoring, frozen) ==
    active = 0 /\ admissions = 0 /\ ~restoring /\ ~frozen

CONSTANTS
    DeadlineKillsChild,     \* the handoff deadline also bounds the replacement child
    RestoreBeforeListen,    \* the replacement restores before publishing its lease
    StopRacesRestore,       \* Stop is not serialized with one conversation's restoration
    HeadlessWaiterUnheld,   \* pre-170: web --no-open reports ready without a held client
    PresenceAfterWindow,    \* the shell opens its window before holding presence
    IdleCommitNonAtomic,    \* idle exit acts on an earlier sample, admission stays open
    FinishUngated,          \* handoff does not wait for an in-progress finalization
    SummaryUsesWriteBudget, \* a blocked tool consumes the time needed to save the final checkpoint
    ParkedNotCheckpointed,  \* handoff checkpoints running managers only
    HandoffIgnoresTransfer, \* a restart freezes an engine during backup or source replacement
    IgnoreHostUpdateHold,   \* idle exit ignores an admitted host-owned update
    IdleCountsOnlyManagers, \* idle load ignores parked workers, restoration and handoff
    ParkedAdmissionNonAtomic, \* choose/take drops the parked runtime before successor occupancy
    ParkedOwnerCheckedLate, \* attach retained workers before validating their project
    RestoreOwnerCheckedLate, \* occupy before checking a tagged worker's project
    SkipParkedIndexEntry,   \* a terminal manager hides its live parked checkpoint
    MaxTurns

Engines == {"old", "new"}
EngineStates == {"absent", "booting", "restoring", "serving", "frozen", "exited"}
Live == {"booting", "restoring", "serving", "frozen"}
Watched == {"restoring", "serving", "frozen"}   \* the idle watcher runs once the lease is published
WorkStates == {"none", "running", "finishing", "parked"}
LauncherStates == {"start", "handoff", "acked", "spawned", "reserved",
                   "opened", "closed", "failed", "cancelled", "relaunch"}

VARIABLES
    eng,          \* process state of each engine
    lease,        \* engine whose published lease is reachable, or "none"
    stopping,     \* idle exit committed: presence admission is closed (TryIdleShutdown)
    sampled,      \* last idle sample, used only when IdleCommitNonAtomic
    hold,         \* the shell holds a counted presence connection on target
    launcher,     \* the shell's launch phase (ensureEngineRecord / desktop presence)
    target,       \* engine of the shell's own build
    deadline,     \* the shared 30 s handoff budget has expired
    handoffPhase, \* summary wait and the final durable write in the old engine
    work,         \* the conversation's in-memory execution on owner
    owner,        \* engine executing work
    checkpoint,   \* durable restart checkpoint awaiting restoration: "none", "running", "parked"
    restorePhase, \* replacement's restoration phase, including owner refusal
    snapshot,     \* what restoration read from the checkpoint
    stopped,      \* durable human Stop for the current turn
    stopAcked,    \* the human was told the Stop succeeded
    gate,         \* normal finalization holds the finish gate (lockFinish)
    turns,        \* turns started through the window, bounded by MaxTurns
    successor,    \* last parked admission: none, refused, admitted, or privately detached
    updateHold,   \* a phone-confirmed update owns the old engine until exit or failure
    transfer      \* backup or source replacement owns the old engine

vars == <<eng, lease, stopping, sampled, hold, launcher, target, deadline, handoffPhase, work,
          owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

Alive(e) == eng[e] \in Live

(* Engine.ActiveWork: a running or finishing manager, a parked worker, an   *)
(* engine still restoring, and a handoff stage. Clients come from presence. *)
ActiveWork(e) ==
    \/ owner = e /\ work \in {"running", "finishing"}
    \/ ~IdleCountsOnlyManagers /\ owner = e /\ work = "parked"
    \/ ~IdleCountsOnlyManagers /\ eng[e] \in {"restoring", "frozen"}

Idle(e) ==
    /\ ~ActiveWork(e) /\ ~(hold /\ target = e)
    /\ IgnoreHostUpdateHold \/ e # "old" \/ ~updateHold

Init ==
    /\ eng = [e \in Engines |-> IF e = "old" THEN "serving" ELSE "absent"]
    /\ lease = "old"
    /\ stopping = [e \in Engines |-> FALSE]
    /\ sampled = [e \in Engines |-> FALSE]
    /\ hold = FALSE
    /\ launcher = "start"
    /\ target \in Engines
    /\ deadline = FALSE
    /\ handoffPhase = "initial"
    /\ work \in {"none", "running", "parked"}
    /\ owner = "old"
    /\ checkpoint = "none"
    /\ restorePhase = "pending"
    /\ snapshot = "none"
    /\ stopped = FALSE
    /\ stopAcked = FALSE
    /\ gate = FALSE
    /\ turns = 0
    /\ successor = "none"
    /\ updateHold = FALSE
    /\ transfer = FALSE

-----------------------------------------------------------------------------
(* Work on its owner. *)

StartTurn ==
    /\ launcher = "opened" /\ hold
    /\ eng[target] = "serving" /\ ~stopping[target]
    /\ work = "none" /\ checkpoint = "none" /\ turns < MaxTurns
    /\ work' = "running" /\ owner' = target
    /\ stopped' = FALSE /\ stopAcked' = FALSE /\ turns' = turns + 1
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   checkpoint, restorePhase, snapshot, gate, successor, updateHold, transfer>>

(* A retained Registry changes manager turns without losing its runtime.  *)
(* The host holds rt.mu across choosing it, recording its real owner edge, *)
(* and claiming the successor. Move uses the same runtime lock, so        *)
(* ownerMatches represents ownership at the claim boundary. The separate  *)
(* ProjectMoveAdmission.tla refinement checks interleaved Move, lookup,    *)
(* and explicit goal resume receipt without multiplying this state space. *)
(* This model does not prove SQLite atomicity.                             *)
CanStartParkedSuccessor ==
    /\ launcher = "opened" /\ hold
    /\ eng[target] = "serving" /\ ~stopping[target]
    /\ owner = target /\ work = "parked" /\ ~gate
    /\ checkpoint = "none" /\ turns < MaxTurns

StartParkedSuccessor(ownerMatches) ==
    /\ ~ParkedAdmissionNonAtomic /\ CanStartParkedSuccessor
    /\ ownerMatches \/ ParkedOwnerCheckedLate
    /\ work' = "running" /\ owner' = target
    /\ stopped' = FALSE /\ stopAcked' = FALSE /\ turns' = turns + 1
    /\ successor' = IF ownerMatches THEN "admitted" ELSE "foreign_claimed"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   checkpoint, restorePhase, snapshot, gate, updateHold, transfer>>

(* A foreign owner or failed lineage write leaves the original work intact. *)
RefuseParkedSuccessor(ownerMatches) ==
    /\ CanStartParkedSuccessor
    /\ successor' = IF ownerMatches THEN "refused" ELSE "owner_refused"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, restorePhase, snapshot, stopped,
                   stopAcked, gate, turns, updateHold, transfer>>

(* Seed the split choose/take -> occupy design. The Registry still contains *)
(* owned workers, but ActiveWork can no longer see them in the runtime.    *)
DetachParkedSuccessor ==
    /\ ParkedAdmissionNonAtomic /\ CanStartParkedSuccessor
    /\ work' = "none" /\ successor' = "detached"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, stopped,
                   stopAcked, gate, turns, updateHold, transfer>>

(* Finalization releases the runtime, then persists the terminal receipt or *)
(* parks the workers. lockFinish holds the gate across both steps.          *)
BeginFinish ==
    /\ work = "running" /\ eng[owner] = "serving"
    /\ FinishUngated \/ ~gate
    /\ work' = "finishing" /\ gate' = ~FinishUngated
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, turns, successor, updateHold, transfer>>

EndFinish ==
    /\ work = "finishing" /\ Alive(owner)
    /\ work' \in {"none", "parked"} /\ gate' = FALSE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, turns, successor, updateHold, transfer>>

WorkerDone ==
    /\ work = "parked" /\ eng[owner] = "serving"
    /\ work' = "none"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* interruptThread on a live runtime: a running manager or parked workers. *)
(* StopLive abstracts normal terminalization. The concrete engine pairs any *)
(* unresolved admitted Ask with its original call before the turn terminal. *)
(* Handoff is a different transition and preserves an unanswered pause.    *)
StopLive ==
    /\ eng[owner] = "serving" /\ lease = owner
    /\ work \in {"running", "parked"} /\ ~stopped
    /\ work' = "none" /\ stopped' = TRUE /\ stopAcked' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, gate, turns, successor, updateHold, transfer>>

(* stopBeforeRestore: no runtime yet, the Stop is recorded durably. restoreMu *)
(* keeps it out of the window between reading and occupying a checkpoint.   *)
StopBeforeRestore ==
    /\ eng["new"] = "restoring" /\ lease = "new"
    /\ checkpoint # "none" /\ ~stopped
    /\ restorePhase = "pending" \/ (StopRacesRestore /\ restorePhase = "read")
    /\ stopped' = TRUE /\ stopAcked' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, restorePhase, snapshot, gate, turns, successor, updateHold, transfer>>

-----------------------------------------------------------------------------
(* C-027 handoff, driven by the shell of the new build. *)

BeginHandoff ==
    /\ launcher = "start" /\ target = "new" /\ lease = "old"
    /\ launcher' = "handoff"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* PrepareRestart: lockRestartFinishes under the shared deadline, then a    *)
(* baseline checkpoint covering active and parked registries, then cancel.  *)
Freeze ==
    LET captured == owner = "old" /\
                    (work = "running" \/ (work = "parked" /\ ~ParkedNotCheckpointed))
    IN
    /\ launcher = "handoff" /\ eng["old"] = "serving" /\ ~deadline
    /\ ~transfer \/ HandoffIgnoresTransfer
    /\ FinishUngated \/ ~gate
    /\ eng' = [eng EXCEPT !["old"] = "frozen"]
    /\ checkpoint' = IF captured THEN work ELSE checkpoint
    /\ handoffPhase' = IF captured THEN "summarizing" ELSE "checkpointing"
    /\ work' = IF captured THEN "none" ELSE work
    /\ UNCHANGED <<lease, stopping, sampled, hold, launcher, target, deadline, owner,
                   restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* The shared summary window either settles normally or expires with an    *)
(* unknown tool. The current cutoff leaves a separate opportunity to save. *)
(* The fault abstracts the observed case where the old reserve was too     *)
(* small for final persistence. Storage can independently fail or time out. *)
EndSummary(expired) ==
    /\ launcher = "handoff" /\ eng["old"] = "frozen" /\ ~deadline
    /\ handoffPhase = "summarizing"
    /\ handoffPhase' = IF ~expired THEN "checkpointing"
                       ELSE IF SummaryUsesWriteBudget THEN "summary_exhausted"
                       ELSE "checkpointing_unknown"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline, work,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

SaveFinalCheckpoint ==
    /\ launcher = "handoff" /\ eng["old"] = "frozen" /\ ~deadline
    /\ handoffPhase \in {"checkpointing", "checkpointing_unknown"}
    /\ handoffPhase' = IF handoffPhase = "checkpointing" THEN "saved" ELSE "saved_unknown"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline, work,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

Ack ==
    /\ launcher = "handoff" /\ eng["old"] = "frozen" /\ ~deadline
    /\ handoffPhase \in {"saved", "saved_unknown"}
    /\ launcher' = "acked"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* replaceEngine failed but discovery finds no holder: continue to launch. *)
OldAlreadyGone ==
    /\ launcher = "handoff" /\ eng["old"] = "exited"
    /\ launcher' = "acked"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* Before ACK an expired budget is an error that leaves the old engine as is. *)
HandoffTimesOut ==
    /\ launcher = "handoff" /\ (deadline \/ handoffPhase = "summary_exhausted")
    /\ launcher' = "failed"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

OldExits ==
    /\ launcher = "acked" /\ eng["old"] = "frozen"
    /\ eng' = [eng EXCEPT !["old"] = "exited"]
    /\ lease' = IF lease = "old" THEN "none" ELSE lease
    /\ updateHold' = FALSE
    /\ UNCHANGED <<stopping, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, transfer>>

Spawn ==
    /\ launcher = "acked" /\ eng["old"] = "exited" /\ eng["new"] = "absent"
    /\ eng' = [eng EXCEPT !["new"] = "booting"]
    /\ launcher' = "spawned"
    /\ UNCHANGED <<lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

DeadlineExpires ==
    /\ launcher \in {"handoff", "acked", "spawned"} /\ ~deadline
    /\ deadline' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* spawnEngineContext killing its child when the launch context expires. *)
DeadlineKillsReplacement ==
    /\ DeadlineKillsChild /\ deadline
    /\ launcher = "spawned" /\ eng["new"] = "booting"
    /\ eng' = [eng EXCEPT !["new"] = "exited"]
    /\ launcher' = "failed"
    /\ UNCHANGED <<lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* An explicit human cancellation may always reap the launcher's own child. *)
HumanCancelsLaunch ==
    /\ launcher = "spawned" /\ eng["new"] = "booting"
    /\ eng' = [eng EXCEPT !["new"] = "exited"]
    /\ launcher' = "cancelled"
    /\ UNCHANGED <<lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

-----------------------------------------------------------------------------
(* The replacement publishes and restores (serveEngine, Engine.Restore).
   While restoration is in its read phase, ResumeModelBuilder may await the
   owned account discovery loop before occupying a runtime. The engine stays
   restoring and counted as ActiveWork; Stop and cancellation keep this gate.
   ModelModeAdmission refines catalog readiness and original model identity. *)

RestoringHere == IF RestoreBeforeListen THEN "booting" ELSE "restoring"

Publish ==
    /\ eng["new"] = "booting"
    /\ ~RestoreBeforeListen \/ restorePhase \in {"done", "owner_refused", "foreign_claimed", "panel_refused"}
    /\ eng' = [eng EXCEPT !["new"] = IF RestoreBeforeListen THEN "serving" ELSE "restoring"]
    /\ lease' = "new"
    /\ UNCHANGED <<stopping, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

ReadCheckpoint ==
    /\ eng["new"] = RestoringHere /\ restorePhase = "pending"
    /\ checkpoint # "none"
    /\ IF stopped THEN snapshot' = "none"
       ELSE snapshot' \in (IF checkpoint = "none" THEN {"none"} ELSE {checkpoint, "foreign"})
    /\ restorePhase' = "read"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* Startup can publish ready directly only with no outstanding indexed work.
   The fault models an index that clears a parked worker with its manager. *)
SkipSettledRecovery ==
    /\ eng["new"] = RestoringHere /\ restorePhase = "pending"
    /\ checkpoint = "none" \/ (SkipParkedIndexEntry /\ checkpoint = "parked" /\ ~stopped)
    /\ restorePhase' = "done"
    /\ eng' = IF RestoreBeforeListen THEN eng ELSE [eng EXCEPT !["new"] = "serving"]
    /\ UNCHANGED <<lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* Engine.Restore first settles this conversation's orphaned panel sources
   under restoreMu. A storage/identity failure refuses this conversation before
   occupying its runtime. Its durable input is retained; PanelOwnership checks
   that the outer recovery pass still visits independent conversations. *)
RefusePanelRecovery ==
    /\ eng["new"] = RestoringHere /\ restorePhase = "pending"
    /\ checkpoint' = "none" /\ snapshot' = "none" /\ restorePhase' = "panel_refused"
    /\ eng' = IF RestoreBeforeListen THEN eng ELSE [eng EXCEPT !["new"] = "serving"]
    /\ UNCHANGED <<lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

OccupyRuntime ==
    /\ eng["new"] = RestoringHere /\ restorePhase = "read"
    /\ snapshot # "foreign" \/ RestoreOwnerCheckedLate
    /\ work' = IF snapshot = "foreign" THEN checkpoint ELSE snapshot
    /\ owner' = IF snapshot = "none" THEN owner ELSE "new"
    /\ checkpoint' = "none" /\ snapshot' = "none"
    /\ restorePhase' = IF snapshot = "foreign" THEN "foreign_claimed" ELSE "done"
    /\ eng' = IF RestoreBeforeListen THEN eng ELSE [eng EXCEPT !["new"] = "serving"]
    /\ UNCHANGED <<lease, stopping, sampled, hold, launcher, target, deadline,
                   stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* Complete this handoff attempt without occupying the runtime. The handoff
   obligation is cleared here; the database retains the recovery input. *)
RefuseForeignRuntime ==
    /\ eng["new"] = RestoringHere /\ restorePhase = "read" /\ snapshot = "foreign"
    /\ checkpoint' = "none" /\ snapshot' = "none" /\ restorePhase' = "owner_refused"
    /\ eng' = IF RestoreBeforeListen THEN eng ELSE [eng EXCEPT !["new"] = "serving"]
    /\ UNCHANGED <<lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

-----------------------------------------------------------------------------
(* Shell presence: desktop window or headless web waiter ready. *)

(* serveWeb's old ready callback and signal wait did not hold any presence. *)
UnheldWebReady ==
    /\ HeadlessWaiterUnheld /\ launcher = "start" /\ target = "old"
    /\ lease = target /\ Alive(target) /\ ~stopping[target]
    /\ launcher' = "opened"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

Reserve ==
    /\ \/ launcher = "start" /\ target = "old"
       \/ launcher = "spawned"
    /\ lease = target /\ Alive(target)
    /\ launcher' = IF PresenceAfterWindow THEN "opened"
                   ELSE IF stopping[target] THEN "relaunch"
                   ELSE "reserved"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

(* presence.attach: refused once idle exit committed or the engine left. *)
Attach ==
    /\ launcher = "reserved"
    /\ IF Alive(target) /\ ~stopping[target]
       THEN hold' = TRUE /\ launcher' = "opened"
       ELSE hold' = hold /\ launcher' = "relaunch"
    /\ UNCHANGED <<eng, lease, stopping, sampled, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

FrontendAttaches ==
    /\ PresenceAfterWindow /\ launcher = "opened" /\ ~hold
    /\ Alive(target) /\ ~stopping[target]
    /\ hold' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

WindowCloses ==
    /\ launcher = "opened" /\ hold
    /\ hold' = FALSE /\ launcher' = "closed"
    /\ UNCHANGED <<eng, lease, stopping, sampled, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

-----------------------------------------------------------------------------
(* Idle exit (lease.Watcher with Server.TryIdleShutdown). *)

(* A confirmed host update reserves a private hold under the idle admission
   lock. A link dropping has no transition on this hold. On handoff it releases
   with the old engine, and pre-handoff failures release it explicitly. *)
BeginHostUpdate ==
    /\ eng["old"] = "serving" /\ ~stopping["old"] /\ ~updateHold
    /\ updateHold' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, restorePhase, snapshot, stopped,
                   stopAcked, gate, turns, successor, transfer>>

ReleaseHostUpdate ==
    /\ updateHold
    /\ updateHold' = FALSE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, restorePhase, snapshot, stopped,
                   stopAcked, gate, turns, successor, transfer>>


(* A backup or provider source switch can begin while the package is still
   downloading. It and Freeze must arbitrate under the same engine lock. *)
BeginTransfer ==
    /\ updateHold /\ eng["old"] = "serving" /\ ~transfer
    /\ transfer' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, restorePhase, snapshot, stopped,
                   stopAcked, gate, turns, successor, updateHold>>

EndTransfer ==
    /\ transfer
    /\ transfer' = FALSE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, restorePhase, snapshot, stopped,
                   stopAcked, gate, turns, successor, updateHold>>

IdleCommit(e) ==
    /\ ~IdleCommitNonAtomic
    /\ eng[e] \in Watched /\ ~stopping[e] /\ Idle(e)
    /\ stopping' = [stopping EXCEPT ![e] = TRUE]
    /\ UNCHANGED <<eng, lease, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

IdleSample(e) ==
    /\ IdleCommitNonAtomic
    /\ eng[e] \in Watched
    /\ sampled' = [sampled EXCEPT ![e] = Idle(e)]
    /\ UNCHANGED <<eng, lease, stopping, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

IdleExit(e) ==
    /\ eng[e] \in Watched
    /\ IF IdleCommitNonAtomic THEN sampled[e] ELSE stopping[e]
    /\ eng' = [eng EXCEPT ![e] = "exited"]
    /\ lease' = IF lease = e THEN "none" ELSE lease
    /\ UNCHANGED <<stopping, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns, successor, updateHold, transfer>>

-----------------------------------------------------------------------------

LifecycleStep ==
    \/ StartTurn \/ DetachParkedSuccessor
    \/ \E ownerMatches \in BOOLEAN : StartParkedSuccessor(ownerMatches) \/ RefuseParkedSuccessor(ownerMatches)
    \/ BeginFinish \/ EndFinish \/ WorkerDone
    \/ StopLive \/ StopBeforeRestore
    \/ BeginHandoff \/ Ack \/ OldAlreadyGone \/ HandoffTimesOut
    \/ OldExits \/ Spawn \/ DeadlineExpires \/ DeadlineKillsReplacement
    \/ HumanCancelsLaunch
    \/ Publish \/ SkipSettledRecovery \/ ReadCheckpoint \/ OccupyRuntime \/ RefuseForeignRuntime \/ RefusePanelRecovery
    \/ UnheldWebReady \/ Reserve \/ Attach \/ FrontendAttaches \/ WindowCloses
    \/ BeginHostUpdate \/ ReleaseHostUpdate \/ BeginTransfer \/ EndTransfer
    \/ \E e \in Engines : IdleCommit(e) \/ IdleSample(e) \/ IdleExit(e)

Next ==
    \/ Freeze \/ EndSummary(FALSE) \/ EndSummary(TRUE) \/ SaveFinalCheckpoint
    \/ LifecycleStep /\ UNCHANGED handoffPhase

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Safety. *)

TypeOK ==
    /\ eng \in [Engines -> EngineStates]
    /\ lease \in Engines \cup {"none"}
    /\ stopping \in [Engines -> BOOLEAN]
    /\ sampled \in [Engines -> BOOLEAN]
    /\ hold \in BOOLEAN
    /\ launcher \in LauncherStates
    /\ target \in Engines
    /\ deadline \in BOOLEAN
    /\ handoffPhase \in {"initial", "summarizing", "checkpointing", "checkpointing_unknown",
                         "saved", "saved_unknown", "summary_exhausted"}
    /\ work \in WorkStates
    /\ owner \in Engines
    /\ checkpoint \in {"none", "running", "parked"}
    /\ restorePhase \in {"pending", "read", "done", "owner_refused", "foreign_claimed", "panel_refused"}
    /\ snapshot \in {"none", "running", "parked", "foreign"}
    /\ stopped \in BOOLEAN
    /\ stopAcked \in BOOLEAN
    /\ gate \in BOOLEAN
    /\ turns \in 0..MaxTurns
    /\ successor \in {"none", "refused", "admitted", "detached", "owner_refused", "foreign_claimed"}
    /\ updateHold \in BOOLEAN
    /\ transfer \in BOOLEAN

(* A manager, a finalization or a parked worker never outlives its process. *)
WorkHasLiveOwner == work # "none" => Alive(owner)

(* An unstopped checkpoint always has an engine that will restore it, except *)
(* while the shell waits to spawn the replacement or after a human cancel.  *)
CheckpointHasOwner ==
    (checkpoint # "none" /\ ~stopped /\ launcher \notin {"cancelled", "acked"})
        => (Alive("old") \/ Alive("new"))

(* An acknowledged Stop is never undone by restoration. *)
StopIsFinal == stopAcked => work = "none"

ReadyHasNoPendingCheckpoint ==
    (eng["new"] = "serving" /\ restorePhase = "done") => checkpoint = "none"

(* A privately held parked Registry must still be counted by its runtime. *)
RegistryAlwaysAccounted == successor = "detached" => work # "none"

RestoreOwnsProject == restorePhase # "foreign_claimed"
ParkedSuccessorOwnsProject == successor # "foreign_claimed"

(* A ready window or headless waiter has an engine until its owner closes. *)
WindowHasEngine == launcher = "opened" => Alive(target)

HandoffRespectsTransfer == transfer => eng["old"] # "frozen"

HostUpdateKeepsEngine == updateHold => (Alive("old") /\ ~stopping["old"])

SummaryLeavesWriteBudget == handoffPhase # "summary_exhausted"

-----------------------------------------------------------------------------
(* Reachability witnesses: each must be violated by the current design, or   *)
(* the invariants above would hold only because the scenario never happens. *)

NeverAdmittedHostUpdate == ~updateHold

NeverRestoredIntoWindow ==
    ~(launcher = "opened" /\ target = "new" /\ owner = "new" /\ work # "none")
NeverParkedCheckpoint == ~(checkpoint = "parked")
NeverReadyWithoutRecovery == ~(eng["new"] = "serving" /\ restorePhase = "done" /\ owner = "old")
NeverRefusedForeignRuntime == restorePhase # "owner_refused"
NeverRefusedPanelRecovery == restorePhase # "panel_refused"
NeverParkedSuccessor == ~(successor = "admitted" /\ work = "running" /\ turns > 0)
NeverRefusedParkedSuccessor == ~(successor = "refused" /\ work = "parked")
NeverRefusedForeignParkedSuccessor == ~(successor = "owner_refused" /\ work = "parked")
NeverStopBeforeRestore == ~(stopAcked /\ checkpoint # "none")
NeverOpenedAfterDeadline == ~(deadline /\ launcher = "opened" /\ eng["new"] = "serving")
NeverIdleExitAfterClose == ~(launcher = "closed" /\ eng[target] = "exited")
NeverRelaunchAfterIdleCommit == ~(launcher = "relaunch" /\ stopping[target])
NeverFinishWaitsForHandoff == ~(launcher = "handoff" /\ gate /\ work = "finishing")
NeverAckWithUnknownTool == ~(launcher = "acked" /\ handoffPhase = "saved_unknown")

=============================================================================
