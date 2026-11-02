------------------------- MODULE WakeConsentTimeline -------------------------
(***************************************************************************)
(* Orthogonal timeline abstraction of internal/store/wake_policy.go and      *)
(* CancelScheduleByHuman. One fixed destination has two schedule identities.  *)
(* At most five original human events arrive in any order: two explicit       *)
(* allowances, one ordinary continuation and each ID's first cancellation.   *)
(* One duplicate DELETE may additionally arrive after its exact typed receipt.*)
(* No schedule rows, cadence, retargeting or manager-check lifecycle is modeled.*)
(*                                                                           *)
(* humanEvents is an independent intent oracle, not reconstructed from the    *)
(* runtime receipt/cache. events is the stored input log; decisions is keyed  *)
(* by its original sequence number, while applied records completion order.  *)
(* A cancellation is already a structured denial and never awaits a classifier.*)
(* Any unresolved prose input blocks arming, even if a later event is resolved.*)
(* A read/write storage error refuses the one modeled arm without mutation.   *)
(*                                                                           *)
(* Semantic labels are inputs: this does NOT prove classifier accuracy, quote *)
(* validation, event revision/digest handling, SQLite atomicity, or Go locking.*)
(* All switches FALSE is the required abstract protocol. One switch re-finds  *)
(* a known pre-fix authority omission; the others are seeded hazards, not      *)
(* claims that these faults remain in the production implementation.          *)
(* Finite safety/reachability only; classification is not required to finish.  *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
    ClassificationCompletionOverridesSequence,
    CancelDeduplicatesByThread,
    DuplicateDeleteAppendsDeny,
    PendingInputDoesNotBlock,
    HumanCancelDoesNotWithdraw,
    MaxHumanEvents

AllowTokens == {"allow1", "allow2"}
ProseTokens == AllowTokens \cup {"continue"}
CancelTokens == {"cancelA", "cancelB"}
HumanTokens == ProseTokens \cup CancelTokens
DuplicateTokens == {"duplicateA", "duplicateB"}
ScheduleIDs == {"A", "B"}
MaxStoredEvents == MaxHumanEvents + 1

CancelToken(id) == IF id = "A" THEN "cancelA" ELSE "cancelB"
DuplicateToken(id) == IF id = "A" THEN "duplicateA" ELSE "duplicateB"
CancelID(token) ==
    IF token \in {"cancelA", "duplicateA"} THEN "A" ELSE "B"
IsCancellation(token) == token \in CancelTokens \cup DuplicateTokens

(* The independent human intent oracle never reads the classifier cache. *)
HumanIntent(token) ==
    IF token \in AllowTokens THEN "allow"
    ELSE IF token \in CancelTokens THEN "deny" ELSE "unchanged"

(* Correct classification is an explicit abstraction assumption. *)
ClassifierResult(token) ==
    IF token \in AllowTokens THEN "allow" ELSE "unchanged"

LastIndex(indices) ==
    IF indices = {} THEN 0
    ELSE CHOOSE n \in indices : \A other \in indices : other <= n

VARIABLES
    humanEvents, events, decisions, classified, applied,
    repeatedID, repeatAfterAllowance,
    armResult, armAtHumanVersion, unsafeArm

vars == <<humanEvents, events, decisions, classified, applied,
          repeatedID, repeatAfterAllowance,
          armResult, armAtHumanVersion, unsafeArm>>

HumanSources ==
    {i \in 1..Len(humanEvents) : HumanIntent(humanEvents[i]) # "unchanged"}
HumanSource == LastIndex(HumanSources)
HumanSourceToken == IF HumanSource = 0 THEN "" ELSE humanEvents[HumanSource]
HumanPermits ==
    IF HumanSource = 0 THEN TRUE
    ELSE HumanIntent(humanEvents[HumanSource]) = "allow"

DecisionAt(i) ==
    IF IsCancellation(events[i]) THEN "deny" ELSE decisions[i]
PendingInputs ==
    {i \in 1..Len(events) : events[i] \in ProseTokens /\ decisions[i] = "pending"}
HasPending == PendingInputs # {}
StoredSources ==
    {i \in 1..Len(events) : DecisionAt(i) \in {"allow", "deny"}}
AppliedSources ==
    {i \in 1..Len(applied) : DecisionAt(applied[i]) \in {"allow", "deny"}}
LastApplied == LastIndex(AppliedSources)
LastAppliedToken == IF LastApplied = 0 THEN "" ELSE events[applied[LastApplied]]
PolicySource ==
    IF ClassificationCompletionOverridesSequence
    THEN IF LastApplied = 0 THEN 0 ELSE applied[LastApplied]
    ELSE LastIndex(StoredSources)
PolicySourceToken == IF PolicySource = 0 THEN "" ELSE events[PolicySource]
PolicyPermits ==
    IF PolicySource = 0 THEN TRUE ELSE DecisionAt(PolicySource) = "allow"
RuntimePermits ==
    /\ (~HasPending \/ PendingInputDoesNotBlock)
    /\ PolicyPermits

ReceiptPositions(id) ==
    {i \in 1..Len(events) : IsCancellation(events[i]) /\ CancelID(events[i]) = id}
ReceiptIDs == {id \in ScheduleIDs : ReceiptPositions(id) # {}}

Init ==
    /\ humanEvents = <<>> /\ events = <<>>
    /\ decisions = [i \in 1..MaxStoredEvents |-> "pending"]
    /\ classified = <<>> /\ applied = <<>>
    /\ repeatedID = "none" /\ repeatAfterAllowance = FALSE
    /\ armResult = "not_tried" /\ armAtHumanVersion = 0 /\ unsafeArm = FALSE

(* Original local human input is durable before asynchronous interpretation. *)
HumanProse(token) ==
    /\ token \in ProseTokens
    /\ token \notin {humanEvents[i] : i \in 1..Len(humanEvents)}
    /\ Len(humanEvents) < MaxHumanEvents
    /\ humanEvents' = Append(humanEvents, token)
    /\ events' = Append(events, token)
    /\ UNCHANGED <<decisions, classified, applied, repeatedID,
                   repeatAfterAllowance, armResult, armAtHumanVersion, unsafeArm>>

(* Each ID's first local human DELETE is a new withdrawal of authority. *)
(* HumanCancelDoesNotWithdraw re-finds the known pre-fix missing-authority fault.*)
(* A per-thread deduplication seed wrongly lets A's receipt hide B's action. *)
HumanCancel(id) ==
    /\ id \in ScheduleIDs
    /\ CancelToken(id) \notin {humanEvents[i] : i \in 1..Len(humanEvents)}
    /\ Len(humanEvents) < MaxHumanEvents
    /\ humanEvents' = Append(humanEvents, CancelToken(id))
    /\ LET omitted == HumanCancelDoesNotWithdraw
                      \/ (CancelDeduplicatesByThread /\ ReceiptIDs # {})
       IN /\ events' = IF omitted THEN events ELSE Append(events, CancelToken(id))
          /\ applied' = IF omitted THEN applied ELSE Append(applied, Len(events) + 1)
    /\ UNCHANGED <<decisions, classified, repeatedID, repeatAfterAllowance,
                   armResult, armAtHumanVersion, unsafeArm>>

(* Completion order is unconstrained, but the cache belongs to original seq. *)
Classify(i) ==
    /\ i \in PendingInputs
    /\ decisions' = [decisions EXCEPT ![i] = ClassifierResult(events[i])]
    /\ classified' = Append(classified, i)
    /\ applied' = Append(applied, i)
    /\ UNCHANGED <<humanEvents, events, repeatedID, repeatAfterAllowance,
                   armResult, armAtHumanVersion, unsafeArm>>

(* An exact (schedule ID, fixed destination) typed receipt makes this DELETE  *)
(* the same old action. In particular, it cannot cover a later explicit allow.*)
RepeatDelete(id) ==
    /\ id \in ReceiptIDs /\ repeatedID = "none"
    /\ repeatedID' = id
    /\ repeatAfterAllowance' = (HumanPermits /\ ~HasPending)
    /\ events' = IF DuplicateDeleteAppendsDeny
                  THEN Append(events, DuplicateToken(id)) ELSE events
    /\ applied' = IF DuplicateDeleteAppendsDeny
                   THEN Append(applied, Len(events) + 1) ELSE applied
    /\ UNCHANGED <<humanEvents, decisions, classified,
                   armResult, armAtHumanVersion, unsafeArm>>

(* The authorization read and manager mutation are one abstract store step. *)
(* The separate schedule model checks stale reads and transaction ordering. *)
ModelArm(storageError) ==
    /\ armResult = "not_tried"
    /\ armAtHumanVersion' = Len(humanEvents)
    /\ armResult' =
          IF storageError THEN "storage_error"
          ELSE IF RuntimePermits THEN "armed"
          ELSE IF HasPending /\ ~PendingInputDoesNotBlock THEN "pending_refused"
          ELSE "deny_refused"
    /\ unsafeArm' = (~storageError /\ RuntimePermits /\ (~HumanPermits \/ HasPending))
    /\ UNCHANGED <<humanEvents, events, decisions, classified, applied,
                   repeatedID, repeatAfterAllowance>>

Next ==
    \/ \E token \in ProseTokens : HumanProse(token)
    \/ \E id \in ScheduleIDs : HumanCancel(id)
    \/ \E i \in 1..MaxStoredEvents : Classify(i)
    \/ \E id \in ScheduleIDs : RepeatDelete(id)
    \/ \E storageError \in BOOLEAN : ModelArm(storageError)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ MaxHumanEvents \in 1..5
    /\ humanEvents \in Seq(HumanTokens) /\ Len(humanEvents) <= MaxHumanEvents
    /\ Cardinality({humanEvents[i] : i \in 1..Len(humanEvents)}) = Len(humanEvents)
    /\ events \in Seq(HumanTokens \cup DuplicateTokens)
    /\ Len(events) <= MaxStoredEvents
    /\ decisions \in [1..MaxStoredEvents -> {"pending", "allow", "unchanged"}]
    /\ classified \in Seq(1..Len(events))
    /\ applied \in Seq(1..Len(events))
    /\ repeatedID \in ScheduleIDs \cup {"none"}
    /\ repeatAfterAllowance \in BOOLEAN
    /\ armResult \in {"not_tried", "storage_error", "armed", "pending_refused", "deny_refused"}
    /\ armAtHumanVersion \in 0..MaxHumanEvents /\ unsafeArm \in BOOLEAN

EveryArmAuthorized == ~unsafeArm
(* Equality of the source identity also rejects a missing later cancellation *)
(* when both the old and new intent happen to be deny. It forbids always-deny.*)
SettledPolicyMatchesLatestIntent ==
    ~HasPending => /\ PolicyPermits = HumanPermits
                   /\ PolicySourceToken = HumanSourceToken
ExactIDReceiptUniqueness ==
    \A id \in ScheduleIDs : Cardinality(ReceiptPositions(id)) <= 1
(* This separate negative must lose real permission, not only change source. *)
LaterAllowanceSurvivesDuplicate ==
    (repeatAfterAllowance /\ ~HasPending /\ HumanPermits) => PolicyPermits

(* Reachability checks are intentionally false invariants in separate cases. *)
NeverFullHumanTimeline == ~(Len(humanEvents) = 5 /\ ~HasPending)
NeverOutOfOrderClassification ==
    ~(\E a, b \in 1..Len(classified) : a < b /\ classified[a] > classified[b])
NeverLateAllowPreservesCancellation ==
    ~(~HasPending /\ HumanSourceToken \in CancelTokens
      /\ LastAppliedToken \in AllowTokens /\ ~PolicyPermits)
NeverTwoRoundsOfReauthorization ==
    ~(humanEvents = <<"cancelA", "allow1", "cancelB", "allow2", "continue">>
      /\ ~HasPending /\ PolicyPermits /\ PolicySourceToken = "allow2")
NeverSecondIDRevokesLaterAllowance ==
    ~(humanEvents = <<"cancelA", "allow1", "cancelB">> /\ ~HasPending
      /\ PolicySourceToken = "cancelB" /\ armResult = "deny_refused"
      /\ armAtHumanVersion = 3)
NeverDuplicatePreservesLaterAllowance ==
    ~(repeatedID # "none" /\ repeatAfterAllowance /\ ~HasPending
      /\ PolicyPermits /\ Cardinality(ReceiptPositions(repeatedID)) = 1
      /\ armResult = "armed" /\ armAtHumanVersion = Len(humanEvents))
NeverPendingRefusal ==
    ~(armResult = "pending_refused" /\ HasPending
      /\ armAtHumanVersion = Len(humanEvents))
NeverStorageRefusal == armResult # "storage_error"
NeverOrdinaryContinuePreservesDenial ==
    ~(humanEvents = <<"cancelA", "continue">> /\ ~HasPending /\ ~PolicyPermits
      /\ armResult = "deny_refused" /\ armAtHumanVersion = 2)
NeverSecondAllowSurvivesEarlierResult ==
    ~(humanEvents = <<"cancelA", "allow1", "cancelB", "allow2">> /\ ~HasPending
      /\ LastAppliedToken = "allow1" /\ PolicySourceToken = "allow2"
      /\ armResult = "armed" /\ armAtHumanVersion = 4)
=============================================================================
