----------------------- MODULE ModelGenerationRecovery -----------------------
(***************************************************************************)
(* One existing turn and Registry, with a capped manager generation followed *)
(* by at most one recovery RunWith. Engine ownership, workers and exec       *)
(* sessions do not move. This refines a boundary absent from EngineLifecycle *)
(* and EventStream: the asynchronous ADK AfterModel writer must settle before *)
(* Registry.exec returns its typed cap and a successor RunWith owns history. *)
(*                                                                         *)
(* NoJoin restores the suspected early return in run.go, not an unrelated    *)
(* engine Stop/handoff or event seq protocol. It is an accepted historical    *)
(* switch only after a real Registry/ADK regression exhibits the same stale   *)
(* writer ordering. A model counterexample alone does not prove the Go bug.  *)
(*                                                                         *)
(* historyEpoch abstracts the current run's non-tool input/history identity. *)
(* SetHistory merges missing tool results, but does not preserve a newer     *)
(* input/correction when an older AfterModel replaces the whole transcript.  *)
(* Snapshot values are immutable copies indexed by the observed cap ID;      *)
(* actual request-byte identity and worker/process ownership require tests.  *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
    NoJoin,
    MaxRequests,
    InitialRequestsSpent

ASSUME /\ NoJoin \in BOOLEAN
       /\ InitialRequestsSpent \in Nat
       /\ MaxRequests = InitialRequestsSpent + 1

VARIABLES oldPhase, oldWriterDone, capObserved, snapshotID, snapshotEpoch,
          recoveryInputID, recoveryInputEpoch, claimCount, recoveryStarted,
          currentEpoch, historyEpoch, cancelled, startedAfterCancel,
          requestsSpent, secondCap, cause

vars == <<oldPhase, oldWriterDone, capObserved, snapshotID, snapshotEpoch,
          recoveryInputID, recoveryInputEpoch, claimCount, recoveryStarted,
          currentEpoch, historyEpoch, cancelled, startedAfterCancel,
          requestsSpent, secondCap, cause>>

Init ==
    /\ oldPhase = "streaming" /\ oldWriterDone = FALSE /\ capObserved = FALSE
    /\ snapshotID = 0 /\ snapshotEpoch = 0
    /\ recoveryInputID = 0 /\ recoveryInputEpoch = 0
    /\ claimCount = 0 /\ recoveryStarted = FALSE
    /\ currentEpoch = 1 /\ historyEpoch = 1
    /\ cancelled = FALSE /\ startedAfterCancel = FALSE
    /\ requestsSpent = InitialRequestsSpent /\ secondCap = FALSE /\ cause = "none"

(* generationModel.Stream binds its actual input to this generation's ID. *)
CaptureInput ==
    /\ oldPhase = "streaming" /\ snapshotID = 0
    /\ snapshotID' = 1 /\ snapshotEpoch' = historyEpoch
    /\ UNCHANGED <<oldPhase, oldWriterDone, capObserved, recoveryInputID,
                   recoveryInputEpoch, claimCount, recoveryStarted, currentEpoch,
                   historyEpoch, cancelled, startedAfterCancel, requestsSpent,
                   secondCap, cause>>

(* streamAcc.drain observes clean reasoning-only EOF/length, no tool chunks. *)
ObserveCap ==
    /\ oldPhase = "streaming" /\ snapshotID = 1
    /\ oldPhase' = "cap_read" /\ capObserved' = TRUE /\ cause' = "output_limit"
    /\ UNCHANGED <<oldWriterDone, snapshotID, snapshotEpoch, recoveryInputID,
                   recoveryInputEpoch, claimCount, recoveryStarted, currentEpoch,
                   historyEpoch, cancelled, startedAfterCancel, requestsSpent,
                   secondCap>>

(* The old runner's real AfterModel/SetHistory finishes asynchronously. *)
OldAfterModel ==
    /\ oldPhase \in {"cap_read", "returned"} /\ ~oldWriterDone
    /\ oldWriterDone' = TRUE /\ historyEpoch' = 1
    /\ UNCHANGED <<oldPhase, capObserved, snapshotID, snapshotEpoch,
                   recoveryInputID, recoveryInputEpoch, claimCount, recoveryStarted,
                   currentEpoch, cancelled, startedAfterCancel, requestsSpent,
                   secondCap, cause>>

(* Fixed exec drains the already capped iterator before returning. The bad *)
(* switch permits the original immediate return while its writer is alive. *)
ReturnCap ==
    /\ oldPhase = "cap_read" /\ (NoJoin \/ oldWriterDone)
    /\ oldPhase' = "returned"
    /\ UNCHANGED <<oldWriterDone, capObserved, snapshotID, snapshotEpoch,
                   recoveryInputID, recoveryInputEpoch, claimCount, recoveryStarted,
                   currentEpoch, historyEpoch, cancelled, startedAfterCancel,
                   requestsSpent, secondCap, cause>>

(* The retry allowance is durable before another generation is admitted. *)
ClaimRecovery ==
    /\ oldPhase = "returned" /\ capObserved /\ ~cancelled
    /\ claimCount = 0 /\ snapshotID = 1
    /\ claimCount' = 1
    /\ UNCHANGED <<oldPhase, oldWriterDone, capObserved, snapshotID, snapshotEpoch,
                   recoveryInputID, recoveryInputEpoch, recoveryStarted, currentEpoch,
                   historyEpoch, cancelled, startedAfterCancel, requestsSpent,
                   secondCap, cause>>

(* Same Registry/turn/provider budget; the capped input, not initial input. *)
StartRecovery ==
    /\ oldPhase = "returned" /\ claimCount = 1 /\ ~recoveryStarted
    /\ ~cancelled /\ requestsSpent < MaxRequests
    /\ recoveryStarted' = TRUE /\ currentEpoch' = 2 /\ historyEpoch' = 2
    /\ recoveryInputID' = snapshotID /\ recoveryInputEpoch' = snapshotEpoch
    /\ requestsSpent' = requestsSpent + 1 /\ startedAfterCancel' = cancelled
    /\ UNCHANGED <<oldPhase, oldWriterDone, capObserved, snapshotID, snapshotEpoch,
                   claimCount, cancelled, secondCap, cause>>

(* A second cap must not grant another allowance or reset the wire budget. *)
ObserveSecondCap ==
    /\ recoveryStarted /\ ~secondCap
    /\ secondCap' = TRUE
    /\ UNCHANGED <<oldPhase, oldWriterDone, capObserved, snapshotID, snapshotEpoch,
                   recoveryInputID, recoveryInputEpoch, claimCount, recoveryStarted,
                   currentEpoch, historyEpoch, cancelled, startedAfterCancel,
                   requestsSpent, cause>>

(* Cancellation blocks both a new durable claim and a later admission. It *)
(* never relabels the already observed generation's output-limit cause.   *)
Cancel ==
    /\ ~cancelled /\ cancelled' = TRUE
    /\ UNCHANGED <<oldPhase, oldWriterDone, capObserved, snapshotID, snapshotEpoch,
                   recoveryInputID, recoveryInputEpoch, claimCount, recoveryStarted,
                   currentEpoch, historyEpoch, startedAfterCancel, requestsSpent,
                   secondCap, cause>>

Next == CaptureInput \/ ObserveCap \/ OldAfterModel \/ ReturnCap \/ ClaimRecovery
        \/ StartRecovery \/ ObserveSecondCap \/ Cancel
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ oldPhase \in {"streaming", "cap_read", "returned"}
    /\ oldWriterDone \in BOOLEAN /\ capObserved \in BOOLEAN
    /\ snapshotID \in 0..1 /\ snapshotEpoch \in 0..1
    /\ recoveryInputID \in 0..1 /\ recoveryInputEpoch \in 0..1
    /\ claimCount \in 0..1 /\ recoveryStarted \in BOOLEAN
    /\ currentEpoch \in 1..2 /\ historyEpoch \in 1..2
    /\ cancelled \in BOOLEAN /\ startedAfterCancel \in BOOLEAN
    /\ requestsSpent \in InitialRequestsSpent..MaxRequests
    /\ secondCap \in BOOLEAN /\ cause \in {"none", "output_limit"}

RecoveredHistoryOwnsEpoch == recoveryStarted => historyEpoch = currentEpoch
RecoveryUsesCappedInput == recoveryStarted =>
    recoveryInputID = 1 /\ recoveryInputEpoch = snapshotEpoch /\ snapshotEpoch = 1
OneDurableClaimPerTurn == claimCount <= 1 /\ (recoveryStarted => claimCount = 1)
CancellationDoesNotStartRecovery == ~startedAfterCancel
RequestBudgetConserved ==
    requestsSpent = InitialRequestsSpent + (IF recoveryStarted THEN 1 ELSE 0)
    /\ requestsSpent <= MaxRequests
CapCauseRetained == capObserved => cause = "output_limit"

(* Reachability witnesses prevent success by disabling the recovery path. *)
NeverRecovery == ~recoveryStarted
NeverCancelledAfterClaim == ~(cancelled /\ claimCount = 1 /\ ~recoveryStarted)
NeverSecondCap == ~secondCap
=============================================================================
