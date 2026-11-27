---------------------- MODULE ProjectDistillation ----------------------
EXTENDS Naturals, FiniteSets

CONSTANTS AllowConcurrent, AllowForeignSource, ReceiptWithoutWrite

Runs == {1, 2}
Targets == {"notes", "skills"}
Sources == {"inside", "outside"}
VARIABLES active, started, proposed, written, receipts, finished
vars == <<active, started, proposed, written, receipts, finished>>

Init ==
    /\ active = {}
    /\ started = {}
    /\ proposed = [r \in Runs |-> "none"]
    /\ written = {}
    /\ receipts = {}
    /\ finished = {}

Start(r) ==
    /\ r \notin started
    /\ (AllowConcurrent \/ active = {})
    /\ active' = active \cup {r}
    /\ started' = started \cup {r}
    /\ UNCHANGED <<proposed, written, receipts, finished>>

Submit(r, s) ==
    /\ r \in active
    /\ proposed[r] = "none"
    /\ proposed' = [proposed EXCEPT ![r] = s]
    /\ UNCHANGED <<active, started, written, receipts, finished>>

Save(r, t, ok) ==
    /\ r \in active
    /\ proposed[r] \in Sources
    /\ (AllowForeignSource \/ proposed[r] = "inside")
    /\ <<r, t>> \notin receipts
    /\ written' = IF ok THEN written \cup {<<r, t>>} ELSE written
    /\ receipts' = IF ok \/ ReceiptWithoutWrite
                    THEN receipts \cup {<<r, t>>} ELSE receipts
    /\ UNCHANGED <<active, started, proposed, finished>>

Finish(r) ==
    /\ r \in active
    /\ active' = active \ {r}
    /\ finished' = finished \cup {r}
    /\ UNCHANGED <<started, proposed, written, receipts>>

Next == \E r \in Runs :
    Start(r) \/ Finish(r) \/ (\E s \in Sources : Submit(r, s))
      \/ (\E t \in Targets, ok \in BOOLEAN : Save(r, t, ok))

Spec == Init /\ [][Next]_vars
TypeOK ==
    /\ active \subseteq started
    /\ finished \subseteq started
    /\ started \subseteq Runs
    /\ proposed \in [Runs -> Sources \cup {"none"}]
    /\ written \subseteq Runs \X Targets
    /\ receipts \subseteq Runs \X Targets
OneManualRun == Cardinality(active) <= 1
ScopedWrites == \A pair \in written : proposed[pair[1]] = "inside"
RealReceipts == receipts \subseteq written
NeverBothTargets == ~ (\E r \in finished : \A t \in Targets : <<r, t>> \in receipts)
=============================================================================
