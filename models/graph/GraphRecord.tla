---------------------------- MODULE GraphRecord ----------------------------
(* One recording pass before a turn's answer is kept.

   A writable turn with tool results that are not on the graph withholds that
   answer once. The next answer is kept even when the pass wrote nothing. The
   host does not invent the subjects. A read-only turn, a graph that is off,
   an unreadable allowance, an unreadable count and a failed allowance write
   all publish. Restart keeps a stored allowance and does not grant another pass.

   SecondPass withholds again after the first pass. DropStoredPass forgets the
   stored allowance at restart. HoldReadOnly, HoldUnreadable and HoldUncounted
   withhold an answer the turn should keep. HoldUnstored withholds despite a
   failed allowance write. InventOnPublish marks the graph recorded without a
   committed update_graph. *)
EXTENDS Naturals

CONSTANTS SecondPass, HoldReadOnly, HoldUnreadable, HoldUncounted,
          HoldUnstored, InventOnPublish, DropStoredPass

Modes == {"off", "read", "write"}
Reads == {"ok", "bad"}

VARIABLES mode, log, tally, persist, unrecorded, withholds, phase, recorded, seen,
          storedPass, memory

vars == <<mode, log, tally, persist, unrecorded, withholds, phase, recorded, seen,
          storedPass, memory>>

Init ==
    /\ mode = "write" /\ log = "ok" /\ tally = "ok" /\ persist = "ok"
    /\ unrecorded = FALSE /\ withholds = 0 /\ phase = "setup"
    /\ recorded = FALSE /\ seen = FALSE
    /\ storedPass = FALSE /\ memory = FALSE

Setup ==
    /\ phase = "setup"
    /\ \E m \in Modes, a \in Reads, n \in Reads, p \in Reads :
        /\ mode' = m /\ log' = a /\ tally' = n /\ persist' = p
    /\ phase' = "open"
    /\ UNCHANGED <<unrecorded, withholds, recorded, seen, storedPass, memory>>

Produce ==
    /\ phase = "open" /\ ~unrecorded
    /\ unrecorded' = TRUE
    /\ UNCHANGED <<mode, log, tally, persist, withholds, phase, recorded, seen, storedPass, memory>>

Record ==
    /\ unrecorded /\ phase \in {"open", "held"}
    /\ unrecorded' = FALSE /\ recorded' = TRUE /\ seen' = TRUE
    /\ UNCHANGED <<mode, log, tally, persist, withholds, phase, storedPass, memory>>

due ==
    /\ mode = "write" /\ log = "ok" /\ tally = "ok" /\ unrecorded
    /\ IF SecondPass /\ phase = "held" THEN TRUE
       ELSE ~storedPass /\ ~memory

extra ==
    \/ (HoldReadOnly /\ mode = "read" /\ unrecorded /\ ~memory)
    \/ (HoldUnreadable /\ log = "bad" /\ mode = "write" /\ unrecorded /\ ~memory)
    \/ (HoldUncounted /\ tally = "bad" /\ mode = "write" /\ unrecorded /\ ~memory)

Answer ==
    /\ phase \in {"open", "held"}
    /\ IF (due \/ extra) /\ (persist = "ok" \/ HoldUnstored) /\ withholds < 2
       THEN /\ withholds' = withholds + 1
            /\ memory' = TRUE /\ storedPass' = (persist = "ok") /\ phase' = "held"
            /\ UNCHANGED <<mode, log, tally, persist, unrecorded, recorded, seen>>
       ELSE /\ phase' = "published"
            /\ recorded' = IF (InventOnPublish /\ ~seen) THEN TRUE ELSE recorded
            /\ UNCHANGED <<mode, log, tally, persist, unrecorded, withholds, seen, storedPass, memory>>

Restart ==
    /\ phase \in {"held", "published"}
    /\ memory \/ (DropStoredPass /\ storedPass)
    /\ memory' = FALSE
    /\ IF DropStoredPass
       THEN /\ storedPass' = FALSE /\ phase' = "open"
       ELSE UNCHANGED <<storedPass, phase>>
    /\ UNCHANGED <<mode, log, tally, persist, unrecorded, withholds, recorded, seen>>

Next == Setup \/ Produce \/ Record \/ Answer \/ Restart

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ mode \in Modes /\ log \in Reads /\ tally \in Reads /\ persist \in Reads
    /\ unrecorded \in BOOLEAN /\ recorded \in BOOLEAN /\ seen \in BOOLEAN
    /\ storedPass \in BOOLEAN /\ memory \in BOOLEAN
    /\ withholds \in 0..2
    /\ phase \in {"setup", "open", "held", "published"}

OneRecordingPass == withholds <= 1
QuietModePublishes == mode \in {"off", "read"} => withholds = 0
UnreadablePublishes == log = "bad" => withholds = 0
UncountedPublishes == tally = "bad" => withholds = 0
UnstoredPublishes == persist = "bad" => withholds = 0
HostDoesNotInvent == recorded => seen

NeverWithheld ==
    ~(phase = "held" /\ withholds = 1 /\ mode = "write" /\ unrecorded /\ ~recorded /\ storedPass)
NeverPublishedDirty ==
    ~(phase = "published" /\ withholds = 1 /\ unrecorded /\ ~recorded)
NeverQuietPublished ==
    ~(mode = "read" /\ phase = "published" /\ withholds = 0 /\ unrecorded)
NeverPublishedAfterFailedWrite ==
    ~(mode = "write" /\ log = "ok" /\ tally = "ok" /\ persist = "bad" /\
      phase = "published" /\ withholds = 0 /\ unrecorded)

=============================================================================
