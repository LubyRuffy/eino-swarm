---------------------------- MODULE EventStream ----------------------------
(***************************************************************************)
(* C-003 for one conversation and one live reader: the engine stores rows  *)
(* with sequence numbers and broadcasts them; the reader's SSE connection  *)
(* forwards each new row once, catches up from the database when its       *)
(* subscription lagged or when it reconnects with Last-Event-ID, and the   *)
(* page applies every row above the highest seq it has seen. A rewind      *)
(* deletes the rows from a user message on and tells readers where it cut. *)
(* The engine may restart, which reseeds its sequence counter from the     *)
(* database (store.nextEventSeq).                                          *)
(*                                                                         *)
(* Each Boolean constant puts back one earlier or plausible behaviour so   *)
(* the checker can show which invariant it breaks. All FALSE is the        *)
(* current design. tools/model_check.py maps every switch to its source.  *)
(***************************************************************************)
EXTENDS Naturals, Sequences, FiniteSets

CONSTANTS
    MarkerUnstored,       \* the rewind marker is broadcast with seq 0 and never stored
    MarkerAfterUnlock,    \* the rewind marker is broadcast after the record lock is released
    BroadcastAfterUnlock, \* a stored row is broadcast after the record lock is released
    DropWhenSlow,         \* a stored row that does not fit a slow reader is dropped unflagged
    MaxSeq,               \* bound on assigned sequence numbers
    Cap                   \* capacity of the reader's subscription channel

None == [seq |-> 0, cut |-> 0]
(* An event: an ordinary row has cut 0, a rewind marker the seq it cut from. *)
Events == [seq : 0..MaxSeq, cut : 0..MaxSeq]

VARIABLES
    stored,    \* rows in the database
    next,      \* the engine's in-memory sequence counter
    outbox,    \* stored rows not yet broadcast (only with BroadcastAfterUnlock)
    pending,   \* a rewind marker not yet broadcast (only with MarkerAfterUnlock)
    chan,      \* events queued on the reader's subscription
    lagged,    \* the subscription missed a stored row and must catch up
    up,        \* the reader is subscribed
    view,      \* seqs of the ordinary rows the reader shows
    highest,   \* highest seq the connection delivered (its SSE id)
    budget,    \* remaining rewinds, restarts and disconnects
    top,       \* highest sequence number ever assigned
    ghost      \* scenario tags for the reachability witnesses

vars == <<stored, next, outbox, pending, chan, lagged, up, view, highest, budget, top, ghost>>

Max(S) == IF S = {} THEN 0 ELSE CHOOSE x \in S : \A y \in S : y <= x
Seqs(S) == {e.seq : e \in S}
RowSeqs(S) == {e.seq : e \in {x \in S : x.cut = 0}}

(* The subscription channel is non-blocking (Engine.broadcast): a row that  *)
(* does not fit flags the reader as lagged; an event without a seq is lost. *)
Push(ev) ==
    IF ~up THEN UNCHANGED <<chan, lagged>>
    ELSE IF Len(chan) < Cap THEN chan' = Append(chan, ev) /\ UNCHANGED lagged
    ELSE /\ lagged' = (lagged \/ (ev.seq > 0 /\ ~DropWhenSlow))
         /\ UNCHANGED chan

(* The page applies a row above its highest seq; a marker without a seq,  *)
(* or one above highest, drops every shown row from its cut on.            *)
ApplyView(v, ev) ==
    IF ev.cut = 0 THEN v \cup {ev.seq} ELSE {s \in v : s < ev.cut}

(* Catch-up replays the rows stored after highest, in order. A row is kept *)
(* unless a later replayed marker cut it; shown rows unless any did.        *)
CatchUpView(v, h) ==
    LET r == {e \in stored : e.seq > h}
        cuts == {e \in r : e.cut > 0}
    IN {s \in v : \A m \in cuts : s < m.cut}
       \cup {e.seq : e \in {x \in r : x.cut = 0 /\ \A m \in cuts : ~(m.seq > x.seq /\ x.seq >= m.cut)}}

Init ==
    /\ stored = {}
    /\ next = 0
    /\ outbox = {}
    /\ pending = None
    /\ chan = <<>>
    /\ lagged = FALSE
    /\ up = TRUE
    /\ view = {}
    /\ highest = 0
    /\ budget = [rewind |-> 1, restart |-> 1, drop |-> 1]
    /\ top = 0
    /\ ghost = {}

(* Engine.record: assign, store and broadcast under recordMu. *)
Record ==
    /\ next < MaxSeq
    /\ LET ev == [seq |-> next + 1, cut |-> 0] IN
       /\ stored' = stored \cup {ev}
       /\ next' = next + 1
       /\ top' = Max({top, next + 1})
       /\ IF BroadcastAfterUnlock
          THEN outbox' = outbox \cup {ev} /\ UNCHANGED <<chan, lagged>>
          ELSE Push(ev) /\ UNCHANGED outbox
    /\ ghost' = IF budget.restart = 0 THEN ghost \cup {"recordAfterRestart"} ELSE ghost
    /\ UNCHANGED <<pending, up, view, highest, budget>>

Flush ==
    /\ \E ev \in outbox :
        /\ Push(ev)
        /\ outbox' = outbox \ {ev}
    /\ UNCHANGED <<stored, next, pending, up, view, highest, budget, top, ghost>>

(* Engine.applyRewind: truncate from a stored row, then announce the cut. *)
Rewind ==
    /\ budget.rewind > 0
    /\ pending = None
    /\ \E from \in RowSeqs(stored) :
        LET kept == {e \in stored : e.seq < from}
            marker == IF MarkerUnstored THEN [seq |-> 0, cut |-> from]
                      ELSE [seq |-> next + 1, cut |-> from]
        IN /\ MarkerUnstored \/ next < MaxSeq
           /\ stored' = IF MarkerUnstored THEN kept ELSE kept \cup {marker}
           /\ next' = IF MarkerUnstored THEN next ELSE next + 1
           /\ top' = IF MarkerUnstored THEN top ELSE Max({top, next + 1})
           /\ IF MarkerAfterUnlock
              THEN pending' = marker /\ UNCHANGED <<chan, lagged>>
              ELSE Push(marker) /\ UNCHANGED pending
    /\ budget' = [budget EXCEPT !.rewind = @ - 1]
    /\ UNCHANGED <<outbox, up, view, highest, ghost>>

EmitMarker ==
    /\ pending # None
    /\ Push(pending)
    /\ pending' = None
    /\ UNCHANGED <<stored, next, outbox, up, view, highest, budget, top, ghost>>

(* streamEvents: catch up first when lagged, skip a stored row already     *)
(* delivered, forward the rest; the page applies what arrives in order.    *)
Deliver ==
    /\ up /\ chan # <<>>
    /\ LET ev == Head(chan)
           v0 == IF lagged THEN CatchUpView(view, highest) ELSE view
           h0 == IF lagged THEN Max({highest} \cup Seqs(stored)) ELSE highest
       IN IF ev.seq > 0 /\ ev.seq <= h0
          THEN view' = v0 /\ highest' = h0
          ELSE view' = ApplyView(v0, ev) /\ highest' = Max({h0, ev.seq})
    /\ chan' = Tail(chan)
    /\ lagged' = FALSE
    /\ ghost' = IF lagged /\ \E e \in stored : e.seq > highest /\ e.cut > 0
                THEN ghost \cup {"catchUpAcrossRewind"} ELSE ghost
    /\ UNCHANGED <<stored, next, outbox, pending, up, budget, top>>

(* The lag ticker catches up even when no further event arrives. *)
CatchUp ==
    /\ up /\ lagged
    /\ view' = CatchUpView(view, highest)
    /\ highest' = Max({highest} \cup Seqs(stored))
    /\ lagged' = FALSE
    /\ ghost' = IF \E e \in stored : e.seq > highest /\ e.cut > 0
                THEN ghost \cup {"catchUpAcrossRewind"} ELSE ghost
    /\ UNCHANGED <<stored, next, outbox, pending, chan, up, budget, top>>

(* The connection drops; EventSource reconnects with Last-Event-ID. *)
Drop ==
    /\ up /\ budget.drop > 0
    /\ up' = FALSE /\ chan' = <<>> /\ lagged' = FALSE
    /\ budget' = [budget EXCEPT !.drop = @ - 1]
    /\ UNCHANGED <<stored, next, outbox, pending, view, highest, top, ghost>>

Reconnect ==
    /\ ~up
    /\ up' = TRUE
    /\ view' = CatchUpView(view, highest)
    /\ highest' = Max({highest} \cup Seqs(stored))
    /\ UNCHANGED <<stored, next, outbox, pending, chan, lagged, budget, top, ghost>>

(* The engine process restarts: unbroadcast work is gone, the counter is   *)
(* reseeded from the stored rows, and the reader must reconnect.            *)
Restart ==
    /\ budget.restart > 0
    /\ next' = Max(Seqs(stored))
    /\ outbox' = {} /\ pending' = None
    /\ up' = FALSE /\ chan' = <<>> /\ lagged' = FALSE
    /\ budget' = [budget EXCEPT !.restart = @ - 1]
    /\ UNCHANGED <<stored, view, highest, top, ghost>>

Next == Record \/ Flush \/ Rewind \/ EmitMarker \/ Deliver \/ CatchUp
        \/ Drop \/ Reconnect \/ Restart

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Safety *)

TypeOK ==
    /\ stored \subseteq Events /\ outbox \subseteq Events /\ pending \in Events
    /\ next \in 0..MaxSeq /\ highest \in 0..MaxSeq
    /\ Len(chan) <= Cap /\ view \subseteq 1..MaxSeq /\ top \in 0..MaxSeq

(* Once everything sent has arrived, the reader shows exactly the stored   *)
(* rows: none missing, none the database no longer has.                     *)
ConvergedView ==
    (up /\ chan = <<>> /\ ~lagged /\ outbox = {} /\ pending = None)
        => view = RowSeqs(stored)

(* A sequence number names one row for good: it is the reader's dedupe key *)
(* and its resume cursor, so the counter never falls below one it issued.   *)
SeqIdentity == next >= top

-----------------------------------------------------------------------------
(* Witnesses: each must be violated, showing its scenario is reachable. *)

NeverCatchUpAcrossRewind == "catchUpAcrossRewind" \notin ghost
NeverRecordAfterRestart == ~("recordAfterRestart" \in ghost /\ budget.rewind = 0 /\ up /\ chan = <<>>)

=============================================================================
