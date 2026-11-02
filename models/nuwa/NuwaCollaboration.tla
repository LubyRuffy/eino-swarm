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
    MaxWrites

Workers == {"parent", "child"}
Models == {"planner", "executor"}
Chosen == [w \in Workers |-> IF w = "parent" THEN "planner" ELSE "executor"]

VARIABLES phase, live, saved, binding, written, published, lateWrite,
          foreignEvidence, restored, stopped

vars == <<phase, live, saved, binding, written, published, lateWrite,
          foreignEvidence, restored, stopped>>

Init ==
    /\ phase = "running"
    /\ live = {"parent"}
    /\ saved = {}
    /\ binding = Chosen
    /\ written = 0 /\ published = 0
    /\ lateWrite = FALSE /\ foreignEvidence = FALSE
    /\ restored = FALSE /\ stopped = FALSE

(* Descendants enter the same Registry and global budget as their parent. *)
Delegate ==
    /\ phase = "running" /\ "parent" \in live /\ "child" \notin live
    /\ live' = live \cup {"child"}
    /\ UNCHANGED <<phase, saved, binding, written, published, lateWrite,
                   foreignEvidence, restored, stopped>>

(* Handoff freezes tools, checkpoints every descendant, then restores exactly
   those model bindings. No model catalog/default is consulted on restoration. *)
Freeze ==
    /\ phase = "running" /\ ~restored
    /\ phase' = "frozen" /\ saved' = live /\ live' = {}
    /\ UNCHANGED <<binding, written, published, lateWrite,
                   foreignEvidence, restored, stopped>>

Restore ==
    /\ phase = "frozen"
    /\ phase' = "running" /\ live' = saved /\ restored' = TRUE
    /\ binding' = IF RebindOnRestore
                   THEN [w \in Workers |-> "planner"] ELSE binding
    /\ UNCHANGED <<saved, written, published, lateWrite, foreignEvidence, stopped>>

Stop ==
    /\ phase \in {"running", "frozen"}
    /\ phase' = "stopped" /\ stopped' = TRUE
    /\ live' = IF CancelParentOnly THEN live \ {"parent"} ELSE {}
    /\ saved' = {}
    /\ UNCHANGED <<binding, written, published, lateWrite, foreignEvidence, restored>>

(* The write transaction checks owner/cancellation and project references.
   A stopped/deleted owner cannot be recreated by a late callback. *)
Commit ==
    /\ written < MaxWrites
    /\ (phase = "running" \/ (WriteAfterStop /\ phase = "stopped"))
    /\ written' = written + 1
    /\ lateWrite' = (lateWrite \/ phase # "running")
    /\ foreignEvidence' = (foreignEvidence \/ CrossProjectEvidence)
    /\ published' = IF PublishBeforeCommit THEN written + 2 ELSE published
    /\ UNCHANGED <<phase, live, saved, binding, restored, stopped>>

(* SSE revisions invalidate a snapshot. They never publish uncommitted data;
   reconnecting readers recover from the current transactional snapshot. *)
Refresh ==
    /\ published < written /\ published' = written
    /\ UNCHANGED <<phase, live, saved, binding, written, lateWrite,
                   foreignEvidence, restored, stopped>>

Delete ==
    /\ phase # "deleted"
    /\ phase' = "deleted" /\ live' = {} /\ saved' = {}
    /\ UNCHANGED <<binding, written, published, lateWrite, foreignEvidence, restored, stopped>>

Next == Delegate \/ Freeze \/ Restore \/ Stop \/ Commit \/ Refresh \/ Delete
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ phase \in {"running", "frozen", "stopped", "deleted"}
    /\ live \subseteq Workers /\ saved \subseteq Workers
    /\ binding \in [Workers -> Models]
    /\ written \in 0..MaxWrites /\ published \in 0..(MaxWrites + 1)
    /\ lateWrite \in BOOLEAN /\ foreignEvidence \in BOOLEAN
    /\ restored \in BOOLEAN /\ stopped \in BOOLEAN

BindingStable == binding = Chosen
StopOwnsDescendants == stopped => live = {}
NoLateFact == ~lateWrite
ProjectScoped == ~foreignEvidence
VisibleIsCommitted == published <= written

NeverRestored == ~restored
NeverSharedResult == written = 0
NeverDelegated == "child" \notin live
=============================================================================
