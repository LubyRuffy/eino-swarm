------------------------------- MODULE SideChat -------------------------------
EXTENDS Naturals, Sequences, FiniteSets

(* A request can settle after cancellation or a different panel opens. Its
   identity is never reused within this bound. Task steps represent legitimate
   main progress and answers independently of the side request's outcomes. *)
CONSTANTS
    UseMainSend,
    RebindContext,
    SkipReplyOwner,
    EnableSideTools,
    HoldMainDuringModel

Threads == {"a", "b"}
NoThread == "none"
Requests == 1..2
NoRequest == 0
TaskPhases == {"running", "paused", "done"}
RequestPhases == {"unused", "sent", "model", "ok", "failed"}
MainState == [phase: TaskPhases, seq: 0..2, queued: BOOLEAN]

VARIABLES selected, panel, active, answers, failure, req, main,
          mainChangedBySide, staleApplied, toolsExposed, lateDropped

vars == <<selected, panel, active, answers, failure, req, main,
          mainChangedBySide, staleApplied, toolsExposed, lateDropped>>
mainVars == <<main, mainChangedBySide>>
ui == <<selected, panel, active, answers, failure>>

Init ==
    /\ selected = "a"
    /\ panel = FALSE /\ active = NoRequest /\ answers = <<>> /\ failure = NoRequest
    /\ req = [id \in Requests |-> [thread |-> NoThread, contextThread |-> NoThread,
                                  phase |-> "unused", mainAtModel |-> "running",
                                  progressed |-> FALSE, afterError |-> FALSE]]
    /\ main = [thread \in Threads |-> [phase |-> "running", seq |-> 0, queued |-> FALSE]]
    /\ mainChangedBySide = FALSE /\ staleApplied = FALSE
    /\ toolsExposed = FALSE /\ lateDropped = FALSE

AdvanceMain(thread) ==
    /\ main[thread].phase # "done"
    /\ (~HoldMainDuringModel
        \/ ~\E id \in Requests : req[id].thread = thread /\ req[id].phase = "model")
    /\ main' = [main EXCEPT
                    ![thread].phase = IF @ = "running" THEN "paused" ELSE "done",
                    ![thread].seq = @ + 1]
    /\ UNCHANGED <<ui, req, mainChangedBySide, staleApplied, toolsExposed, lateDropped>>

Open ==
    /\ ~panel /\ panel' = TRUE
    /\ UNCHANGED <<selected, active, answers, failure, req, mainVars,
                   staleApplied, toolsExposed, lateDropped>>

DiscardPanel(thread) ==
    /\ panel' = FALSE /\ selected' = thread
    /\ active' = NoRequest /\ answers' = <<>> /\ failure' = NoRequest
    /\ UNCHANGED <<req, mainVars, staleApplied, toolsExposed, lateDropped>>

Close == panel /\ DiscardPanel(selected)
Navigate(thread) == thread # selected /\ DiscardPanel(thread)

Cancel ==
    /\ active # NoRequest
    /\ active' = NoRequest
    /\ UNCHANGED <<selected, panel, answers, failure, req, mainVars,
                   staleApplied, toolsExposed, lateDropped>>

Send(id) ==
    /\ panel /\ active = NoRequest /\ req[id].phase = "unused"
    /\ active' = id /\ failure' = NoRequest
    /\ req' = [req EXCEPT ![id] = [thread |-> selected, contextThread |-> NoThread,
                                    phase |-> "sent", mainAtModel |-> "running",
                                    progressed |-> FALSE, afterError |-> failure # NoRequest]]
    /\ main' = IF UseMainSend
               THEN [main EXCEPT ![selected].queued = TRUE] ELSE main
    /\ mainChangedBySide' = (mainChangedBySide \/ main' # main)
    /\ UNCHANGED <<selected, panel, answers, staleApplied, toolsExposed, lateDropped>>

(* Read after dispatch, when navigation may already have happened. Data is
   indexed by the route's captured thread. No worker tool surface is installed. *)
BeginModel(id) ==
    /\ req[id].phase = "sent"
    /\ req' = [req EXCEPT ![id].phase = "model",
                ![id].contextThread = IF RebindContext THEN selected ELSE req[id].thread,
                ![id].mainAtModel = main[req[id].thread].phase]
    /\ toolsExposed' = (toolsExposed \/ EnableSideTools)
    /\ UNCHANGED <<ui, mainVars, staleApplied, lateDropped>>

Owner(id) == panel /\ active = id /\ selected = req[id].thread

Settle(id, ok) ==
    /\ (req[id].phase = "model" \/ (~ok /\ req[id].phase = "sent"))
    /\ req' = [req EXCEPT ![id].phase = IF ok THEN "ok" ELSE "failed",
                ![id].progressed = (req[id].phase = "model" /\ main[req[id].thread].phase # req[id].mainAtModel)]
    /\ LET apply == Owner(id) \/ SkipReplyOwner
       IN /\ active' = IF apply THEN NoRequest ELSE active
          /\ answers' = IF apply /\ ok THEN Append(answers, id) ELSE answers
          /\ failure' = IF apply /\ ~ok THEN id ELSE failure
          /\ staleApplied' = (staleApplied \/ (apply /\ ~Owner(id)))
    /\ lateDropped' = (lateDropped \/ (~Owner(id) /\ active # NoRequest))
    /\ UNCHANGED <<selected, panel, mainVars, toolsExposed>>

Next ==
    \/ Open \/ Close \/ Cancel
    \/ \E thread \in Threads : Navigate(thread) \/ AdvanceMain(thread)
    \/ \E id \in Requests : Send(id) \/ BeginModel(id)
                            \/ Settle(id, TRUE) \/ Settle(id, FALSE)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ selected \in Threads /\ panel \in BOOLEAN
    /\ active \in Requests \cup {NoRequest} /\ failure \in Requests \cup {NoRequest}
    /\ answers \in Seq(Requests) /\ Len(answers) <= Cardinality(Requests)
    /\ req \in [Requests -> [thread: Threads \cup {NoThread}, contextThread: Threads \cup {NoThread},
                             phase: RequestPhases, mainAtModel: TaskPhases,
                             progressed: BOOLEAN, afterError: BOOLEAN]]
    /\ main \in [Threads -> MainState]
    /\ mainChangedBySide \in BOOLEAN /\ staleApplied \in BOOLEAN
    /\ toolsExposed \in BOOLEAN /\ lateDropped \in BOOLEAN

SideLeavesMainUntouched == ~mainChangedBySide
ModelReadsBoundThread == \A id \in Requests :
    (req[id].phase \in {"model", "ok"} \/ req[id].contextThread # NoThread) =>
        req[id].thread = req[id].contextThread
ReplyMatchesActiveOwner == ~staleApplied
SideHasNoTools == ~toolsExposed
MainCanAdvanceWhileSideWaits == \A id \in Requests :
    LET thread == req[id].thread
    IN IF req[id].phase = "model" THEN
           main[thread].phase # "done" => ENABLED AdvanceMain(thread)
       ELSE TRUE

NeverAnswerAcrossMainProgress ==
    ~(\E i \in DOMAIN answers : req[answers[i]].progressed)
NeverTwoSideAnswers == Len(answers) < 2
NeverLateReplyWithNewRequest == ~lateDropped
NeverRecoverAfterSideFailure ==
    ~(\E i \in DOMAIN answers : req[answers[i]].afterError)

=============================================================================
