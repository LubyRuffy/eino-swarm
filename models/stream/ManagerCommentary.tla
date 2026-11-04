-------------------------- MODULE ManagerCommentary --------------------------
EXTENDS Naturals, Sequences

(* Two completed manager generations that each request a tool. A classifier
   decision is an input, not a claim about its accuracy. Complete ordinary
   commentary can enter the shared durable/live order before its own tool.
   Claims and unknowns remain private while tools run. This model covers the
   admission boundary; EventStream covers the delivery of admitted rows. *)
CONSTANTS
    DeferOrdinary,
    PublishPrivate,
    KeepPrivateInHistory,
    AcceptWithoutEvidence,
    PublishAfterStop

Generations == 1..2
Kinds == {"unclassified", "ordinary", "claim", "unknown"}
Phases == {"candidate", "classified", "tool", "terminal", "stopped"}
Prose(g) == <<g, "prose">>
Tool(g) == <<g, "tool">>
Events == {Prose(g) : g \in Generations} \cup {Tool(g) : g \in Generations}

VARIABLES generation, phase, kind, public, history, plans, receipts, admitted, stopSize
vars == <<generation, phase, kind, public, history, plans, receipts, admitted, stopSize>>

Has(event) == \E i \in DOMAIN public : public[i] = event
Before(a, b) == \E i,j \in DOMAIN public : i < j /\ public[i] = a /\ public[j] = b
HasEvidence(g) == g \in plans /\ g \in receipts

Init ==
    /\ generation = 1 /\ phase = "candidate"
    /\ kind = [g \in Generations |-> "unclassified"]
    /\ public = <<>> /\ history = {}
    /\ plans = {} /\ receipts = {} /\ admitted = {}
    /\ stopSize = 0

Classify ==
    /\ phase = "candidate" /\ phase' = "classified"
    /\ \E k \in Kinds \ {"unclassified"} : kind' = [kind EXCEPT ![generation] = k]
    /\ UNCHANGED <<generation, public, history, plans, receipts, admitted, stopSize>>

PublishProse ==
    /\ phase = "classified" /\ ~Has(Prose(generation))
    /\ (kind[generation] = "ordinary" /\ ~DeferOrdinary) \/ PublishPrivate
    /\ public' = Append(public, Prose(generation))
    /\ UNCHANGED <<generation, phase, kind, history, plans, receipts, admitted, stopSize>>

PublishTool ==
    /\ phase = "classified" /\ phase' = "tool"
    /\ kind[generation] # "ordinary" \/ Has(Prose(generation)) \/ DeferOrdinary
    /\ public' = Append(public, Tool(generation))
    /\ UNCHANGED <<generation, kind, history, plans, receipts, admitted, stopSize>>

(* Evidence may arrive while executing the tool. Only matching evidence for
   this candidate is modeled; another generation's evidence cannot be used. *)
RecordPlan ==
    /\ phase = "tool" /\ generation \notin plans
    /\ plans' = plans \cup {generation}
    /\ UNCHANGED <<generation, phase, kind, public, history, receipts, admitted, stopSize>>

RecordReceipt ==
    /\ phase = "tool" /\ generation \notin receipts
    /\ receipts' = receipts \cup {generation}
    /\ UNCHANGED <<generation, phase, kind, public, history, plans, admitted, stopSize>>

Advance ==
    /\ phase = "tool"
    /\ IF generation = 1
       THEN /\ generation' = 2 /\ phase' = "candidate"
            /\ history' = IF (kind[generation] = "ordinary" /\ Has(Prose(generation)))
                            \/ KeepPrivateInHistory
                          THEN history \cup {generation} ELSE history
       ELSE /\ phase' = "terminal" /\ UNCHANGED <<generation, history>>
    /\ UNCHANGED <<kind, public, plans, receipts, admitted, stopSize>>

TerminalProse ==
    /\ phase = "terminal"
    /\ \E g \in Generations :
         /\ ~Has(Prose(g))
         /\ (kind[g] = "claim" /\ (HasEvidence(g) \/ AcceptWithoutEvidence))
             \/ (kind[g] = "ordinary" /\ DeferOrdinary)
         /\ public' = Append(public, Prose(g))
         /\ admitted' = IF kind[g] = "claim" THEN admitted \cup {g} ELSE admitted
    /\ UNCHANGED <<generation, phase, kind, history, plans, receipts, stopSize>>

Stop ==
    /\ phase # "stopped" /\ phase' = "stopped"
    /\ stopSize' = Len(public)
    /\ UNCHANGED <<generation, kind, public, history, plans, receipts, admitted>>

LeakAfterStop ==
    /\ PublishAfterStop /\ phase = "stopped"
    /\ ~Has(Prose(generation)) /\ kind[generation] = "ordinary"
    /\ public' = Append(public, Prose(generation))
    /\ UNCHANGED <<generation, phase, kind, history, plans, receipts, admitted, stopSize>>

Next == Classify \/ PublishProse \/ PublishTool \/ RecordPlan \/ RecordReceipt
        \/ Advance \/ TerminalProse \/ Stop \/ LeakAfterStop
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ generation \in Generations /\ phase \in Phases
    /\ kind \in [Generations -> Kinds]
    /\ public \in Seq(Events) /\ Len(public) <= 4
    /\ history \subseteq Generations /\ plans \subseteq Generations
    /\ receipts \subseteq Generations /\ admitted \subseteq Generations
    /\ stopSize \in 0..4

OrdinaryBeforeTool == \A g \in Generations :
    (kind[g] = "ordinary" /\ Has(Tool(g))) => Before(Prose(g), Tool(g))
PrivateProseNotPublic == \A g \in Generations :
    Has(Prose(g)) => (kind[g] = "ordinary" \/ g \in admitted)
PrivateProseNotInHistory == \A g \in history :
    kind[g] = "ordinary" /\ Has(Prose(g))
ClaimAdmissionHasEvidence == \A g \in admitted : HasEvidence(g)
StopKeepsPublishedPrefix == phase = "stopped" => Len(public) = stopSize

NeverTwoOrdinaryRounds ==
    ~(/\ kind[1] = "ordinary" /\ kind[2] = "ordinary"
      /\ public = <<Prose(1), Tool(1), Prose(2), Tool(2)>> /\ history = {1})

NeverPrivateClaimAndToolProgress ==
    ~(/\ kind[1] = "claim" /\ generation = 2 /\ phase = "candidate"
      /\ Has(Tool(1)) /\ ~Has(Prose(1)) /\ 1 \notin history /\ ~HasEvidence(1))

NeverTerminalVerifiedClaim ==
    ~(phase = "terminal" /\ 1 \in admitted /\ Has(Prose(1)) /\ HasEvidence(1))

=============================================================================
