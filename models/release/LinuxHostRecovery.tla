--------------------------- MODULE LinuxHostRecovery ---------------------------
EXTENDS Naturals

(* One installation and one attempted update. The supervisor and committed
   journal live outside both data images. Crash includes losing all processes;
   Boot is the verified service startup, with no SSH or phone action. *)

CONSTANTS NoBootOwner, SkipJournal, TouchOriginal, LoopbackOnly, ActivateBeforeSync,
          RestoreAfterCommit, CompetingLauncher, DropListener, CopyBeforeRecord

VARIABLES phase, supervisor, journal, live, originalIntact, localReady, proof,
          effect, powered, writers, port

vars == <<phase, supervisor, journal, live, originalIntact, localReady, proof,
          effect, powered, writers, port>>

Init ==
    /\ phase = "serving" /\ supervisor \in BOOLEAN
    /\ journal = "original" /\ live = "original"
    /\ originalIntact = TRUE /\ localReady = FALSE /\ proof = FALSE /\ effect = FALSE /\ powered = TRUE
    /\ writers = 1 /\ port = "retained"

TypeOK ==
    /\ phase \in {"serving", "staged", "copying", "prepared", "trial", "promoted", "recovered", "booted-pending-cleanup"}
    /\ journal \in {"original", "preparing", "trial", "committed"}
    /\ live \in {"original", "candidate"}
    /\ supervisor \in BOOLEAN /\ originalIntact \in BOOLEAN /\ localReady \in BOOLEAN
    /\ proof \in BOOLEAN /\ effect \in BOOLEAN /\ powered \in BOOLEAN
    /\ writers \in 0..2
    /\ port \in {"retained", "unbound", "foreign"}

Stage ==
    /\ powered /\ phase = "serving" /\ (supervisor \/ NoBootOwner)
    /\ phase' = "staged"
    /\ UNCHANGED <<supervisor, journal, live, originalIntact, localReady, proof, effect, powered, writers, port>>

BeginCopy ==
    /\ powered /\ phase = "staged"
    /\ phase' = "copying" /\ writers' = 0
    /\ journal' = IF CopyBeforeRecord THEN journal ELSE "preparing"
    /\ port' = IF DropListener THEN "unbound" ELSE port
    /\ UNCHANGED <<supervisor, live, originalIntact, localReady, proof, effect, powered>>

Prepare ==
    /\ powered /\ phase = "copying"
    /\ phase' = "prepared"
    /\ journal' = IF SkipJournal THEN "original" ELSE "trial"
    /\ UNCHANGED <<supervisor, live, originalIntact, localReady, proof, effect, powered, writers, port>>

StartTrial ==
    /\ powered /\ phase = "prepared" /\ writers = 0
    /\ phase' = "trial" /\ live' = "candidate" /\ writers' = 1
    /\ originalIntact' = ~TouchOriginal
    /\ UNCHANGED <<supervisor, journal, localReady, proof, effect, powered, port>>

Listen ==
    /\ powered /\ phase = "trial" /\ ~localReady
    /\ localReady' = TRUE
    /\ UNCHANGED <<phase, supervisor, journal, live, originalIntact, proof, effect, powered, writers, port>>

VerifyPair ==
    /\ powered /\ phase = "trial" /\ localReady /\ ~proof
    /\ proof' = TRUE
    /\ UNCHANGED <<phase, supervisor, journal, live, originalIntact, localReady, effect, powered, writers, port>>

Promote ==
    /\ powered /\ phase = "trial" /\ localReady /\ (proof \/ LoopbackOnly)
    /\ phase' = "promoted"
    /\ journal' = IF ActivateBeforeSync THEN journal ELSE "committed"
    /\ effect' = ActivateBeforeSync
    /\ UNCHANGED <<supervisor, live, originalIntact, localReady, proof, powered, writers, port>>

EnableWrites ==
    /\ powered /\ phase = "promoted" /\ journal = "committed" /\ ~effect
    /\ effect' = TRUE
    /\ UNCHANGED <<phase, supervisor, journal, live, originalIntact, localReady, proof, powered, writers, port>>

Crash ==
    /\ powered
    /\ powered' = FALSE /\ writers' = 0
    /\ UNCHANGED <<phase, supervisor, journal, live, originalIntact, localReady, proof, effect, port>>

Boot ==
    /\ ~powered /\ supervisor
    /\ powered' = TRUE /\ writers' = 1
    /\ phase' = IF journal = "preparing" THEN "booted-pending-cleanup" ELSE "recovered"
    /\ live' = IF journal = "committed" /\ ~RestoreAfterCommit THEN "candidate" ELSE "original"
    /\ journal' = CASE live' = "candidate" -> "committed"
                      [] journal = "preparing" -> "preparing"
                      [] OTHER -> "original"
    /\ UNCHANGED <<supervisor, originalIntact, localReady, proof, effect, port>>

ExpireTrial ==
    /\ powered /\ phase \in {"copying", "prepared", "trial"} /\ supervisor
    /\ phase' = "recovered" /\ live' = "original" /\ writers' = 1
    /\ journal' = IF journal = "preparing" THEN "preparing" ELSE "original"
    /\ UNCHANGED <<supervisor, originalIntact, localReady, proof, effect, powered, port>>

OpenForeign ==
    /\ CompetingLauncher /\ powered /\ phase = "trial" /\ writers = 1
    /\ writers' = 2
    /\ UNCHANGED <<phase, supervisor, journal, live, originalIntact, localReady, proof, effect, powered, port>>

TakePort ==
    /\ powered /\ phase \in {"copying", "prepared"} /\ port = "unbound"
    /\ port' = "foreign"
    /\ UNCHANGED <<phase, supervisor, journal, live, originalIntact, localReady, proof, effect, powered, writers>>

Next == Stage \/ BeginCopy \/ Prepare \/ StartTrial \/ Listen \/ VerifyPair \/ Promote \/ EnableWrites
        \/ Crash \/ Boot \/ ExpireTrial \/ OpenForeign \/ TakePort

(* These fairness assumptions represent a working systemd boot service
   and a trial watchdog able to read and sync its local storage. *)
Spec == Init /\ [][Next]_vars /\ WF_vars(Boot) /\ WF_vars(ExpireTrial)

BootOwnerRequired == phase \in {"staged", "copying", "prepared", "trial", "promoted"} => supervisor
CloneHasDurableOwner == phase = "copying" => journal = "preparing"
TrialHasDurablePlan == phase = "trial" => journal = "trial"
RetainOriginal == journal # "committed" => originalIntact
CommitRequiresPair == journal = "committed" => proof
EffectsRequireCommittedCandidate == effect => journal = "committed" /\ live = "candidate"
OneWriter == writers <= 1
RetainedListener == phase \in {"copying", "prepared", "trial"} => port # "foreign"
EventuallyResolves ==
    (phase \in {"copying", "prepared", "trial"}) ~> (phase \in {"recovered", "promoted", "booted-pending-cleanup"})

NeverRolledBack == ~(phase = "recovered" /\ localReady /\ live = "original")
NeverBootedCommitted == ~(phase = "recovered" /\ effect /\ live = "candidate")
NeverBootedWithPartialCopy == ~(phase = "booted-pending-cleanup" /\ journal = "preparing" /\ writers = 1)

=============================================================================
