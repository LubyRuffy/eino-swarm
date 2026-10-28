--------------------------- MODULE EngineLifecycle ---------------------------
(***************************************************************************)
(* One data directory, one conversation and one launching shell.           *)
(*                                                                         *)
(* The old engine is serving when the shell starts. If it is the shell's   *)
(* own build the shell attaches to it; otherwise it runs the C-027 handoff: *)
(* freeze and checkpoint the old engine, acknowledge, let it exit, spawn   *)
(* the replacement, which publishes its lease and restores the checkpoint. *)
(* Either engine may leave on idle (C-001) when nothing keeps it alive.    *)
(*                                                                         *)
(* Each Boolean constant puts back one earlier or plausible behaviour so   *)
(* the checker can show which invariant it breaks. All FALSE is the        *)
(* current design. tools/model_check.py maps every switch to its source.  *)
(*                                                                         *)
(* This is a model of the protocol, not of the Go code: an action here is  *)
(* one locked step there. The comments name the code each step stands for. *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
    DeadlineKillsChild,     \* the handoff deadline also bounds the replacement child
    RestoreBeforeListen,    \* the replacement restores before publishing its lease
    StopRacesRestore,       \* Stop is not serialized with one conversation's restoration
    PresenceAfterWindow,    \* the shell opens its window before holding presence
    IdleCommitNonAtomic,    \* idle exit acts on an earlier sample, admission stays open
    FinishUngated,          \* handoff does not wait for an in-progress finalization
    ParkedNotCheckpointed,  \* handoff checkpoints running managers only
    IdleCountsOnlyManagers, \* idle load ignores parked workers, restoration and handoff
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
    work,         \* the conversation's in-memory execution on owner
    owner,        \* engine executing work
    checkpoint,   \* durable restart checkpoint awaiting restoration: "none", "running", "parked"
    restorePhase, \* replacement's restoration of this conversation: "pending", "read", "done"
    snapshot,     \* what restoration read from the checkpoint
    stopped,      \* durable human Stop for the current turn
    stopAcked,    \* the human was told the Stop succeeded
    gate,         \* normal finalization holds the finish gate (lockFinish)
    turns         \* turns started through the window, bounded by MaxTurns

vars == <<eng, lease, stopping, sampled, hold, launcher, target, deadline, work,
          owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

Alive(e) == eng[e] \in Live

(* Engine.ActiveWork: a running or finishing manager, a parked worker, an   *)
(* engine still restoring, and a handoff stage. Clients come from presence. *)
ActiveWork(e) ==
    \/ owner = e /\ work \in {"running", "finishing"}
    \/ ~IdleCountsOnlyManagers /\ owner = e /\ work = "parked"
    \/ ~IdleCountsOnlyManagers /\ eng[e] \in {"restoring", "frozen"}

Idle(e) == ~ActiveWork(e) /\ ~(hold /\ target = e)

Init ==
    /\ eng = [e \in Engines |-> IF e = "old" THEN "serving" ELSE "absent"]
    /\ lease = "old"
    /\ stopping = [e \in Engines |-> FALSE]
    /\ sampled = [e \in Engines |-> FALSE]
    /\ hold = FALSE
    /\ launcher = "start"
    /\ target \in Engines
    /\ deadline = FALSE
    /\ work \in {"none", "running", "parked"}
    /\ owner = "old"
    /\ checkpoint = "none"
    /\ restorePhase = "pending"
    /\ snapshot = "none"
    /\ stopped = FALSE
    /\ stopAcked = FALSE
    /\ gate = FALSE
    /\ turns = 0

-----------------------------------------------------------------------------
(* Work on its owner. *)

StartTurn ==
    /\ launcher = "opened" /\ hold
    /\ eng[target] = "serving" /\ ~stopping[target]
    /\ work = "none" /\ checkpoint = "none" /\ turns < MaxTurns
    /\ work' = "running" /\ owner' = target
    /\ stopped' = FALSE /\ stopAcked' = FALSE /\ turns' = turns + 1
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   checkpoint, restorePhase, snapshot, gate>>

(* Finalization releases the runtime, then persists the terminal receipt or *)
(* parks the workers. lockFinish holds the gate across both steps.          *)
BeginFinish ==
    /\ work = "running" /\ eng[owner] = "serving"
    /\ FinishUngated \/ ~gate
    /\ work' = "finishing" /\ gate' = ~FinishUngated
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, turns>>

EndFinish ==
    /\ work = "finishing" /\ Alive(owner)
    /\ work' \in {"none", "parked"} /\ gate' = FALSE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, turns>>

WorkerDone ==
    /\ work = "parked" /\ eng[owner] = "serving"
    /\ work' = "none"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

(* interruptThread on a live runtime: a running manager or parked workers. *)
StopLive ==
    /\ eng[owner] = "serving" /\ lease = owner
    /\ work \in {"running", "parked"} /\ ~stopped
    /\ work' = "none" /\ stopped' = TRUE /\ stopAcked' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   owner, checkpoint, restorePhase, snapshot, gate, turns>>

(* stopBeforeRestore: no runtime yet, the Stop is recorded durably. restoreMu *)
(* keeps it out of the window between reading and occupying a checkpoint.   *)
StopBeforeRestore ==
    /\ eng["new"] = "restoring" /\ lease = "new"
    /\ checkpoint # "none" /\ ~stopped
    /\ restorePhase = "pending" \/ (StopRacesRestore /\ restorePhase = "read")
    /\ stopped' = TRUE /\ stopAcked' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, restorePhase, snapshot, gate, turns>>

-----------------------------------------------------------------------------
(* C-027 handoff, driven by the shell of the new build. *)

BeginHandoff ==
    /\ launcher = "start" /\ target = "new" /\ lease = "old"
    /\ launcher' = "handoff"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

(* PrepareRestart: lockRestartFinishes under the shared deadline, then one    *)
(* checkpoint covering active and parked registries, then cancel them.      *)
Freeze ==
    LET captured == owner = "old" /\
                    (work = "running" \/ (work = "parked" /\ ~ParkedNotCheckpointed))
    IN
    /\ launcher = "handoff" /\ eng["old"] = "serving" /\ ~deadline
    /\ FinishUngated \/ ~gate
    /\ eng' = [eng EXCEPT !["old"] = "frozen"]
    /\ checkpoint' = IF captured THEN work ELSE checkpoint
    /\ work' = IF captured THEN "none" ELSE work
    /\ UNCHANGED <<lease, stopping, sampled, hold, launcher, target, deadline, owner,
                   restorePhase, snapshot, stopped, stopAcked, gate, turns>>

Ack ==
    /\ launcher = "handoff" /\ eng["old"] = "frozen" /\ ~deadline
    /\ launcher' = "acked"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

(* replaceEngine failed but discovery finds no holder: continue to launch. *)
OldAlreadyGone ==
    /\ launcher = "handoff" /\ eng["old"] = "exited"
    /\ launcher' = "acked"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

(* Before ACK an expired budget is an error that leaves the old engine as is. *)
HandoffTimesOut ==
    /\ launcher = "handoff" /\ deadline
    /\ launcher' = "failed"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

OldExits ==
    /\ launcher = "acked" /\ eng["old"] = "frozen"
    /\ eng' = [eng EXCEPT !["old"] = "exited"]
    /\ lease' = IF lease = "old" THEN "none" ELSE lease
    /\ UNCHANGED <<stopping, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

Spawn ==
    /\ launcher = "acked" /\ eng["old"] = "exited" /\ eng["new"] = "absent"
    /\ eng' = [eng EXCEPT !["new"] = "booting"]
    /\ launcher' = "spawned"
    /\ UNCHANGED <<lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

DeadlineExpires ==
    /\ launcher \in {"handoff", "acked", "spawned"} /\ ~deadline
    /\ deadline' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

(* spawnEngineContext killing its child when the launch context expires. *)
DeadlineKillsReplacement ==
    /\ DeadlineKillsChild /\ deadline
    /\ launcher = "spawned" /\ eng["new"] = "booting"
    /\ eng' = [eng EXCEPT !["new"] = "exited"]
    /\ launcher' = "failed"
    /\ UNCHANGED <<lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

(* An explicit human cancellation may always reap the launcher's own child. *)
HumanCancelsLaunch ==
    /\ launcher = "spawned" /\ eng["new"] = "booting"
    /\ eng' = [eng EXCEPT !["new"] = "exited"]
    /\ launcher' = "cancelled"
    /\ UNCHANGED <<lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

-----------------------------------------------------------------------------
(* The replacement publishes and restores (serveEngine, Engine.Restore). *)

RestoringHere == IF RestoreBeforeListen THEN "booting" ELSE "restoring"

Publish ==
    /\ eng["new"] = "booting"
    /\ ~RestoreBeforeListen \/ restorePhase = "done"
    /\ eng' = [eng EXCEPT !["new"] = IF RestoreBeforeListen THEN "serving" ELSE "restoring"]
    /\ lease' = "new"
    /\ UNCHANGED <<stopping, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

ReadCheckpoint ==
    /\ eng["new"] = RestoringHere /\ restorePhase = "pending"
    /\ snapshot' = IF stopped THEN "none" ELSE checkpoint
    /\ restorePhase' = "read"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, launcher, target, deadline,
                   work, owner, checkpoint, stopped, stopAcked, gate, turns>>

OccupyRuntime ==
    /\ eng["new"] = RestoringHere /\ restorePhase = "read"
    /\ work' = snapshot
    /\ owner' = IF snapshot = "none" THEN owner ELSE "new"
    /\ checkpoint' = "none" /\ snapshot' = "none" /\ restorePhase' = "done"
    /\ eng' = IF RestoreBeforeListen THEN eng ELSE [eng EXCEPT !["new"] = "serving"]
    /\ UNCHANGED <<lease, stopping, sampled, hold, launcher, target, deadline,
                   stopped, stopAcked, gate, turns>>

-----------------------------------------------------------------------------
(* Shell presence (desktop launch and the presence endpoints). *)

Reserve ==
    /\ \/ launcher = "start" /\ target = "old"
       \/ launcher = "spawned"
    /\ lease = target /\ Alive(target)
    /\ launcher' = IF PresenceAfterWindow THEN "opened"
                   ELSE IF stopping[target] THEN "relaunch"
                   ELSE "reserved"
    /\ UNCHANGED <<eng, lease, stopping, sampled, hold, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

(* presence.attach: refused once idle exit committed or the engine left. *)
Attach ==
    /\ launcher = "reserved"
    /\ IF Alive(target) /\ ~stopping[target]
       THEN hold' = TRUE /\ launcher' = "opened"
       ELSE hold' = hold /\ launcher' = "relaunch"
    /\ UNCHANGED <<eng, lease, stopping, sampled, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

FrontendAttaches ==
    /\ PresenceAfterWindow /\ launcher = "opened" /\ ~hold
    /\ Alive(target) /\ ~stopping[target]
    /\ hold' = TRUE
    /\ UNCHANGED <<eng, lease, stopping, sampled, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

WindowCloses ==
    /\ launcher = "opened" /\ hold
    /\ hold' = FALSE /\ launcher' = "closed"
    /\ UNCHANGED <<eng, lease, stopping, sampled, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

-----------------------------------------------------------------------------
(* Idle exit (lease.Watcher with Server.TryIdleShutdown). *)

IdleCommit(e) ==
    /\ ~IdleCommitNonAtomic
    /\ eng[e] \in Watched /\ ~stopping[e] /\ Idle(e)
    /\ stopping' = [stopping EXCEPT ![e] = TRUE]
    /\ UNCHANGED <<eng, lease, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

IdleSample(e) ==
    /\ IdleCommitNonAtomic
    /\ eng[e] \in Watched
    /\ sampled' = [sampled EXCEPT ![e] = Idle(e)]
    /\ UNCHANGED <<eng, lease, stopping, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

IdleExit(e) ==
    /\ eng[e] \in Watched
    /\ IF IdleCommitNonAtomic THEN sampled[e] ELSE stopping[e]
    /\ eng' = [eng EXCEPT ![e] = "exited"]
    /\ lease' = IF lease = e THEN "none" ELSE lease
    /\ UNCHANGED <<stopping, sampled, hold, launcher, target, deadline, work, owner,
                   checkpoint, restorePhase, snapshot, stopped, stopAcked, gate, turns>>

-----------------------------------------------------------------------------

Next ==
    \/ StartTurn \/ BeginFinish \/ EndFinish \/ WorkerDone
    \/ StopLive \/ StopBeforeRestore
    \/ BeginHandoff \/ Freeze \/ Ack \/ OldAlreadyGone \/ HandoffTimesOut
    \/ OldExits \/ Spawn \/ DeadlineExpires \/ DeadlineKillsReplacement
    \/ HumanCancelsLaunch
    \/ Publish \/ ReadCheckpoint \/ OccupyRuntime
    \/ Reserve \/ Attach \/ FrontendAttaches \/ WindowCloses
    \/ \E e \in Engines : IdleCommit(e) \/ IdleSample(e) \/ IdleExit(e)

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
    /\ work \in WorkStates
    /\ owner \in Engines
    /\ checkpoint \in {"none", "running", "parked"}
    /\ restorePhase \in {"pending", "read", "done"}
    /\ snapshot \in {"none", "running", "parked"}
    /\ stopped \in BOOLEAN
    /\ stopAcked \in BOOLEAN
    /\ gate \in BOOLEAN
    /\ turns \in 0..MaxTurns

(* A manager, a finalization or a parked worker never outlives its process. *)
WorkHasLiveOwner == work # "none" => Alive(owner)

(* An unstopped checkpoint always has an engine that will restore it, except *)
(* while the shell waits to spawn the replacement or after a human cancel.  *)
CheckpointHasOwner ==
    (checkpoint # "none" /\ ~stopped /\ launcher \notin {"cancelled", "acked"})
        => (Alive("old") \/ Alive("new"))

(* An acknowledged Stop is never undone by restoration. *)
StopIsFinal == stopAcked => work = "none"

(* A window the shell opened has an engine behind it until the window closes. *)
WindowHasEngine == launcher = "opened" => Alive(target)

-----------------------------------------------------------------------------
(* Reachability witnesses: each must be violated by the current design, or   *)
(* the invariants above would hold only because the scenario never happens. *)

NeverRestoredIntoWindow ==
    ~(launcher = "opened" /\ target = "new" /\ owner = "new" /\ work # "none")
NeverParkedCheckpoint == ~(checkpoint = "parked")
NeverStopBeforeRestore == ~(stopAcked /\ checkpoint # "none")
NeverOpenedAfterDeadline == ~(deadline /\ launcher = "opened" /\ eng["new"] = "serving")
NeverIdleExitAfterClose == ~(launcher = "closed" /\ eng[target] = "exited")
NeverRelaunchAfterIdleCommit == ~(launcher = "relaunch" /\ stopping[target])
NeverFinishWaitsForHandoff == ~(launcher = "handoff" /\ gate /\ work = "finishing")

=============================================================================
