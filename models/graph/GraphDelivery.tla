---------------------------- MODULE GraphDelivery ----------------------------
(* A conversation can change from its own graph to a project graph while its
   stream and panel remain open. Revisions are local to each owner. This
   model starts after the initial stream frame, holds one old read across an
   owner change, and allows a fresh read before or after that old read ends. *)
EXTENDS Naturals

CONSTANTS CompareRevisionOnly, KeepOldOwnerPicture, ApplyObsoleteRead

Owners == {"conversation", "project"}
Revisions == 0..2
Empty == [owner |-> "none", rev |-> 0]
Snapshot == [owner : Owners, rev : Revisions]

VARIABLES owner, revisions, polled, announced, shown, pending,
          obsolete, discarded, lastSwitch

vars == <<owner, revisions, polled, announced, shown, pending,
          obsolete, discarded, lastSwitch>>

Current == [owner |-> owner, rev |-> revisions[owner]]

Init ==
    /\ owner = "conversation"
    /\ revisions \in [Owners -> 0..1]
    /\ revisions[owner] = 1
    /\ polled = Current /\ announced = Current /\ shown = Current
    /\ pending = Empty /\ obsolete = FALSE /\ discarded = FALSE
    /\ lastSwitch = "none"

Move ==
    /\ owner' \in Owners \ {owner}
    /\ UNCHANGED <<revisions, polled, announced, shown, pending,
                  obsolete, discarded, lastSwitch>>

Commit ==
    /\ \E o \in Owners :
         /\ revisions[o] < 2
         /\ revisions' = [revisions EXCEPT ![o] = @ + 1]
    /\ UNCHANGED <<owner, polled, announced, shown, pending,
                  obsolete, discarded, lastSwitch>>

Poll ==
    /\ Current # polled
    /\ polled' = Current
    /\ announced' = IF CompareRevisionOnly /\ Current.rev = announced.rev
                    THEN announced ELSE Current
    /\ IF announced'.owner # announced.owner
       THEN /\ shown' = IF KeepOldOwnerPicture THEN shown ELSE Empty
            /\ obsolete' = (pending # Empty)
            /\ lastSwitch' = IF announced'.rev = announced.rev THEN "equal"
                              ELSE IF announced'.rev < announced.rev THEN "lower"
                              ELSE "higher"
       ELSE UNCHANGED <<shown, obsolete, lastSwitch>>
    /\ UNCHANGED <<owner, revisions, pending, discarded>>

BeginDelayedRead ==
    /\ owner = announced.owner /\ pending = Empty
    /\ pending' = Current /\ obsolete' = FALSE
    /\ UNCHANGED <<owner, revisions, polled, announced, shown, discarded, lastSwitch>>

FreshRead ==
    /\ owner = announced.owner
    /\ shown' = Current
    /\ UNCHANGED <<owner, revisions, polled, announced, pending,
                  obsolete, discarded, lastSwitch>>

FinishDelayedRead ==
    /\ pending # Empty
    /\ IF obsolete /\ ~ApplyObsoleteRead
       THEN /\ UNCHANGED shown /\ discarded' = TRUE
       ELSE /\ shown' = IF obsolete \/ shown = Empty \/ pending.rev >= shown.rev
                        THEN pending ELSE shown
            /\ UNCHANGED discarded
    /\ pending' = Empty /\ obsolete' = FALSE
    /\ UNCHANGED <<owner, revisions, polled, announced, lastSwitch>>

Next == Move \/ Commit \/ Poll \/ BeginDelayedRead \/ FreshRead \/ FinishDelayedRead
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ owner \in Owners /\ revisions \in [Owners -> Revisions]
    /\ polled \in Snapshot /\ announced \in Snapshot
    /\ shown \in Snapshot \cup {Empty} /\ pending \in Snapshot \cup {Empty}
    /\ obsolete \in BOOLEAN /\ discarded \in BOOLEAN
    /\ lastSwitch \in {"none", "equal", "lower", "higher"}

PollAnnouncesOwner == announced = polled
PictureBelongsToAnnouncedOwner == shown = Empty \/ shown.owner = announced.owner

NeverShowsEqualMove == ~(lastSwitch = "equal" /\ shown = announced /\ shown.owner = "project")
NeverShowsLowerMove == ~(lastSwitch = "lower" /\ shown = announced /\ shown.owner = "project")
NeverDiscardsOldRead == ~(discarded /\ shown = announced /\ shown.owner = "project")

=============================================================================
