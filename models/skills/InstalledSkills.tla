------------------------- MODULE InstalledSkills -------------------------
(* One name, two projects, and two installers competing per project.
   All switches FALSE models the current design; each switch removes one guard. *)
CONSTANTS
    PublishWithoutValidation,
    PublishOverExisting,
    CleanupBeforeHide,
    ReadOtherProject,
    LoadFallsBackToLearned,
    ListIncludesBody,
    ReviewTouchesInstall,
    PublishAfterProjectDeletion

Projects == {"a", "b"}
Installers == {"first", "second"}
Stages == {"idle", "copying", "invalid", "checked"}
Bundles == {"none", "partial", "complete", "rewritten"}
Empty == [stage |-> [i \in Installers |-> "idle"], active |-> "none",
          learned |-> FALSE, retired |-> FALSE, visibleRetired |-> FALSE,
          replaced |-> FALSE, deleted |-> FALSE]
NoResponse == [project |-> "none", owner |-> "none",
               action |-> "none", content |-> "none"]

VARIABLES catalog, response
vars == <<catalog, response>>

Init ==
    /\ catalog = [p \in Projects |-> Empty]
    /\ response = NoResponse

(* Copy and validation only change private stages. Another installer can
   complete its stage and publish while this one is still preparing. *)
Start(p, i) ==
    /\ ~catalog[p].deleted
    /\ catalog[p].stage[i] = "idle"
    /\ catalog' = [catalog EXCEPT ![p].stage[i] = "copying"]
    /\ UNCHANGED response

Validate(p, i, valid) ==
    /\ catalog[p].stage[i] = "copying"
    /\ catalog' = [catalog EXCEPT ![p].stage[i] = IF valid THEN "checked" ELSE "invalid"]
    /\ UNCHANGED response

Abandon(p, i) ==
    /\ catalog[p].stage[i] # "idle"
    /\ catalog' = [catalog EXCEPT ![p].stage[i] = "idle"]
    /\ UNCHANGED response

(* Check the external name and rename the complete directory under its lock. *)
Publish(p, i) ==
    /\ (~catalog[p].deleted \/ PublishAfterProjectDeletion)
    /\ catalog[p].stage[i] = "checked"
       \/ (PublishWithoutValidation /\ catalog[p].stage[i] \in {"copying", "invalid"})
    /\ catalog[p].active = "none" \/ PublishOverExisting
    /\ catalog' = [catalog EXCEPT ![p].stage[i] = "idle",
                   ![p].active = IF catalog[p].stage[i] = "checked" THEN "complete" ELSE "partial",
                   ![p].replaced = @ \/ catalog[p].active # "none"]
    /\ UNCHANGED response

(* Learned skill operations use a separate namespace. *)
Learn(p) ==
    /\ ~catalog[p].deleted
    /\ ~catalog[p].learned
    /\ catalog' = [catalog EXCEPT ![p].learned = TRUE]
    /\ response' = NoResponse

Review(p) ==
    /\ catalog[p].learned
    /\ catalog' = [catalog EXCEPT ![p].learned = FALSE,
                   ![p].active = IF ReviewTouchesInstall /\ @ = "complete" THEN "rewritten" ELSE @]
    /\ UNCHANGED response

(* Detach is the visible removal. A failed cleanup leaves a retired tree,
   outside the catalog. Clearing the prior response lets witnesses check a
   new observation following this removal. *)
Detach(p) ==
    /\ catalog[p].active # "none"
    /\ catalog' = [catalog EXCEPT ![p].retired = TRUE,
                   ![p].visibleRetired = CleanupBeforeHide,
                   ![p].active = IF CleanupBeforeHide THEN @ ELSE "none"]
    /\ response' = NoResponse

Cleanup(p) ==
    /\ catalog[p].retired
    /\ catalog' = [catalog EXCEPT ![p].retired = FALSE, ![p].visibleRetired = FALSE,
                   ![p].active = IF catalog[p].visibleRetired THEN "none" ELSE @]
    /\ UNCHANGED response

(* The deleted project invalidates retained store handles. Private upload
   stages can still finish reading or abandon, but cannot publish. *)
DeleteProject(p) ==
    /\ ~catalog[p].deleted
    /\ catalog' = [catalog EXCEPT ![p].deleted = TRUE, ![p].active = "none",
                   ![p].learned = FALSE, ![p].retired = FALSE, ![p].visibleRetired = FALSE]
    /\ response' = NoResponse

(* List and load observe the calling project's external skill namespace.
   A same-named learned skill cannot satisfy an external load. *)
List(p) ==
    LET found == catalog[p].active # "none"
    IN /\ response' = [project |-> p, owner |-> IF found THEN p ELSE "none",
                        action |-> "list",
                        content |-> IF ~found THEN "none"
                                    ELSE IF ListIncludesBody THEN "body" ELSE "metadata"]
       /\ UNCHANGED catalog

Load(p) ==
    LET other == CHOOSE q \in Projects : q # p
        learnedFallback == catalog[p].active = "none" /\ catalog[p].learned /\ LoadFallsBackToLearned
        owner == IF catalog[p].active # "none" \/ learnedFallback THEN p
                 ELSE IF ReadOtherProject /\ catalog[other].active # "none" THEN other
                 ELSE "none"
    IN /\ response' = [project |-> p, owner |-> owner, action |-> "load",
                        content |-> IF learnedFallback THEN "learned"
                                    ELSE IF owner = "none" THEN "none" ELSE "body"]
       /\ UNCHANGED catalog

Next ==
    \E p \in Projects :
        \/ \E i \in Installers :
            Start(p, i) \/ Validate(p, i, TRUE) \/ Validate(p, i, FALSE)
            \/ Abandon(p, i) \/ Publish(p, i)
        \/ Learn(p) \/ Review(p) \/ Detach(p) \/ Cleanup(p) \/ List(p) \/ Load(p) \/ DeleteProject(p)

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ catalog \in [Projects -> [stage: [Installers -> Stages], active: Bundles,
                                   learned: BOOLEAN, retired: BOOLEAN, visibleRetired: BOOLEAN,
                                   replaced: BOOLEAN, deleted: BOOLEAN]]
    /\ response \in [project: Projects \cup {"none"}, owner: Projects \cup {"none"},
                      action: {"none", "list", "load"}, content: {"none", "metadata", "body", "learned"}]

VisibleBundleValidated == \A p \in Projects : catalog[p].active # "partial"
ExistingBundlePreserved == \A p \in Projects : ~catalog[p].replaced
RemovedBundleHidden == \A p \in Projects : ~catalog[p].visibleRetired
ReadIsProjectScoped == response.owner # "none" => response.owner = response.project
LoadIsExternalOnly == response.action = "load" => response.content # "learned"
ListDisclosesMetadataOnly == response.action = "list" => response.content # "body"
ReviewPreservesInstalled == \A p \in Projects : catalog[p].active # "rewritten"
ProjectDeletionIsFinal == \A p \in Projects : catalog[p].deleted => catalog[p].active = "none"

(* Witnesses are checked as deliberately false invariants. *)
NeverInstalledList == ~(response.action = "list" /\ response.content = "metadata")
NeverInstalledLoad == ~(response.action = "load" /\ response.content = "body")
NeverInvalidStage == ~(\E p \in Projects, i \in Installers :
    catalog[p].stage[i] = "invalid" /\ catalog[p].active = "none")
NeverCompetingInstalls == ~(\E p \in Projects :
    catalog[p].active = "complete" /\ \E i \in Installers : catalog[p].stage[i] = "checked")
NeverSameNameAcrossProjects == ~(\A p \in Projects : catalog[p].active = "complete")
NeverSameNameLearnedAndInstalled == ~(\E p \in Projects :
    catalog[p].active = "complete" /\ catalog[p].learned)
NeverLearnedOnlyLoadMiss == ~(\E p \in Projects :
    catalog[p].learned /\ catalog[p].active = "none"
    /\ response.project = p /\ response.action = "load" /\ response.content = "none")
NeverDetachedLookup == ~(\E p \in Projects :
    catalog[p].retired /\ response.project = p /\ response.action = "load" /\ response.content = "none")
NeverReinstalledWithPendingCleanup == ~(\E p \in Projects :
    catalog[p].retired /\ catalog[p].active = "complete")
NeverDeletedWithStagedUpload == ~(\E p \in Projects, i \in Installers :
    catalog[p].deleted /\ catalog[p].stage[i] = "checked")
=========================================================================
