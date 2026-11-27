---------------------------- MODULE GraphStore ------------------------------
(* One owner's subject graph written by several workers at once.

   A write is one tool call with a stable call id. The store merges attributes
   additively without a version, requires the current version to overwrite or
   to change status, replays a repeated call only when its body is identical,
   refuses a write that reaches two distinct subjects, never changes an identity
   key and writes nothing once its owner has been deleted. *)
EXTENDS Naturals

CONSTANTS MaxVersion, SkipVersionCheck, ReplayChangedBody, MergeTwoSubjects,
          ConfirmWithoutEvidence, RewriteIdentity, WriteAfterDelete, AdditiveOverwrites

VARIABLES sub, owner, receipts, pending, flags

vars == <<sub, owner, receipts, pending, flags>>

Attrs == {"a", "b"}
Bodies == {"x", "y"}
Kinds == {"add", "overwrite", "confirm", "both", "rekey"}
CallIds == {1, 2}

Subject == [version: 0..MaxVersion, attrs: SUBSET Attrs, status: {"lead", "confirmed"},
            evidenced: BOOLEAN, key: {"k1", "k2"}]
Request == [kind: Kinds \cup {"none"}, id: CallIds, body: Bodies, expected: 0..MaxVersion,
            evidence: BOOLEAN]
Receipt == [id: CallIds, body: Bodies]
Flags == [staleOverwrite: BOOLEAN, mismatchReplayed: BOOLEAN, silentMerge: BOOLEAN,
          keyChanged: BOOLEAN, lateWrite: BOOLEAN, attrsLost: BOOLEAN,
          replayed: BOOLEAN, refusedStale: BOOLEAN, refusedMerge: BOOLEAN]

NoRequest == [kind |-> "none", id |-> 1, body |-> "x", expected |-> 0, evidence |-> FALSE]
NoFlags == [staleOverwrite |-> FALSE, mismatchReplayed |-> FALSE, silentMerge |-> FALSE,
            keyChanged |-> FALSE, lateWrite |-> FALSE, attrsLost |-> FALSE,
            replayed |-> FALSE, refusedStale |-> FALSE, refusedMerge |-> FALSE]

Init ==
    /\ sub = [version |-> 1, attrs |-> {}, status |-> "lead", evidenced |-> FALSE, key |-> "k1"]
    /\ owner = "live" /\ receipts = {} /\ pending = NoRequest /\ flags = NoFlags

(* Preparing a call grants nothing. It records the version the model saw. *)
Capture(kind, id, body, evidence) ==
    /\ pending.kind = "none" /\ owner \in {"live", "deleted"}
    /\ pending' = [kind |-> kind, id |-> id, body |-> body, expected |-> sub.version,
                   evidence |-> evidence]
    /\ UNCHANGED <<sub, owner, receipts, flags>>

(* A second worker's additive write moves the version under the first call. *)
CompetingWrite ==
    /\ pending.kind # "none" /\ owner = "live" /\ sub.version < MaxVersion
    /\ sub' = [sub EXCEPT !.version = @ + 1, !.attrs = @ \cup {"b"}]
    /\ UNCHANGED <<owner, receipts, pending, flags>>

DeleteOwner ==
    /\ owner = "live"
    /\ owner' = "deleted"
    /\ UNCHANGED <<sub, receipts, pending, flags>>

Replayed == \E r \in receipts : r.id = pending.id
SameBody == \E r \in receipts : r.id = pending.id /\ r.body = pending.body

Apply(kind) ==
    CASE kind = "add" ->
           [sub EXCEPT !.version = @ + 1,
                       !.attrs = IF AdditiveOverwrites THEN {IF pending.body = "x" THEN "a" ELSE "b"}
                                 ELSE @ \cup {IF pending.body = "x" THEN "a" ELSE "b"}]
      [] kind = "overwrite" ->
           [sub EXCEPT !.version = @ + 1, !.attrs = {IF pending.body = "x" THEN "a" ELSE "b"}]
      [] kind = "confirm" ->
           [sub EXCEPT !.version = @ + 1, !.status = "confirmed",
                       !.evidenced = @ \/ pending.evidence]
      [] kind = "rekey" -> [sub EXCEPT !.version = @ + 1, !.key = "k2"]
      [] OTHER -> [sub EXCEPT !.version = @ + 1]

Finish ==
    LET kind == pending.kind
        fresh == pending.expected = sub.version
        needsVersion == kind \in {"overwrite", "confirm"}
        versionOK == ~needsVersion \/ fresh \/ SkipVersionCheck
        evidenceOK == kind # "confirm" \/ pending.evidence \/ ConfirmWithoutEvidence
        keyOK == kind # "rekey" \/ RewriteIdentity
        mergeOK == kind # "both" \/ MergeTwoSubjects
        ownerOK == owner = "live" \/ WriteAfterDelete
        admitted == ownerOK /\ versionOK /\ evidenceOK /\ keyOK /\ mergeOK
        next == Apply(kind)
    IN /\ kind # "none" /\ sub.version < MaxVersion
       /\ IF Replayed
          THEN /\ flags' = [flags EXCEPT !.mismatchReplayed = @ \/ (~SameBody /\ ReplayChangedBody),
                                         !.replayed = @ \/ SameBody]
               /\ UNCHANGED <<sub, receipts>>
          ELSE /\ sub' = IF admitted THEN next ELSE sub
               /\ receipts' = IF admitted THEN receipts \cup {[id |-> pending.id, body |-> pending.body]}
                              ELSE receipts
               /\ flags' = [flags EXCEPT
                   !.staleOverwrite = @ \/ (admitted /\ needsVersion /\ ~fresh),
                   !.silentMerge = @ \/ (admitted /\ kind = "both"),
                   !.keyChanged = @ \/ (admitted /\ kind = "rekey"),
                   !.lateWrite = @ \/ (admitted /\ owner = "deleted"),
                   !.attrsLost = @ \/ (admitted /\ kind = "add" /\ ~(sub.attrs \subseteq next.attrs)),
                   !.refusedStale = @ \/ (~admitted /\ needsVersion /\ ~fresh /\ ownerOK),
                   !.refusedMerge = @ \/ (~admitted /\ kind = "both")]
       /\ pending' = NoRequest
       /\ UNCHANGED owner

Next ==
    \/ CompetingWrite \/ DeleteOwner \/ Finish
    \/ \E k \in Kinds, id \in CallIds, b \in Bodies, e \in BOOLEAN : Capture(k, id, b, e)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ sub \in Subject /\ owner \in {"live", "deleted"} /\ receipts \subseteq Receipt
    /\ pending \in Request /\ flags \in Flags

NoLostUpdate == ~flags.staleOverwrite
ReplayNeedsSameBody == ~flags.mismatchReplayed
RefusesTwoSubjects == ~flags.silentMerge
ConfirmedHasEvidence == sub.status = "confirmed" => sub.evidenced
IdentityKeyStable == ~flags.keyChanged /\ sub.key = "k1"
NoWriteAfterDelete == ~flags.lateWrite
AdditiveKeepsAttributes == ~flags.attrsLost

NeverConfirmedWithEvidence == ~(sub.status = "confirmed" /\ sub.evidenced)
NeverReplayedIdentical == ~flags.replayed
NeverRefusedStale == ~flags.refusedStale
NeverRefusedTwoSubjects == ~flags.refusedMerge
NeverOwnerDeleted == ~(owner = "deleted")

=============================================================================
