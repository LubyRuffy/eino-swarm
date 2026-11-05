----------------------------- MODULE AccountCatalog -----------------------------
EXTENDS TLC

(* The signed-in account's model catalog is published apart from the fetch that
   read it. Login and logout change the credential while a fetch is in flight.
   PublishedAgrees: a published catalog belongs to the current credential, or
   nothing is published. StaleObservation applies the credential observed at
   fetch time after it has changed. UnsealedLogout changes the credential and
   leaves the previous catalog in place. *)
CONSTANTS
    StaleObservation,
    UnsealedLogout

VARIABLES credential, published, observed, refresh

vars == <<credential, published, observed, refresh>>

Init ==
    /\ credential = "A" /\ published = "A"
    /\ observed = "none" /\ refresh = "idle"

SignOut ==
    /\ credential # "none"
    /\ credential' = "none"
    /\ published' = IF UnsealedLogout THEN published ELSE "none"
    /\ UNCHANGED <<observed, refresh>>

SignIn(token) ==
    /\ credential # token
    /\ credential' = token
    /\ published' = IF published = token THEN published ELSE "none"
    /\ UNCHANGED <<observed, refresh>>

Fetch ==
    /\ refresh = "idle"
    /\ observed' = credential
    /\ refresh' = "fetched"
    /\ UNCHANGED <<credential, published>>

Publish ==
    /\ refresh = "fetched"
    /\ refresh' = "idle"
    /\ observed' = "none"
    /\ credential' = credential
    /\ IF StaleObservation
       THEN published' = observed
       ELSE published' = IF observed = credential THEN observed ELSE published

Next == SignOut \/ (\E token \in {"A", "B"}: SignIn(token)) \/ Fetch \/ Publish
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ credential \in {"none", "A", "B"}
    /\ published \in {"none", "A", "B"}
    /\ observed \in {"none", "A", "B"}
    /\ refresh \in {"idle", "fetched"}

PublishedAgrees == published = "none" \/ published = credential

\* Reachable on the current design, so PublishedAgrees is not vacuously true.
NeverSignedOut == credential # "none"
NeverInFlight == refresh # "fetched"
NeverOtherAccount == credential # "B"
=============================================================================
