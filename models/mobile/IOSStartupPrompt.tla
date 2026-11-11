--------------------------- MODULE IOSStartupPrompt ---------------------------
EXTENDS Naturals

(* The iOS shell binds static CapacitorCookies and CapacitorHttp flags into
   the document-start script before the page navigates. The historical bridge
   asks for those flags with a synchronous prompt, and that prompt can stay
   unanswered. SyncConfigPrompt restores that startup query.
   InstallAfterNavigation lets the page run before the flags are bound.
   Reset only keeps the finite state machine free of terminal deadlocks.
   This is the shell startup decision, not WebKit IPC. *)

CONSTANTS
    SyncConfigPrompt,
    InstallAfterNavigation

VARIABLES
    configBound, phase

vars == <<configBound, phase>>

Phases == {"idle", "ready", "prompt", "hung"}

Init ==
    /\ configBound = FALSE
    /\ phase = "idle"

TypeOK ==
    /\ configBound \in BOOLEAN
    /\ phase \in Phases

BindConfig ==
    /\ phase = "idle"
    /\ ~configBound
    /\ ~InstallAfterNavigation
    /\ configBound' = TRUE
    /\ UNCHANGED phase

StartUnbound ==
    /\ InstallAfterNavigation
    /\ phase = "idle"
    /\ ~configBound
    /\ phase' = "prompt"
    /\ UNCHANGED configBound

RunScript ==
    /\ phase = "idle"
    /\ configBound
    /\ IF SyncConfigPrompt THEN phase' = "prompt" ELSE phase' = "ready"
    /\ UNCHANGED configBound

AnswerPrompt ==
    /\ phase = "prompt"
    /\ phase' = "ready"
    /\ UNCHANGED configBound

DropPrompt ==
    /\ phase = "prompt"
    /\ phase' = "hung"
    /\ UNCHANGED configBound

Reset ==
    /\ phase \in {"ready", "hung"}
    /\ phase' = "idle"
    /\ configBound' = FALSE

Next ==
    \/ BindConfig
    \/ StartUnbound
    \/ RunScript
    \/ AnswerPrompt
    \/ DropPrompt
    \/ Reset

Spec == Init /\ [][Next]_vars

StartupPromptReturns == phase /= "hung"

ConfigBeforeScript == phase = "idle" \/ configBound

NeverReady == phase /= "ready"

=============================================================================
