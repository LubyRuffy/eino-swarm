-------------------------- MODULE NuwaCollaboration --------------------------
(* A bounded collaboration registry and its project evidence stream. This is
   a protocol model, not a proof of Go atomicity. The real Registry/HTTP tests
   must establish that the code uses these same ownership boundaries. *)
EXTENDS Naturals, FiniteSets

CONSTANTS
    RebindOnRestore,
    CancelParentOnly,
    WriteAfterStop,
    CrossProjectEvidence,
    PublishBeforeCommit,
    RewindSkipsRevision,
    DeleteMovedSkipsRevision,
    MaxWrites

Workers == {"parent", "child"}
Models == {"planner", "executor"}
Chosen == [w \in Workers |-> IF w = "parent" THEN "planner" ELSE "executor"]

VARIABLES phase, live, saved, binding, written, published, lateWrite,
          foreignEvidence, restored, stopped, sourceRequired, approved

vars == <<phase, live, saved, binding, written, published, lateWrite,
          foreignEvidence, restored, stopped, sourceRequired, approved>>

Init ==
    /\ phase = "running"
    /\ live = {"parent"}
    /\ saved = {}
    /\ binding = Chosen /\ approved = Chosen
    /\ written = 0 /\ published = 0
    /\ lateWrite = FALSE /\ foreignEvidence = FALSE
    /\ restored = FALSE /\ stopped = FALSE
    /\ sourceRequired = 0

(* Descendants enter the same Registry and global budget as their parent. *)
Delegate ==
    /\ phase = "running" /\ "parent" \in live /\ "child" \notin live
    /\ live' = live \cup {"child"}
    /\ UNCHANGED <<phase, saved, binding, written, published, lateWrite,
                   foreignEvidence, restored, stopped, sourceRequired, approved>>

(* Handoff freezes tools, checkpoints every descendant, then restores exactly
   those model bindings. No model catalog/default is consulted on restoration. *)
Freeze ==
    /\ phase = "running" /\ ~restored
    /\ phase' = "frozen" /\ saved' = live /\ live' = {}
    /\ UNCHANGED <<binding, written, published, lateWrite,
                   foreignEvidence, restored, stopped, sourceRequired, approved>>

Restore ==
    /\ phase = "frozen"
    /\ phase' = "running" /\ live' = saved /\ restored' = TRUE
    /\ binding' = IF RebindOnRestore
                   THEN [w \in Workers |-> "planner"] ELSE approved
    /\ UNCHANGED <<saved, written, published, lateWrite, foreignEvidence, stopped, sourceRequired, approved>>

Stop ==
    /\ phase \in {"running", "frozen"}
    /\ phase' = "stopped" /\ stopped' = TRUE
    /\ live' = IF CancelParentOnly THEN live \ {"parent"} ELSE {}
    /\ saved' = {}
    /\ UNCHANGED <<binding, written, published, lateWrite, foreignEvidence, restored, sourceRequired, approved>>

(* The write transaction checks owner/cancellation and project references.
   A stopped/deleted owner cannot be recreated by a late callback. *)
Commit ==
    /\ written < MaxWrites
    /\ (phase = "running" \/ (WriteAfterStop /\ phase = "stopped"))
    /\ written' = written + 1
    /\ lateWrite' = (lateWrite \/ phase # "running")
    /\ foreignEvidence' = (foreignEvidence \/ CrossProjectEvidence)
    /\ published' = IF PublishBeforeCommit THEN written + 2 ELSE published
    /\ UNCHANGED <<phase, live, saved, binding, restored, stopped, sourceRequired, approved>>

(* SSE revisions invalidate a snapshot. They never publish uncommitted data;
   reconnecting readers recover from the current transactional snapshot. *)
Refresh ==
    /\ published < written /\ published' = written
    /\ UNCHANGED <<phase, live, saved, binding, written, lateWrite,
                   foreignEvidence, restored, stopped, sourceRequired, approved>>

Delete ==
    /\ phase # "deleted"
    /\ phase' = "deleted" /\ live' = {} /\ saved' = {}
    /\ UNCHANGED <<binding, written, published, lateWrite, foreignEvidence, restored, stopped, sourceRequired, approved>>

(* Mutating a visible source in this project requires a committed revision.
   For a moved author this models the original project's revision. *)
MutateSource(skip) ==
    /\ sourceRequired = 0 /\ written < MaxWrites
    /\ sourceRequired' = written + 1
    /\ written' = IF skip THEN written ELSE sourceRequired'
    /\ UNCHANGED <<phase, live, saved, binding, published, lateWrite,
                   foreignEvidence, restored, stopped, approved>>

(* RecoverWorkerModels: an explicit human action while no worker is executing.
   Only the child executor model is unavailable in this bounded instance.
   The separate durable override keeps original selection and authority visible.
   The implementation flattens repeated overrides in one SQLite transaction. *)
AuthorizeRecovery ==
    /\ phase = "frozen" /\ "child" \in saved
    /\ approved' = [approved EXCEPT !["child"] = "planner"]
    /\ UNCHANGED <<phase, live, saved, binding, written, published, lateWrite,
                   foreignEvidence, restored, stopped, sourceRequired>>

Next == AuthorizeRecovery \/ Delegate \/ Freeze \/ Restore \/ Stop \/ Commit \/ Refresh \/ Delete
        \/ MutateSource(RewindSkipsRevision) \/ MutateSource(DeleteMovedSkipsRevision)
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"running", "frozen", "stopped", "deleted"}
    /\ live \subseteq Workers /\ saved \subseteq Workers
    /\ binding \in [Workers -> Models] /\ approved \in [Workers -> Models]
    /\ written \in 0..MaxWrites /\ published \in 0..(MaxWrites + 1)
    /\ lateWrite \in BOOLEAN /\ foreignEvidence \in BOOLEAN
    /\ restored \in BOOLEAN /\ stopped \in BOOLEAN
    /\ sourceRequired \in 0..MaxWrites

BindingStable == \A w \in Workers: binding[w] \in {Chosen[w], approved[w]}
StopOwnsDescendants == stopped => live = {}
NoLateFact == ~lateWrite
ProjectScoped == ~foreignEvidence
VisibleIsCommitted == published <= written
SourceChangeHasRevision == sourceRequired <= written

NeverRestored == ~restored
NeverSharedResult == written = 0 \/ sourceRequired > 0
NeverDelegated == "child" \notin live
=============================================================================
