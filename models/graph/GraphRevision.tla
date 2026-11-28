--------------------------- MODULE GraphRevision ---------------------------
(* Committed revisions and the picture a client will keep. C-060.

   A committed batch and a clear of an existing graph each advance the
   revision by one. A refused batch does not. A graph that has never been
   written stays at revision 0 when cleared. The client replaces its picture
   only with a newer revision, so the empty picture after a clear is visible.
   A preview does not spend a committed revision, and a new revision drops it.

   RefuseAdvances counts a refused batch. ClearStays reuses the revision it
   deleted, so a client that already showed it keeps the old picture.
   ApplyOlder lets a stale read replace a newer picture. PreviewBumpsRevision
   spends a committed revision on an uncommitted frame. *)
EXTENDS Naturals

CONSTANTS MaxRev, RefuseAdvances, ClearStays, ApplyOlder, PreviewBumpsRevision

VARIABLES rev, occupied, shown, peak, stale, refusedOnce, refuseAdvanced,
          clearedFrom, clearedTo, preview, previewRev

vars == <<rev, occupied, shown, peak, stale, refusedOnce, refuseAdvanced,
          clearedFrom, clearedTo, preview, previewRev>>

Init ==
    /\ rev = 0 /\ occupied = FALSE /\ shown = 0 /\ peak = 0 /\ stale = 0
    /\ refusedOnce = FALSE /\ refuseAdvanced = FALSE
    /\ clearedFrom = 0 /\ clearedTo = 0
    /\ preview = 0 /\ previewRev = 0

Commit ==
    /\ rev < MaxRev
    /\ rev' = rev + 1 /\ occupied' = TRUE /\ preview' = 0
    /\ UNCHANGED <<shown, peak, stale, refusedOnce, refuseAdvanced, clearedFrom, clearedTo, previewRev>>

Refuse ==
    /\ rev' = IF (RefuseAdvances /\ rev < MaxRev) THEN rev + 1 ELSE rev
    /\ refuseAdvanced' = (rev' # rev)
    /\ refusedOnce' = TRUE /\ preview' = 0
    /\ UNCHANGED <<occupied, shown, peak, stale, clearedFrom, clearedTo, previewRev>>

Clear ==
    /\ occupied \/ rev > 0
    /\ ClearStays \/ rev < MaxRev
    /\ rev' = IF ClearStays THEN rev ELSE rev + 1
    /\ occupied' = FALSE /\ preview' = 0
    /\ clearedFrom' = rev /\ clearedTo' = rev'
    /\ UNCHANGED <<shown, peak, stale, refusedOnce, refuseAdvanced, previewRev>>

Capture ==
    /\ stale' = rev
    /\ UNCHANGED <<rev, occupied, shown, peak, refusedOnce, refuseAdvanced, clearedFrom, clearedTo, preview, previewRev>>

ApplyFresh ==
    /\ rev > shown
    /\ shown' = rev /\ peak' = rev
    /\ UNCHANGED <<rev, occupied, stale, refusedOnce, refuseAdvanced, clearedFrom, clearedTo, preview, previewRev>>

TakeOlder ==
    /\ ApplyOlder /\ stale < peak
    /\ shown' = stale
    /\ UNCHANGED <<rev, occupied, peak, stale, refusedOnce, refuseAdvanced, clearedFrom, clearedTo, preview, previewRev>>

ShowPreview ==
    /\ preview < 2
    /\ preview' = preview + 1
    /\ IF (PreviewBumpsRevision /\ rev < MaxRev)
       THEN /\ rev' = rev + 1 /\ previewRev' = rev
       ELSE /\ UNCHANGED rev /\ previewRev' = rev
    /\ UNCHANGED <<occupied, shown, peak, stale, refusedOnce, refuseAdvanced, clearedFrom, clearedTo>>

Next == Commit \/ Refuse \/ Clear \/ Capture \/ ApplyFresh \/ TakeOlder \/ ShowPreview

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ rev \in 0..MaxRev /\ shown \in 0..MaxRev /\ peak \in 0..MaxRev
    /\ stale \in 0..MaxRev /\ clearedFrom \in 0..MaxRev /\ clearedTo \in 0..MaxRev
    /\ preview \in 0..2 /\ previewRev \in 0..MaxRev
    /\ occupied \in BOOLEAN /\ refusedOnce \in BOOLEAN /\ refuseAdvanced \in BOOLEAN

RefusedDoesNotAdvance == ~refuseAdvanced
ClearAdvances == clearedFrom = 0 \/ clearedTo = clearedFrom + 1
ClientKeepsNewer == shown = peak
PreviewIsNotRevision == preview = 0 \/ previewRev = rev

NeverShownCommit == ~(occupied /\ rev > 0 /\ shown = rev /\ peak = rev)
NeverShownClear ==
    ~(rev > 0 /\ ~occupied /\ clearedTo = rev /\ shown = rev /\ clearedTo = clearedFrom + 1)
NeverRefused == ~(refusedOnce /\ ~refuseAdvanced)

=============================================================================
