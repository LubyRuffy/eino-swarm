----------------------------- MODULE GraphReplay -----------------------------
(* Two independent model runs replay source turns under one extraction turn.
   Both runs can emit the same raw tool call ID. This model fixes that raw ID
   and varies the source and body, so it can also exercise a retry in one run.
   Admission is shared by append and replace. A currently pending human ask
   retains its answer slot, and neither a busy nor a planning conversation
   loses the committed graph on a refused request. A clear confirmed for one
   graph owner cannot clear another after a project move. *)

CONSTANTS IgnoreSourceRound, AcceptChangedBody, ExtractAnswersAsk,
          ClearBeforeAdmission, ClearInPlanning, IgnoreConfirmedOwner

Sources == {"first", "second"}
Bodies == {"x", "y"}
NoBody == "none"
Owners == {"conversation", "project"}

VARIABLES receipts, source, body, prior, outcome, phase, ask, graphPresent,
          rejected, owner, authorized

vars == <<receipts, source, body, prior, outcome, phase, ask, graphPresent,
          rejected, owner, authorized>>

Init ==
    /\ receipts = [s \in Sources |-> NoBody]
    /\ source = "first" /\ body = "x" /\ prior = NoBody /\ outcome = "none"
    /\ phase = "idle" /\ ask = "none" /\ graphPresent = TRUE /\ rejected = FALSE
    /\ owner = "conversation" /\ authorized = FALSE

Move ==
    /\ phase = "idle" /\ owner = "conversation" /\ owner' = "project"
    /\ UNCHANGED <<receipts, source, body, prior, outcome, phase, ask,
                  graphPresent, rejected, authorized>>

WaitForHuman ==
    /\ phase = "idle"
    /\ phase' = "busy" /\ ask' = "pending"
    /\ UNCHANGED <<receipts, source, body, prior, outcome, graphPresent, rejected, owner, authorized>>

Plan ==
    /\ phase = "idle" /\ phase' = "planning"
    /\ UNCHANGED <<receipts, source, body, prior, outcome, ask, graphPresent, rejected, owner, authorized>>

Extract(replace, expectedOwner) ==
    /\ CASE phase = "idle" ->
             /\ IF replace /\ expectedOwner # owner /\ ~IgnoreConfirmedOwner
                THEN /\ rejected' = TRUE /\ UNCHANGED <<phase, graphPresent, authorized>>
                ELSE /\ phase' = "extracting" /\ graphPresent' = ~replace
                     /\ authorized' = (expectedOwner = owner) /\ rejected' = FALSE
             /\ UNCHANGED ask
         [] phase = "busy" ->
             /\ ask' = IF ExtractAnswersAsk THEN "consumed-by-extraction" ELSE ask
             /\ graphPresent' = (graphPresent /\ ~(ClearBeforeAdmission /\ replace))
             /\ rejected' = ~ExtractAnswersAsk
             /\ UNCHANGED <<phase, authorized>>
         [] phase = "planning" ->
             /\ graphPresent' = (graphPresent /\ ~(ClearInPlanning /\ replace))
             /\ rejected' = ~ClearInPlanning
             /\ UNCHANGED <<phase, ask, authorized>>
         [] OTHER -> FALSE
    /\ UNCHANGED <<receipts, source, body, prior, outcome, owner>>

Attempt(s, b) ==
    LET effective == IF IgnoreSourceRound /\ receipts[s] = NoBody
                     THEN receipts[CHOOSE other \in Sources : other # s]
                     ELSE receipts[s]
    IN /\ phase = "extracting"
       /\ source' = s /\ body' = b /\ prior' = receipts[s]
       /\ CASE effective = NoBody ->
                 /\ receipts' = [receipts EXCEPT ![s] = b] /\ outcome' = "committed"
            [] effective = b \/ AcceptChangedBody ->
                 /\ UNCHANGED receipts /\ outcome' = "replayed"
            [] OTHER -> /\ UNCHANGED receipts /\ outcome' = "refused"
       /\ UNCHANGED <<phase, ask, graphPresent, rejected, owner, authorized>>

Next == Move \/ WaitForHuman \/ Plan \/ (\E replace \in BOOLEAN, expected \in Owners : Extract(replace, expected))
        \/ (\E s \in Sources, b \in Bodies : Attempt(s, b))
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ receipts \in [Sources -> Bodies \cup {NoBody}]
    /\ source \in Sources /\ body \in Bodies /\ prior \in Bodies \cup {NoBody}
    /\ outcome \in {"none", "committed", "replayed", "refused"}
    /\ phase \in {"idle", "busy", "planning", "extracting"}
    /\ ask \in {"none", "pending", "consumed-by-extraction"}
    /\ graphPresent \in BOOLEAN /\ rejected \in BOOLEAN
    /\ owner \in Owners /\ authorized \in BOOLEAN

BusyAskSurvives == phase = "busy" => ask = "pending"
BusyGraphSurvives == phase = "busy" => graphPresent
PlanningGraphSurvives == phase = "planning" => graphPresent
ClearHonorsConfirmedOwner == (phase = "extracting" /\ ~graphPresent) => authorized

NewSourceGetsItsOwnReceipt ==
    (outcome # "none" /\ prior = NoBody) => (outcome = "committed" /\ receipts[source] = body)

RetryUsesOriginalBody ==
    prior # NoBody => /\ outcome = (IF prior = body THEN "replayed" ELSE "refused")
                      /\ receipts[source] = prior

NeverBothSourcesCommit == ~(\A s \in Sources : receipts[s] \in Bodies)
NeverReplays == outcome # "replayed"
NeverRefusesChangedBody == outcome # "refused"
NeverRefusesBusy == ~(phase = "busy" /\ rejected)
NeverRefusesPlanning == ~(phase = "planning" /\ rejected)
NeverRefusesStaleOwner == ~(phase = "idle" /\ owner = "project" /\ rejected /\ graphPresent)
NeverClearsAfterAdmission == ~(phase = "extracting" /\ ~graphPresent)

=============================================================================
