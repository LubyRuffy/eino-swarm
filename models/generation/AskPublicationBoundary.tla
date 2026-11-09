----------------------- MODULE AskPublicationBoundary -----------------------
(***************************************************************************)
(* Private admitted protocol slice. Actual UI-ID early-gate observations on *)
(* both source versions reproduce clean producer EOF, ADK/history/tool entry,*)
(* lagging final call row, empty waiter ID despite the actual ctx call ID,   *)
(* real checkpoint then owner cancellation, and a final unknown ask result. *)
(* ADK history completion precedes tool entry; this model does NOT retain   *)
(* the rejected history-before-first-SetHistory hypothesis.                 *)
(* Model results require independent actual Registry/HTTP implementation QA. *)
(* Answer ACK actions remain target payload obligations only; no actual ACK *)
(* failure is claimed from this unanswered-ask known-fault observation.     *)
(* EventStream Record and lifecycle checkpoint/cancel are opaque existing   *)
(* atomic actions. No seq/broadcast, ownership, result success, or restored  *)
(* placeholder suppression is changed or proven here.                      *)
(***************************************************************************)
EXTENDS Naturals

CONSTANTS
    LateConsumerCancelMasksEOF,
    WaiterUsesStoreIdentityOnly,
    ToolKind,
    SourceOutcome,
    CompleteCall,
    FinishPresent,
    CallID

ASSUME /\ LateConsumerCancelMasksEOF \in BOOLEAN
       /\ WaiterUsesStoreIdentityOnly \in BOOLEAN
       /\ ToolKind \in {"ask", "external"}
       /\ SourceOutcome \in {"clean_eof", "source_error"}
       /\ CompleteCall \in BOOLEAN /\ FinishPresent \in BOOLEAN /\ CallID # ""

VARIABLE s
vars == <<s>>

Init == s = [sourceTerminal |-> "pending", adkEntered |-> FALSE,
             historyCall |-> FALSE, uiDecision |-> "pending", uiSettled |-> FALSE,
             callStored |-> FALSE, waiterArmed |-> FALSE, waiterID |-> "",
             answerAcked |-> FALSE, acceptedID |-> "",
             checkpoint |-> [made |-> FALSE, pauseAsk |-> FALSE, askID |-> "",
                             answerAcked |-> FALSE, answerID |-> "", historyCall |-> FALSE],
             ownerCancelled |-> FALSE, enteredAtCancel |-> FALSE,
             confirmedReceipt |-> FALSE, toolClosed |-> FALSE, toolResult |-> "none"]

(* Observation at the actual source, before consumer-local cancellation. *)
ProducerTerminal ==
    /\ s.sourceTerminal = "pending"
    /\ SourceOutcome = "source_error" \/ ~s.ownerCancelled
    /\ s' = [s EXCEPT !.sourceTerminal = SourceOutcome]

(* Real typedStateModelWrapper finishes concat and AfterModel before ToolsNode. *)
ADKHistoryHasCall ==
    /\ s.sourceTerminal = "clean_eof" /\ CompleteCall /\ ~s.historyCall
    /\ s' = [s EXCEPT !.historyCall = TRUE]

ADKEntersTool ==
    /\ s.sourceTerminal = "clean_eof" /\ CompleteCall
    /\ s.historyCall /\ ~s.adkEntered /\ ~s.ownerCancelled
    /\ s' = [s EXCEPT !.adkEntered = TRUE]

(* Only the targeted live wait with a real nonempty ctx call ID is in scope. *)
ArmWaiterAfterStoreProbes ==
    /\ ToolKind = "ask" /\ s.adkEntered /\ ~s.ownerCancelled
    /\ ~s.waiterArmed /\ ~s.answerAcked
    /\ s' = [s EXCEPT !.waiterArmed = TRUE,
             !.waiterID = IF WaiterUsesStoreIdentityOnly
                          THEN (IF s.callStored THEN CallID ELSE "") ELSE CallID]

(* Input has already passed the real question/answer validation in the probe. *)
AcceptOriginalAnswer ==
    /\ ToolKind = "ask" /\ s.waiterArmed /\ ~s.answerAcked
    /\ ~s.ownerCancelled /\ ~s.checkpoint.made
    /\ s' = [s EXCEPT !.answerAcked = TRUE, !.acceptedID = s.waiterID]

ReleaseAnsweredWaiter ==
    /\ s.waiterArmed /\ s.answerAcked
    /\ s' = [s EXCEPT !.waiterArmed = FALSE]

(* Payload projection; the observed complete-ask slice checkpoints only after *)
(* its actual waiter/ACK, not the unobserved checkpoint-before-tool schedule. *)
SaveCheckpoint ==
    /\ ~s.checkpoint.made
    /\ s.sourceTerminal # "pending"
    /\ ToolKind = "external" \/ SourceOutcome = "source_error" \/ ~CompleteCall \/ s.waiterArmed \/ s.answerAcked
    /\ s' = [s EXCEPT !.checkpoint =
        [made |-> TRUE, pauseAsk |-> ToolKind = "ask" /\ s.waiterArmed /\ ~s.answerAcked,
         askID |-> IF s.waiterArmed THEN s.waiterID ELSE "",
         answerAcked |-> s.answerAcked,
         answerID |-> IF s.answerAcked THEN s.acceptedID ELSE "",
         historyCall |-> s.historyCall]]

(* This slice covers observed checkpoint-before-owner-cancel handoff only. *)
CancelOwner ==
    /\ s.checkpoint.made /\ ~s.ownerCancelled
    /\ s' = [s EXCEPT !.ownerCancelled = TRUE, !.enteredAtCancel = s.adkEntered]

(* signal.go's terminal decision, before NotifyToolCall's record callback. *)
UIDecidesFinalPublication ==
    /\ s.sourceTerminal # "pending" /\ s.uiDecision = "pending"
    /\ s' = [s EXCEPT !.uiDecision =
        IF s.sourceTerminal = "clean_eof" /\ CompleteCall
           /\ ~(LateConsumerCancelMasksEOF /\ ~FinishPresent /\ s.ownerCancelled)
        THEN "publish" ELSE "drop"]

(* Existing EventStream.Record is opaque here: persistence/seq/broadcast are
   not split or weakened. callStored means its final tool_call row exists. *)
StoreFinalCall ==
    /\ s.uiDecision = "publish" /\ ~s.callStored
    /\ s' = [s EXCEPT !.callStored = TRUE]

SettleUIDrain ==
    /\ ~s.uiSettled
    /\ s.uiDecision = "drop" \/ (s.uiDecision = "publish" /\ s.callStored)
    /\ s' = [s EXCEPT !.uiSettled = TRUE]

(* A clean call-generation EOF is never an external-effect result receipt. *)
RecordConfirmedExternalResult ==
    /\ ToolKind = "external" /\ s.adkEntered /\ ~s.confirmedReceipt
    /\ s' = [s EXCEPT !.confirmedReceipt = TRUE, !.toolResult = "confirmed"]

CloseUnconfirmedExternalTool ==
    /\ ToolKind = "external" /\ s.adkEntered /\ s.ownerCancelled /\ ~s.toolClosed
    /\ s' = [s EXCEPT !.toolClosed = TRUE,
             !.toolResult = IF s.confirmedReceipt THEN "confirmed" ELSE "unknown"]

Next == ProducerTerminal \/ ADKHistoryHasCall \/ ADKEntersTool
        \/ ArmWaiterAfterStoreProbes \/ AcceptOriginalAnswer \/ ReleaseAnsweredWaiter
        \/ SaveCheckpoint \/ CancelOwner \/ UIDecidesFinalPublication \/ StoreFinalCall
        \/ SettleUIDrain \/ RecordConfirmedExternalResult \/ CloseUnconfirmedExternalTool
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ s.sourceTerminal \in {"pending", "clean_eof", "source_error"}
    /\ s.uiDecision \in {"pending", "publish", "drop"}
    /\ s.waiterID \in {"", CallID} /\ s.acceptedID \in {"", CallID}
    /\ s.checkpoint.askID \in {"", CallID} /\ s.checkpoint.answerID \in {"", CallID}
    /\ s.toolResult \in {"none", "unknown", "confirmed"}
    /\ \A flag \in {s.adkEntered, s.historyCall, s.uiSettled, s.callStored,
                     s.waiterArmed, s.answerAcked, s.ownerCancelled, s.enteredAtCancel,
                     s.confirmedReceipt, s.toolClosed, s.checkpoint.made,
                     s.checkpoint.pauseAsk, s.checkpoint.answerAcked,
                     s.checkpoint.historyCall} : flag \in BOOLEAN

ObservedSourceFactRetained == s.sourceTerminal # "pending" => s.sourceTerminal = SourceOutcome
NoInvalidToolAdmission == s.adkEntered => s.sourceTerminal = "clean_eof" /\ CompleteCall /\ s.historyCall
NoNewToolAfterOwnerCancel == s.ownerCancelled => s.adkEntered = s.enteredAtCancel
ExecutedCleanCallPublishedWhenUIDone ==
    (s.adkEntered /\ s.sourceTerminal = "clean_eof" /\ CompleteCall /\ s.uiSettled) => s.callStored
WaiterKeepsContextIdentity == s.waiterArmed => s.waiterID = CallID
CheckpointPendingKeepsIdentity == s.checkpoint.pauseAsk => s.checkpoint.askID = CallID
NoInvalidCallPublication == s.callStored => s.sourceTerminal = "clean_eof" /\ CompleteCall
AcknowledgementKeepsContextIdentity == s.answerAcked => s.acceptedID = CallID
CheckpointAcknowledgementKeepsIdentity ==
    s.checkpoint.answerAcked => s.checkpoint.answerID = CallID
ExternalUnknownNotInventedSuccess ==
    (ToolKind = "external" /\ s.toolClosed /\ ~s.confirmedReceipt) => s.toolResult = "unknown"

(* Negative invariants used only as explicit non-vacuity reachability checks. *)
NeverExecutedBeforePublication == ~(s.adkEntered /\ ~s.callStored)
NeverAcceptedBeforePublication == ~(s.answerAcked /\ ~s.callStored)
NeverLateOwnerCancel == ~(s.sourceTerminal = "clean_eof" /\ s.adkEntered /\ s.ownerCancelled /\ ~s.uiSettled)
NeverExternalUnknown == ~(ToolKind = "external" /\ s.toolClosed /\ s.toolResult = "unknown")
NeverErrorDrain == ~(SourceOutcome = "source_error" /\ s.uiSettled /\ ~s.adkEntered)
NeverIncompleteDrain == ~(~CompleteCall /\ s.uiSettled /\ ~s.adkEntered)
NeverPendingCheckpoint == ~(s.checkpoint.pauseAsk /\ s.checkpoint.askID = CallID /\ s.checkpoint.historyCall)
NeverFinishLateDrain == ~(FinishPresent /\ s.ownerCancelled /\ s.uiSettled /\ s.callStored)
=============================================================================
