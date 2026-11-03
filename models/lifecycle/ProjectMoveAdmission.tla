----------------------- MODULE ProjectMoveAdmission -----------------------
EXTENDS TLC

(* Refinement of EngineLifecycle's ParkedSuccessorOwnsProject.
   Unlike its atomic ownerMatches input, this model distinguishes the saved
   lookup, concurrent Move, goal receipt, and Registry publication. *)
CONSTANTS
    UnorderedClaim,
    UnorderedGoal,
    RefusalDropsParked

VARIABLES project, phase, origin, observed, registry, resumedForeign, claimedForeign

vars == <<project, phase, origin, observed, registry, resumedForeign, claimedForeign>>

Init ==
    /\ project = "original" /\ phase = "idle" /\ origin = "none"
    /\ observed = "unknown" /\ registry = "parked"
    /\ resumedForeign = FALSE /\ claimedForeign = FALSE

Begin(kind) ==
    /\ phase = "idle" /\ registry = "parked"
    /\ phase' = "lookup" /\ origin' = kind
    /\ UNCHANGED <<project, observed, registry, resumedForeign, claimedForeign>>

Observe ==
    /\ phase = "lookup"
    /\ observed' = project
    /\ phase' = IF project = "original" THEN "validated" ELSE "refused"
    /\ registry' = IF project = "foreign" /\ RefusalDropsParked THEN "lost" ELSE registry
    /\ UNCHANGED <<project, origin, resumedForeign, claimedForeign>>

Move ==
    /\ (phase \notin {"lookup", "validated", "resumed"}
       \/ (origin = "wake" /\ UnorderedClaim)
       \/ (origin = "goal" /\ UnorderedGoal))
    /\ project' = IF project = "original" THEN "foreign" ELSE "original"
    /\ UNCHANGED <<phase, origin, observed, registry, resumedForeign, claimedForeign>>

RecordResume ==
    /\ origin = "goal" /\ phase = "validated" /\ observed = "original"
    /\ phase' = "resumed"
    /\ resumedForeign' = (project = "foreign")
    /\ UNCHANGED <<project, origin, observed, registry, claimedForeign>>

Admit ==
    /\ (origin = "wake" /\ phase = "validated") \/ (origin = "goal" /\ phase = "resumed")
    /\ IF origin = "goal" /\ project = "foreign"
          THEN /\ phase' = "refused" /\ registry' = registry /\ UNCHANGED claimedForeign
          ELSE /\ phase' = "claimed" /\ registry' = "occupied"
               /\ claimedForeign' = (project = "foreign")
    /\ UNCHANGED <<project, origin, observed, resumedForeign>>

RetryAfterRefusal ==
    /\ phase = "refused" /\ ~resumedForeign
    /\ phase' = "idle" /\ origin' = "none" /\ observed' = "unknown"
    /\ UNCHANGED <<project, registry, resumedForeign, claimedForeign>>

Next == (\E kind \in {"wake", "goal"} : Begin(kind))
        \/ Observe \/ Move \/ RecordResume \/ Admit \/ RetryAfterRefusal
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ project \in {"original", "foreign"}
    /\ phase \in {"idle", "lookup", "validated", "resumed", "refused", "claimed"}
    /\ origin \in {"none", "wake", "goal"}
    /\ observed \in {"unknown", "original", "foreign"}
    /\ registry \in {"parked", "occupied", "lost"}
    /\ resumedForeign \in BOOLEAN
    /\ claimedForeign \in BOOLEAN

ClaimOwnsProject == ~claimedForeign
ResumeReceiptOwnsProject == ~resumedForeign
RefusalKeepsParked == phase = "refused" => registry = "parked"
NeverWakeClaim == ~(phase = "claimed" /\ origin = "wake")
NeverGoalClaim == ~(phase = "claimed" /\ origin = "goal")
NeverMoveBeforeRefusal == ~(phase = "refused" /\ observed = "foreign")
NeverMoveAfterClaim == ~(phase = "claimed" /\ project = "foreign")
=============================================================================
