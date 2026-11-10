------------------------------ MODULE DataBackup ------------------------------
EXTENDS Naturals

(* One data directory can be copied or replaced only while it is idle.
   A failed full restore puts the snapshot back and does not advance the
   generation. A project restore cannot take an identity another project
   already owns. The switches remove those guards so TLC can show the
   invariant that fails. This is the backup state machine, not SQLite. *)

CONSTANTS
    AllowWhileBusy,
    SkipRollback,
    AllowForeignIdentity

VARIABLES
    work, phase, generation, snapshot, foreign, heldGeneration

vars == <<work, phase, generation, snapshot, foreign, heldGeneration>>

Phases == {"idle", "backup", "restore"}
Snapshots == {"current", "restored", "lost"}

Init ==
    /\ work = FALSE
    /\ phase = "idle"
    /\ generation = 0
    /\ snapshot = "current"
    /\ foreign = FALSE
    /\ heldGeneration = 0

TypeOK ==
    /\ work \in BOOLEAN
    /\ phase \in Phases
    /\ generation \in 0..2
    /\ snapshot \in Snapshots
    /\ foreign \in BOOLEAN
    /\ heldGeneration \in 0..2

CanEnter ==
    \/ AllowWhileBusy
    \/ (phase = "idle" /\ ~work)

SetBusy ==
    /\ phase = "idle"
    /\ ~work
    /\ work' = TRUE
    /\ UNCHANGED <<phase, generation, snapshot, foreign, heldGeneration>>

ClearBusy ==
    /\ work
    /\ work' = FALSE
    /\ UNCHANGED <<phase, generation, snapshot, foreign, heldGeneration>>

StartBackup ==
    /\ CanEnter
    /\ phase' = "backup"
    /\ UNCHANGED <<work, generation, snapshot, foreign, heldGeneration>>

FinishBackup ==
    /\ phase = "backup"
    /\ phase' = "idle"
    /\ UNCHANGED <<work, generation, snapshot, foreign, heldGeneration>>

StartRestore ==
    /\ CanEnter
    /\ phase' = "restore"
    /\ heldGeneration' = generation
    /\ snapshot' = "current"
    /\ UNCHANGED <<work, generation, foreign>>

FailRestore ==
    /\ phase = "restore"
    /\ phase' = "idle"
    /\ IF SkipRollback
       THEN /\ snapshot' = "lost"
            /\ generation' = IF generation < 2 THEN generation + 1 ELSE 2
       ELSE /\ snapshot' = "restored"
            /\ UNCHANGED generation
    /\ UNCHANGED <<work, foreign, heldGeneration>>

CommitRestore ==
    /\ phase = "restore"
    /\ phase' = "idle"
    /\ snapshot' = "current"
    /\ generation' = IF generation < 2 THEN generation + 1 ELSE 2
    /\ UNCHANGED <<work, foreign, heldGeneration>>

ApplyProject ==
    /\ CanEnter
    /\ phase = "idle"
    /\ foreign' = (foreign \/ AllowForeignIdentity)
    /\ generation' = IF generation < 2 THEN generation + 1 ELSE 2
    /\ snapshot' = "current"
    /\ UNCHANGED <<work, phase, heldGeneration>>

Next ==
    \/ SetBusy
    \/ ClearBusy
    \/ StartBackup
    \/ FinishBackup
    \/ StartRestore
    \/ FailRestore
    \/ CommitRestore
    \/ ApplyProject

Spec == Init /\ [][Next]_vars

RestoringImpliesIdle == phase \in {"backup", "restore"} => ~work

SnapshotNeverLost ==
    /\ snapshot /= "lost"
    /\ (snapshot = "restored" => generation = heldGeneration)

ForeignUnchanged == ~foreign

NeverCommitted == generation = 0

=============================================================================
