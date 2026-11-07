-------------------------- MODULE DefaultSourceChoice --------------------------
EXTENDS TLC

(* Which row Settings marks as the default, against where a new conversation
   actually starts. The engine uses the server when its catalog is ready and
   the stored source is not "provider"; an omitted source counts as the server.
   Otherwise it uses the configured provider, including while the catalog is
   missing. BadgeIgnoresSource is the old list, which marked models.default
   and ignored that choice. ServerChoiceHidden omits the server row, so a
   ready catalog cannot be selected there. *)

CONSTANTS
    BadgeIgnoresSource,
    ServerChoiceHidden

VARIABLES source, chosen, accountReady

vars == <<source, chosen, accountReady>>

Providers == {"own", "other"}

Init ==
    /\ source = "unset"
    /\ chosen = "own"
    /\ accountReady = FALSE

ToggleReady ==
    /\ accountReady' = ~accountReady
    /\ UNCHANGED <<source, chosen>>

Choose(id) ==
    /\ id \in Providers
    /\ source' = "provider"
    /\ chosen' = id
    /\ UNCHANGED accountReady

ServerListed == accountReady /\ ~ServerChoiceHidden

ChooseServer ==
    /\ ServerListed
    /\ source' = "account"
    /\ UNCHANGED <<chosen, accountReady>>

Next == ToggleReady \/ ChooseServer \/ (\E id \in Providers: Choose(id))
Spec == Init /\ [][Next]_vars

\* The list's rule. The old badge never consulted the source.
serverStarts ==
    /\ accountReady
    /\ source # "provider"
    /\ ~BadgeIgnoresSource

ServerBadge == ServerListed /\ serverStarts

ProviderBadge(id) == id = chosen /\ ~serverStarts

Marked ==
    IF ServerBadge THEN "server"
    ELSE IF ProviderBadge("own") THEN "own"
    ELSE IF ProviderBadge("other") THEN "other"
    ELSE "none"

\* Pool.DefaultProviderID, independent of how the list draws the badge.
EngineStart ==
    IF accountReady /\ source # "provider" THEN "server" ELSE chosen

TypeOK ==
    /\ source \in {"unset", "account", "provider"}
    /\ chosen \in Providers
    /\ accountReady \in BOOLEAN
    /\ Marked \in {"none", "server", "own", "other"}
    /\ EngineStart \in {"server", "own", "other"}

BadgeMatchesStart == Marked = EngineStart

ServerChoiceVisible == accountReady => ServerListed

NeverReady == ~accountReady
NeverProvider == source # "provider"
NeverServerStart == EngineStart # "server"
=============================================================================
