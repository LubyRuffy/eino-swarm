------------------------------ MODULE DataBackup ------------------------------
EXTENDS Naturals

(* One data directory can be replaced only while it is idle. A backup may
   also start while work is still active when that work is recorded as
   omitted from the snapshot; the work itself keeps running. A failed full
   restore puts the snapshot back and does not advance the generation. A
   project restore cannot take an identity another project already owns.
   The switches remove those guards so TLC can show the invariant that
   fails. This is the backup state machine, not SQLite. *)

CONSTANTS
    AllowWhileBusy,
    SkipRollback,
    AllowForeignIdentity

VARIABLES
    work, phase, generation, snapshot, foreign, heldGeneration, omitted

vars == <<work, phase, generation, snapshot, foreign, heldGeneration, omitted>>

Phases == {"idle", "backup", "restore"}
Snapshots == {"current", "restored", "lost"}

Init ==
    /\ work = FALSE
    /\ phase = "idle"
    /\ generation = 0
    /\ snapshot = "current"
    /\ foreign = FALSE
    /\ heldGeneration = 0
    /\ omitted = FALSE

TypeOK ==
    /\ work \in BOOLEAN
    /\ phase \in Phases
    /\ generation \in 0..2
    /\ snapshot \in Snapshots
    /\ foreign \in BOOLEAN
    /\ heldGeneration \in 0..2
    /\ omitted \in BOOLEAN

CanEnter ==
    \/ AllowWhileBusy
    \/ (phase = "idle" /\ ~work)

SetBusy ==
    /\ phase = "idle"
    /\ ~work
    /\ work' = TRUE
    /\ UNCHANGED <<phase, generation, snapshot, foreign, heldGeneration, omitted>>

ClearBusy ==
    /\ work
    /\ work' = FALSE
    /\ UNCHANGED <<phase, generation, snapshot, foreign, heldGeneration, omitted>>

StartBackup ==
    /\ CanEnter
    /\ phase' = "backup"
    /\ omitted' = FALSE
    /\ UNCHANGED <<work, generation, snapshot, foreign, heldGeneration>>

(* The product choice: leave the running work out of this snapshot and
   continue. Restore has no matching action. AllowWhileBusy is the illegal
   path that copies or replaces while work is still inside the snapshot. *)
StartBackupSkipping ==
    /\ phase = "idle"
    /\ work
    /\ ~AllowWhileBusy
    /\ phase' = "backup"
    /\ omitted' = TRUE
    /\ UNCHANGED <<work, generation, snapshot, foreign, heldGeneration>>

FinishBackup ==
    /\ phase = "backup"
    /\ phase' = "idle"
    /\ omitted' = FALSE
    /\ UNCHANGED <<work, generation, snapshot, foreign, heldGeneration>>

StartRestore ==
    /\ CanEnter
    /\ phase' = "restore"
    /\ heldGeneration' = generation
    /\ snapshot' = "current"
    /\ omitted' = FALSE
    /\ UNCHANGED <<work, generation, foreign>>

FailRestore ==
    /\ phase = "restore"
    /\ phase' = "idle"
    /\ IF SkipRollback
       THEN /\ snapshot' = "lost"
            /\ generation' = IF generation < 2 THEN generation + 1 ELSE 2
       ELSE /\ snapshot' = "restored"
            /\ UNCHANGED generation
    /\ UNCHANGED <<work, foreign, heldGeneration, omitted>>

CommitRestore ==
    /\ phase = "restore"
    /\ phase' = "idle"
    /\ snapshot' = "current"
    /\ generation' = IF generation < 2 THEN generation + 1 ELSE 2
    /\ UNCHANGED <<work, foreign, heldGeneration, omitted>>

ApplyProject ==
    /\ CanEnter
    /\ phase = "idle"
    /\ foreign' = (foreign \/ AllowForeignIdentity)
    /\ generation' = IF generation < 2 THEN generation + 1 ELSE 2
    /\ snapshot' = "current"
    /\ UNCHANGED <<work, phase, heldGeneration, omitted>>

Next ==
    \/ SetBusy
    \/ ClearBusy
    \/ StartBackup
    \/ StartBackupSkipping
    \/ FinishBackup
    \/ StartRestore
    \/ FailRestore
    \/ CommitRestore
    \/ ApplyProject

Spec == Init /\ [][Next]_vars

RestoringImpliesIdle ==
    /\ (phase = "restore" => ~work)
    /\ (phase = "backup" => ~work \/ omitted)

SnapshotNeverLost ==
    /\ snapshot /= "lost"
    /\ (snapshot = "restored" => generation = heldGeneration)

ForeignUnchanged == ~foreign

NeverCommitted == generation = 0

NeverOmitted == omitted = FALSE

=============================================================================
