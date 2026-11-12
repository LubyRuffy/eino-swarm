-------------------------- MODULE DefaultSourceChoice --------------------------
EXTENDS TLC

(* Which row Settings marks as the default, against where a new conversation
   actually starts. The engine uses the server when its catalog is ready and
   the stored source is not "provider"; an omitted source counts as the server.
   Otherwise it uses the configured provider, including while the catalog is
   missing. BadgeIgnoresSource is the old list, which marked models.default
   and ignored that choice. ServerChoiceHidden omits the server row, so a
   ready catalog cannot be selected there.

   presentation is the local official-model switch. "full" keeps the choice
   above. "official" while the account is ready starts the next conversation
   on the server and withdraws the own-provider rows. OfficialKeepsOwnProviders
   is that start rule removed. OfficialListsOwnProviders leaves the own rows
   selectable while the screen claims to show only the official model. *)

CONSTANTS
    BadgeIgnoresSource,
    ServerChoiceHidden,
    OfficialKeepsOwnProviders,
    OfficialListsOwnProviders

VARIABLES source, chosen, accountReady, presentation

vars == <<source, chosen, accountReady, presentation>>

Providers == {"own", "other"}

Init ==
    /\ source = "unset"
    /\ chosen = "own"
    /\ accountReady = FALSE
    /\ presentation = "full"

ToggleReady ==
    /\ accountReady' = ~accountReady
    /\ UNCHANGED <<source, chosen, presentation>>

TogglePresentation ==
    /\ presentation' = IF presentation = "full" THEN "official" ELSE "full"
    /\ UNCHANGED <<source, chosen, accountReady>>

\* The official lock is the ready account in official presentation, unless the
\* start rule has been removed. Own rows stay listed when the account is not
\* ready, the person chose the full list, or the hide rule has been removed.
officialLock == accountReady /\ presentation = "official" /\ ~OfficialKeepsOwnProviders

ownListed == OfficialListsOwnProviders \/ ~accountReady \/ presentation # "official"

Choose(id) ==
    /\ ownListed
    /\ id \in Providers
    /\ source' = "provider"
    /\ chosen' = id
    /\ UNCHANGED <<accountReady, presentation>>

ServerListed == accountReady /\ ~ServerChoiceHidden

ChooseServer ==
    /\ ServerListed
    /\ source' = "account"
    /\ UNCHANGED <<chosen, accountReady, presentation>>

Next == ToggleReady \/ TogglePresentation \/ ChooseServer \/ (\E id \in Providers: Choose(id))
Spec == Init /\ [][Next]_vars

\* The list's rule. The old badge never consulted the source. Official
\* presentation marks the server even when a provider was stored earlier.
serverStarts ==
    /\ accountReady
    /\ \/ officialLock
       \/ (source # "provider" /\ ~BadgeIgnoresSource)

ServerBadge == ServerListed /\ serverStarts

ProviderBadge(id) == id = chosen /\ ~serverStarts /\ ownListed

Marked ==
    IF ServerBadge THEN "server"
    ELSE IF ProviderBadge("own") THEN "own"
    ELSE IF ProviderBadge("other") THEN "other"
    ELSE "none"

\* Pool.DefaultProviderID. Official presentation does not consult the stored
\* provider source. The full list does.
EngineStart ==
    IF accountReady /\ (officialLock \/ source # "provider") THEN "server" ELSE chosen

TypeOK ==
    /\ source \in {"unset", "account", "provider"}
    /\ chosen \in Providers
    /\ accountReady \in BOOLEAN
    /\ presentation \in {"official", "full"}
    /\ Marked \in {"none", "server", "own", "other"}
    /\ EngineStart \in {"server", "own", "other"}

BadgeMatchesStart == Marked = EngineStart

ServerChoiceVisible == accountReady => ServerListed

OfficialStartsOnServer ==
    accountReady /\ presentation = "official" => EngineStart = "server"

OfficialHidesOwnProviders ==
    accountReady /\ presentation = "official" => ~ownListed

NeverReady == ~accountReady
NeverProvider == source # "provider"
NeverServerStart == EngineStart # "server"
NeverOfficial == presentation # "official"
=============================================================================
