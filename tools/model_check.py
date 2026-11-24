#!/usr/bin/env python3
"""Model-check the engine's ordering protocols with TLC.

    python3 tools/model_check.py [--fetch] [--jar PATH] [--out DIR] [CASE ...]

Each model under models/ states one protocol: models/lifecycle/EngineLifecycle.tla
(C-027 handoff, Stop, parked workers, C-001 idle exit and shell presence),
models/lifecycle/ProjectMoveAdmission.tla (C-037 live project ownership),
models/stream/EventStream.tla (C-003 stored order, slow readers, catch-up,
rewind and restart), and models/scheduling/ScheduleAuthorization.tla (C-009 /
C-012 human cancellation, durable waiting authority and manager mutations).
models/generation/ModelGenerationRecovery.tla checks same-Registry capped generation
recovery, asynchronous AfterModel history ownership and one durable retry.
models/generation/AskPublicationBoundary.tla checks clean-EOF publication across
late owner cancellation and the actual Ask tool context identity.
models/scheduling/WakeConsentTimeline.tla checks multiple original human
decisions, asynchronous classification and exact-ID cancellation deduplication.
models/scheduling/WakePolicyBackfill.tla checks bounded calls, same-turn
classification progress and the atomic schedule-write recheck (C-009 / C-012).
models/stream/ManagerCommentary.tla checks the order and privacy of admitted
manager commentary before it enters the event stream.
models/account/DeviceLogin.tla checks validated public-link publication, manual
authorization, explicit browser launch intent, terminal link clearing and required human approval.
models/release/LinuxRelease.tla checks both Linux architecture targets, name/machine
agreement, exact runtime evidence for new uploads and immutable public bytes.
models/account/AccountCatalog.tla checks that a published account catalog
belongs to the current credential (C-040).
models/account/DefaultSourceChoice.tla checks that the Settings default badge
is the source a new conversation starts on, that a ready server catalog
is a selectable row, and that official presentation starts on the server
without listing own providers (C-040, C-046).
models/provider/RouteRecovery.tla checks that confirmed route refusals stop
after a consecutive cap instead of waiting forever or buying model retries (C-029).
models/desktop/RendererRecovery.tla checks that a flat visible body is not
healthy paint, an unknown capture does not become a blank frame, and the one
reload precedes a bounded successor only in the foreground (C-034).
models/skills/InstalledSkills.tla checks project-scoped skill publication,
lookup, removal and separation from learned memory.
models/knowledge/ProjectKnowledge.tla checks current write authority, revision
comparison, indivisible refusal and retained history identity (C-053).
models/sidechat/SideChat.tla checks that temporary replies stay within their
request owner and never write to or take control of the main task.
models/backup/DataBackup.tla checks that a restore waits until the data
directory is idle, a backup may proceed only by omitting work that stays
running, a failed full restore rolls back, and a project restore cannot take
another project's identity (C-044).
models/mobile/IOSStartupPrompt.tla checks that iOS binds static cookie and HTTP
flags before the page starts, and that the old synchronous startup prompt can
stay unanswered (C-045).
models/mobile/SpeechCapture.tla checks final capture bytes, compatible ASR
containers, converted byte limits and cancellation (C-048).
models/mobile/SpeechPlayback.tla checks paired ownership across catalog refresh,
ordered segment draining, complete playback and real authority revocation (C-048).
Both speech models check eventual settlement under explicitly fair progress.
models/lifecycle/WorkerCollectionView.tla checks that a live collection pause
stays actionable when the transcript flag is idle, and that a decline is
closing rather than the idle stopped sentence while the turn is still running.
models/lifecycle/WorkerCollectionAdmission.tla checks that an automatic resume
withdraws its waiter before releasing the runtime lock, a later decline is not
stored, and a durable Stop still blocks that resume.
models/swarm/ConcurrencyCap.tla checks that one task's running sub-agents stay
within the live cap when lowered, and invalid Settings saves are refused (C-009).
models/mobile/PhoneRoster.tla checks that a watchdog finish leaves that run
failed on the phone: a parent error does not retire a live peer, a retain
receipt does not revive the dead run, the running count stays one row per
worker, and the history cursor does not follow the sidecar (C-006, C-019).
Every case sets its model's switch constants and states
the expected outcome: the current design must pass every safety invariant;
each historical or seeded behaviour must break the named invariant (checked
alone with TypeOK, so the reported invariant does not depend on which
counterexample TLC's workers reach first); each reachability witness must be
violated so that a passing invariant cannot be vacuous. A case whose outcome
differs fails the run.

TLC is tla2tools.jar, found through --jar, $TLA2TOOLS_JAR or the user cache.
--fetch downloads the pinned release into that cache and checks its SHA-256;
nothing is downloaded without it. Each case runs in its own directory under
--out (a fresh temporary directory by default), which keeps its config, TLC
output and any counterexample trace.
"""

from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
import tempfile
import urllib.request
from model_check_provider_admission import provider_admission_cases
from dataclasses import dataclass, field
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

TLA_VERSION = "v1.7.4"
TLA_URL = f"https://github.com/tlaplus/tlaplus/releases/download/{TLA_VERSION}/tla2tools.jar"
TLA_SHA256 = "936a262061c914694dfd669a543be24573c45d5aa0ff20a8b96b23d01e050e88"
CACHE_JAR = Path.home() / ".cache" / "tlaplus" / TLA_VERSION / "tla2tools.jar"


@dataclass(frozen=True)
class Model:
    name: str
    spec: Path
    switches: tuple[str, ...]
    safety: tuple[str, ...]
    # Bounds that keep the state space finite.
    constants: tuple[tuple[str, int | str], ...]
    properties: tuple[str, ...] = ()


MODELS = {
    m.name: m
    for m in (
        Model("visible-history", ROOT / "models" / "stream" / "VisibleHistory.tla",
              ("UseVisibleCursor", "SkipOlderMessage", "KeepRewoundWork"),
              ("TypeOK", "NoDeferredReplay", "PreviousMessageReached", "WorkStillStored"), ()),
        Model("task-panel", ROOT / "models" / "panels" / "TaskPanel.tla",
              ("IgnoreOwner", "DeliverBeforeCommit", "ApplyDuplicate", "IgnoreBaseRevision", "IgnoreOperationDigest", "CleanupOtherOwner", "SkipRecovery", "SeparatePanelConnection"),
              ("TypeOK", "CommittedBeforeDelivery", "OwnerRequired", "Idempotent", "BaseRevisionRequired", "OperationBodyStable", "CleanupKeepsOwner", "NoOrphanAfterRestore", "SaveConnectionAvailable"), ()),
        Model("panel-instance", ROOT / "models" / "panels" / "PanelInstance.tla",
              ("ShowInitial", "IgnoreClose", "StopTurnOnClose"),
              ("TypeOK", "VisibilityRequiresEvidence", "ClosedStaysHidden", "TaskUnaffected"), ()),
        Model("panel-close-ordering", ROOT / "models" / "panels" / "PanelCloseOrdering.tla",
              ("CancelBeforeCommit", "AckBeforeCancel", "CancelSibling", "AcceptLateWrite",
               "CloseWithoutDeletion", "ForgetCloseOnRestart", "AcceptForeignClose"),
              ("TypeOK", "FailedCloseDoesNotCancel", "CloseRecordsDeletion", "CloseCancelsOnlyOwner",
               "AckAfterCancellation", "NoLatePublication", "CloseSurvivesRestart", "ForeignDoesNotClose"), ()),
        Model("panel-close-delivery", ROOT / "models" / "panels" / "PanelCloseDelivery.tla",
              ("IgnoreCloseAck", "ApplyOldPayload", "ApplyOldAck"),
              ("TypeOK", "AckHidesCurrentInstance", "OldPayloadCannotRevive", "OldAckCannotAffectCurrentVisit"), ()),
        Model("panel-ownership", ROOT / "models" / "panels" / "PanelOwnership.tla",
              ("StealRunningSource", "AbortAllRecovery"),
              ("TypeOK", "RunningOwnerKept", "HealthyThreadsRecover"), ()),
        Model("panel-mcp-admission", ROOT / "models" / "panels" / "PanelMCPAdmission.tla",
              ("BindLatestSettings", "RestoreOtherDestination", "SkipSourcePolicy", "IgnoreWorkerScope",
               "SkipSessionAdmission", "SkipRemoteRecheck"),
              ("TypeOK", "TurnBindingFrozen", "RestoreDestinationKept", "SourcePolicyRequired",
               "WorkerScopeRequired", "SessionAdmissionRequired", "RemoteRecheckRequired"), ()),
        *(Model(name, ROOT / "models" / "panels" / "EASMCollection.tla",
                ("KeepOldQueryData", "RetryCompleted", "MergePartialTotals", "ClaimCompleteOnSettled",
                 "ReturnBeforeJoin", "PublishUnlocked", "InventFailedPersistence", "TrustStoredRunning", "CollectWithoutQuery"),
                ("TypeOK", "QueryDataOwned", "DoneStepsNotRepeated", "SummaryDoneHasWholeReply",
                 "CompletionClaimRequiresAllSteps", "ReturnWaitsForJoin", "LockedPublicationStable",
                 "FailedPublishKeepsDurable", "NoQueryStartsNoCalls", "TerminalObservationSettles"), (("Variant", variant),))
          for name, variant in (("easm-collection", 1), ("easm-collection-interleaving", 2))),
        Model("panel-frame", ROOT / "models" / "panels" / "PanelFrameDelivery.tla",
              ("ForgetHiddenResync", "AckBeforeApply", "ExecuteIncompatible", "SilentTimeout"),
              ("TypeOK", "AcknowledgedWasApplied", "RecoveryRemainsSendable", "OnlyCompatibleExecutes", "FailureIsVisible"), ()),
        Model("panel-projection", ROOT / "models" / "panels" / "PanelProjection.tla",
              ("RejectStaleCursor", "ForgetDelete", "OverwriteNewer", "IgnoreVisit", "KeepCoveredObservation"),
              ("TypeOK", "EligibleSnapshotAdmitted", "DeletedNeverResurrects", "NewerRevisionRetained", "PreviousVisitIgnored", "CoveredAbsenceSettles"), ()),
        Model("panel-producer", ROOT / "models" / "panels" / "PanelProducer.tla",
              ("DropObservedResult", "FinishBeforeJoin", "NonCanonicalIdentity"),
              ("TypeOK", "RequestsAtMostOnce", "ObservedResultsRetained", "ReturnWaitsForJoin", "ReturnedObservationsApplied"), ()),
        Model("mobile-panel", ROOT / "models" / "panels" / "MobileTaskPanel.tla",
              ("MixTransferRevision", "AcceptLateVisit", "ApplyPartialBody", "CloseOtherTransfer", "UsePanelCursorForChat", "RefreshCancelsTransfer"),
              ("TypeOK", "FrozenTransferRevision", "CurrentVisitOwnsApply", "CompleteBodyBeforeApply", "ValidatedBodyBeforeApply", "OldCloseKeepsSuccessor", "PanelCursorKeepsChatSeq", "ManifestKeepsActiveTransfer"), ()),
        Model("inspector-registry", ROOT / "models" / "panels" / "InspectorRegistry.tla",
              ("BorrowSidebarProject", "IgnoreThreadScope", "ExposeForeignTaskRows", "ForgetHistory", "BlankFallback", "UnmountHiddenFrame",
               "ExposeForeignProjectStatus", "IgnoreSignalOwner", "IgnoreSupersededSignal", "AcceptDeletedSignal", "ReplayStealsFocus",
               "RequireNuwaMode", "WorkStealsFocus"),
              ("TypeOK", "ProjectScopeCorrect", "ThreadScopeCorrect", "EveryMountedRowIsOwned", "HistoryRemainsVisible", "FallbackSelectsAvailable",
               "HiddenFrameRetained", "ProjectStatusOwned", "AutoOpenOwnerMatches", "AutoOpenUsesLatestSignal",
               "AutoOpenRequiresPresentInstance", "ReplayDoesNotOpen", "OpenConversationHasWork", "WorkAvailabilityKeepsFocus"), ()),
        Model(
            "speech-capture", ROOT / "models" / "mobile" / "SpeechCapture.tla",
            ("AutoMP4Unchanged", "SnapshotBeforeTail", "SkipConvertedLimit", "IgnoreCancelledConversion"),
            ("TypeOK", "ASRCompatible", "FinalChunkRetained", "ConvertedBytesBounded", "CancelledNeverPublished"),
            (), ("CaptureEventuallySettles",),
        ),
        Model(
            "speech-playback", ROOT / "models" / "mobile" / "SpeechPlayback.tla",
            ("RefreshCancelsPaired", "AdvanceBeforeDrain", "DropLastSegment", "IgnoreAuthorityLoss"),
            ("TypeOK", "PairedRefreshKeepsOwner", "NextPartWaitsForDrain", "SuccessIncludesAllAudio", "AuthorityLossStopsAudio"),
            (("Segments", 3), ("Samples", 2), ("RefreshBudget", 2)), ("PlaybackEventuallySettles",),
        ),
        Model("model-mode", ROOT / "models" / "lifecycle" / "ModelModeAdmission.tla",
              ("SampleThenCommit", "DropBoundCatalog", "RewriteRecoveredModel", "RecoverBeforeDiscovery", "IgnoreRevocation"),
              ("TypeOK", "SwitchIsIdle", "BoundCatalogSurvives", "RecoveryKeepsBinding", "RecoveryWaitsForDiscovery", "RevocationClosesSource"), ()),
        Model("mcp-call-recovery", ROOT / "models" / "mcp" / "MCPCallRecovery.tla",
              ("RetryUncertain", "IgnoreBlock", "RenewAgain", "LeaveManagerRunning", "BlockDeadline",
               "BlockMissing", "DispatchMissing", "AllowToolError", "BlockRateLimit", "BlockWrongType", "DispatchWrongType"),
              ("TypeOK", "VerifiedBeforeRetry", "OneReplacement", "NoWorkAfterBlock",
               "ManagerCancellationRequested", "DeadlineKeepsAdmission", "MissingKeepsAdmission",
               "MissingNeverDispatches", "RemoteErrorBlocks", "RateLimitKeepsAdmission",
               "WrongTypeKeepsAdmission", "WrongTypeNeverDispatches"), ()),
        Model(
            "concurrency-cap", ROOT / "models" / "swarm" / "ConcurrencyCap.tla",
            ("ShrinkKeepsHolders", "NormalizeInvalidSave"),
            ("TypeOK", "HoldersWithinCap", "InvalidSaveRefused"),
            (("MaxWorkers", 3),),
        ),
        Model(
            "phone-roster", ROOT / "models" / "mobile" / "PhoneRoster.tla",
            ("IgnoreTimeout", "ErrorKillsRunning", "RetainRevives", "CountEachSpawn", "ReadyMovesCursor"),
            ("TypeOK", "DeadStaysDead", "ErrorDoesNotRetire", "CountAgrees", "CursorStable"),
            (),
        ),
        Model(
            "generation-recovery", ROOT / "models" / "generation" / "ModelGenerationRecovery.tla",
            ("NoJoin",),
            ("TypeOK", "RecoveredHistoryOwnsEpoch", "RecoveryUsesCappedInput",
             "OneDurableClaimPerTurn", "CancellationDoesNotStartRecovery",
             "RequestBudgetConserved", "CapCauseRetained"),
            (("MaxRequests", 3), ("InitialRequestsSpent", 2)),
        ),
        Model(
            "route", ROOT / "models" / "provider" / "RouteRecovery.tla",
            ("UnboundedRouteRecovery", "ExhaustionStartsModelRetry"),
            ("TypeOK", "RouteRefusalStops", "NoRetryAfterExhaustion"),
            (("Cap", 3), ("MaxProbes", 9)),
        ),
        Model(
            "renderer-recovery", ROOT / "models" / "desktop" / "RendererRecovery.tla",
            ("IgnorePresentedFrame", "UnknownCountsAsFlat", "BlankSkipsReload",
             "BackgroundSpendsRecovery"),
            ("TypeOK", "VisibleFlatIsNotHealthy", "UnknownMatchStaysHealthy",
             "ReplacementFollowsReload", "RecoveryStaysInForeground"),
            (("MaxProbes", 3), ("MaxReplacements", 2)),
        ),
        Model(
            "diagram-paint", ROOT / "models" / "desktop" / "DiagramPaint.tla",
            ("IgnoreEdge", "IgnorePixels", "IgnoreElements", "IgnoreText", "IgnoreBytes"),
            ("TypeOK", "MountedEdge", "MountedPixels", "MountedElements",
             "MountedText", "MountedBytes"),
            (("MaxEdge", 4), ("MaxPixels", 6), ("MaxElements", 2),
             ("MaxText", 2), ("MaxBytes", 2), ("Bound", 5)),
        ),
        Model(
            "installed-skills", ROOT / "models" / "skills" / "InstalledSkills.tla",
            ("PublishWithoutValidation", "PublishOverExisting", "CleanupBeforeHide",
             "ReadOtherProject", "LoadFallsBackToLearned", "ListIncludesBody", "ReviewTouchesInstall",
             "PublishAfterProjectDeletion"),
            ("TypeOK", "VisibleBundleValidated", "ExistingBundlePreserved", "RemovedBundleHidden",
             "ReadIsProjectScoped", "LoadIsExternalOnly", "ListDisclosesMetadataOnly", "ReviewPreservesInstalled",
             "ProjectDeletionIsFinal"),
            (),
        ),
        Model(
            "distillation-progress", ROOT / "models" / "knowledge" / "DistillationProgress.tla",
            ("FinishAfterFirstBatch", "AdvanceBeforeAcceptance", "ForgetCursorOnResume",
             "CountIncompleteRecord", "ForgetSegmentOnResume"),
            ("TypeOK", "AcceptBeforeAdvance", "KeepCheckpoint", "CompleteCoversScope"), (),
        ),
        Model(
            "wiki-build", ROOT / "models" / "knowledge" / "WikiBuild.tla",
            ("DocumentBeforeGraph", "CompleteBeforeDocuments", "LoseDocumentCheckpoint",
             "ReviewBeforeLocalScan", "ScanWithoutPublication", "LoseLocalCheckpoint", "ReviewWithoutDecision", "DocumentWithoutNode"),
            ("TypeOK", "GraphFirst", "DocumentsCovered", "KeepPublished",
             "LocalCheckpointHasEvidence", "ReviewFollowsLocalScan", "DocumentsHaveNodes",
             "ReviewCheckpointHasDecision"), (),
        ),
        Model(
            "project-distillation", ROOT / "models" / "knowledge" / "ProjectDistillation.tla",
            ("AllowConcurrent", "AllowForeignSource", "ReceiptWithoutWrite"),
            ("TypeOK", "OneManualRun", "ScopedWrites", "RealReceipts"),
            (),
        ),
        Model(
            "project-knowledge", ROOT / "models" / "knowledge" / "ProjectKnowledge.tla",
            ("UseStaleOwner", "IgnoreReadOnly", "IgnoreRevision", "PublishPartial",
             "AllowForeignSource", "ResolveByLocatorOnly", "IgnoreContentChange", "ReplaceSchema"),
            ("TypeOK", "CurrentOwnerRequired", "RevisionRequired", "RefusalPreservesLibrary",
             "SourceMustBeScoped", "OriginalIdentityRequired", "SchemaOnlyAppends"),
            (("MaxRevision", 3),),
        ),
        Model(
            "ios-startup", ROOT / "models" / "mobile" / "IOSStartupPrompt.tla",
            ("SyncConfigPrompt", "InstallAfterNavigation"),
            ("TypeOK", "StartupPromptReturns", "ConfigBeforeScript"),
            (),
        ),
        Model(
            "backup", ROOT / "models" / "backup" / "DataBackup.tla",
            ("AllowWhileBusy", "SkipRollback", "AllowForeignIdentity"),
            ("TypeOK", "RestoringImpliesIdle", "SnapshotNeverLost", "ForeignUnchanged"),
            (),
        ),
        Model(
            "side-chat", ROOT / "models" / "sidechat" / "SideChat.tla",
            ("UseMainSend", "RebindContext", "SkipReplyOwner", "EnableSideTools", "HoldMainDuringModel"),
            ("TypeOK", "SideLeavesMainUntouched", "ModelReadsBoundThread",
             "ReplyMatchesActiveOwner", "SideHasNoTools", "MainCanAdvanceWhileSideWaits"),
            (),
        ),
        Model(
            "peer-inventory", ROOT / "models" / "native" / "PeerInventory.tla",
            ("NumericPIDOwnsChildren", "IgnoreClaims", "IgnoreSameUIDChildren", "IgnoreActiveController"),
            ("TypeOK", "ClaimsRequired", "SameUIDChildrenRefuse", "ActiveControllerBusy", "ForeignReuseDoesNotBlock"),
            (),
        ),
        Model(
            "device-login", ROOT / "models" / "account" / "DeviceLogin.tla",
            ("RequireBrowser", "PublishUnsafe", "RetainTerminalURL", "BypassApproval", "LaunchWithoutOptIn"),
            ("TypeOK", "ManualLoginAvailable", "OnlyValidatedURL", "TerminalHidesURL", "ApprovalRequired", "BrowserLaunchRequiresOptIn"),
            (),
        ),
        Model(
            "linux-release", ROOT / "models" / "release" / "LinuxRelease.tla",
            ("ArmOnly", "IgnoreMachine", "IgnoreReceipt", "RebuildPublic"),
            ("TypeOK", "BothArchitecturesSupported", "MachineMatchesName", "ReceiptMatchesNewUpload", "PublicBytesPreserved"),
            (),
        ),
        Model(
            "account", ROOT / "models" / "account" / "AccountCatalog.tla",
            ("StaleObservation", "UnsealedLogout"),
            ("TypeOK", "PublishedAgrees"),
            (),
        ),
        Model(
            "default-source", ROOT / "models" / "account" / "DefaultSourceChoice.tla",
            ("BadgeIgnoresSource", "ServerChoiceHidden", "OfficialKeepsOwnProviders", "OfficialListsOwnProviders"),
            ("TypeOK", "BadgeMatchesStart", "ServerChoiceVisible", "OfficialStartsOnServer", "OfficialHidesOwnProviders"),
            (),
        ),
        Model(
            "collection-view", ROOT / "models" / "lifecycle" / "WorkerCollectionView.tla",
            ("GateOnTranscriptRunning", "DeclineShownAsIdle"),
            ("TypeOK", "RunningPauseStaysActionable", "StoppedViewIsIdle",
             "DeclineWhileRunningIsClosing"),
            (),
        ),
        Model(
            "followup-delivery", ROOT / "models" / "lifecycle" / "FollowupDelivery.tla",
            ("SplitInterrupt",), ("TypeOK", "InterruptMatchesDelivery"), (),
        ),
        Model(
            "collection-admission", ROOT / "models" / "lifecycle" / "WorkerCollectionAdmission.tla",
            ("ReleaseAdmissionLater", "DeclineSkipsLog", "StopIgnored"),
            ("TypeOK", "AdmissionClosedAfterResume", "NoDeclineAfterResume", "StopPrecedesResume"),
            (),
        ),
        Model(
            "lifecycle", ROOT / "models" / "lifecycle" / "EngineLifecycle.tla",
            ("DeadlineKillsChild", "RestoreBeforeListen", "StopRacesRestore", "PresenceAfterWindow", "HeadlessWaiterUnheld",
             "IdleCommitNonAtomic", "FinishUngated", "SummaryUsesWriteBudget", "ParkedNotCheckpointed", "IdleCountsOnlyManagers",
             "ParkedAdmissionNonAtomic", "ParkedOwnerCheckedLate", "RestoreOwnerCheckedLate"),
            ("TypeOK", "WorkHasLiveOwner", "CheckpointHasOwner", "StopIsFinal", "WindowHasEngine",
             "RegistryAlwaysAccounted", "RestoreOwnsProject", "ParkedSuccessorOwnsProject", "SummaryLeavesWriteBudget"),
            (("MaxTurns", 2),),
        ),
        Model(
            "project-move", ROOT / "models" / "lifecycle" / "ProjectMoveAdmission.tla",
            ("UnorderedClaim", "UnorderedGoal", "RefusalDropsParked"),
            ("TypeOK", "ClaimOwnsProject", "ResumeReceiptOwnsProject", "RefusalKeepsParked"),
            (),
        ),
        Model(
            "stream", ROOT / "models" / "stream" / "EventStream.tla",
            ("MarkerUnstored", "MarkerAfterUnlock", "BroadcastAfterUnlock", "DropWhenSlow", "PanelCursorAsChatID", "WorkRevisionAsChatID"),
            ("TypeOK", "ConvergedView", "SeqIdentity", "PanelCursorIsIndependent"),
            (("MaxSeq", 5), ("Cap", 1)),
        ),
        Model(
            "manager-commentary", ROOT / "models" / "stream" / "ManagerCommentary.tla",
            ("DeferOrdinary", "PublishPrivate", "KeepPrivateInHistory",
             "AcceptWithoutEvidence", "PublishAfterStop"),
            ("TypeOK", "OrdinaryBeforeTool", "PrivateProseNotPublic",
             "PrivateProseNotInHistory", "ClaimAdmissionHasEvidence", "StopKeepsPublishedPrefix"),
            (),
        ),
        Model(
            "work-pruning", ROOT / "models" / "nuwa" / "WorkPruning.tla",
            ("PruneDeletesRecord", "DropTransitivePrerequisite", "KeepEveryInactive",
             "RememberPruned", "ReconfirmOnRestore", "ReconfirmOnPatch",
             "IgnorePinnedVersion", "IgnoreInactiveSource"),
            ("TypeOK", "StoredIdentitySurvivesPruning", "ActiveRecordsAppear",
             "InactiveAncestorsStayVisible", "UnneededInactiveRecordsCollapse",
             "PinsRequireExplicitReconfirmation", "WarningsReflectStatusAndVersion"),
            (("MaxVersion", 8),),
        ),
        Model(
            "nuwa", ROOT / "models" / "nuwa" / "NuwaCollaboration.tla",
            ("RebindOnRestore", "CancelParentOnly", "WriteAfterStop",
             "CrossProjectEvidence", "PublishBeforeCommit", "RewindSkipsRevision",
             "DeleteMovedSkipsRevision", "PromoteWithoutClosure", "PrivateLeak",
             "PromoteIgnoresThreadRevision", "PromoteIgnoresProjectRevision", "PromoteIgnoresMove",
             "PromoteIgnoresDataGeneration", "PromotePartiallyCommits", "PromotePublishesStaged"),
            ("TypeOK", "BindingStable", "StopOwnsDescendants", "NoLateFact",
             "ProjectScoped", "VisibleIsCommitted", "SourceChangeHasRevision",
             "ProjectDependencyClosure", "PrivateWorkIsOwned", "MergeAdmissionUsesSnapshot",
             "MergeCommitsClosureAndRevisions", "MergeRollbackKeepsCommittedState",
             "MergePublicationIsCommitted"),
            (("MaxWrites", 2),),
        ),
        Model(
            "scheduling", ROOT / "models" / "scheduling" / "ScheduleAuthorization.tla",
            ("CancelDoesNotRevoke", "ClaimedCancelIgnored", "ManagerCancelledMasksHuman",
             "ContinueGrantsPermission", "LateAllowOverridesCancel", "CommitSkipsPolicyRecheck",
             "RestartForgetsPolicy", "CompactForgetsPolicy", "CancelReceiptNotAtomic",
             "CancelDestinationUnchecked", "RetargetIgnoresStatus", "RepeatedDeleteRevokes"),
            ("TypeOK", "EveryArmAuthorized", "CancellationIsDurable", "CancellationIsAtomic",
             "TerminalDestinationStable", "CancellationKeepsSelectedTarget",
             "ClaimedCancellationStopsReport", "DuplicateDeleteKeepsLaterAllowance"),
            (("MaxAttempts", 2),),
        ),
        Model(
            "consent", ROOT / "models" / "scheduling" / "WakeConsentTimeline.tla",
            ("ClassificationCompletionOverridesSequence", "CancelDeduplicatesByThread",
             "DuplicateDeleteAppendsDeny", "PendingInputDoesNotBlock", "HumanCancelDoesNotWithdraw"),
            ("TypeOK", "EveryArmAuthorized", "SettledPolicyMatchesLatestIntent", "ExactIDReceiptUniqueness",
             "LaterAllowanceSurvivesDuplicate"),
            (("MaxHumanEvents", 5),),
        ),
        Model(
            "wake-backfill", ROOT / "models" / "scheduling" / "WakePolicyBackfill.tla",
            ("PerTurnBudget", "SkipWritePolicyRecheck"),
            ("TypeOK", "EveryArmAuthorized", "PendingBackfillAdvances"),
            (("BatchSize", 16), ("InitialInputs", 20)),
            ("EventuallySettled",),
        ),
    )
}


# One publication protocol with finite input variants; existing model inputs stay unchanged.
_ASKPUB_SAFETY = (
    "TypeOK", "ObservedSourceFactRetained", "NoInvalidToolAdmission", "NoInvalidCallPublication",
    "NoNewToolAfterOwnerCancel", "ExecutedCleanCallPublishedWhenUIDone", "WaiterKeepsContextIdentity",
    "CheckpointPendingKeepsIdentity", "AcknowledgementKeepsContextIdentity",
    "CheckpointAcknowledgementKeepsIdentity", "ExternalUnknownNotInventedSuccess",
)
for _name, _kind, _outcome, _complete, _finish in (
    ("ask-publication", "ask", "clean_eof", True, False),
    ("ask-publication-external", "external", "clean_eof", True, False),
    ("ask-publication-finish", "ask", "clean_eof", True, True),
    ("ask-publication-error", "ask", "source_error", True, False),
    ("ask-publication-incomplete", "ask", "clean_eof", False, False),
):
    MODELS[_name] = Model(
        _name, ROOT / "models" / "generation" / "AskPublicationBoundary.tla",
        ("LateConsumerCancelMasksEOF", "WaiterUsesStoreIdentityOnly"), _ASKPUB_SAFETY,
        (("ToolKind", f'"{_kind}"'), ("SourceOutcome", f'"{_outcome}"'),
         ("CompleteCall", "TRUE" if _complete else "FALSE"),
         ("FinishPresent", "TRUE" if _finish else "FALSE"), ("CallID", '"opaque-invocation-id"')),
    )


# Only these protocols separate observed guard-removal obligations from the
# other safety properties checked on every current input variant. Unlisted
# models retain the original requirement to seed every non-TypeOK invariant.
SEEDED_GUARD_OBLIGATIONS = {
    ROOT / "models" / "panels" / "PanelProducer.tla": {
        "DropObservedResult": ("ObservedResultsRetained", "ReturnedObservationsApplied"),
        "FinishBeforeJoin": ("ReturnWaitsForJoin", "ReturnedObservationsApplied"),
        "NonCanonicalIdentity": ("RequestsAtMostOnce",),
    },
    ROOT / "models" / "generation" / "ModelGenerationRecovery.tla": {
        "NoJoin": ("RecoveredHistoryOwnsEpoch",),
    },
    ROOT / "models" / "generation" / "AskPublicationBoundary.tla": {
        "LateConsumerCancelMasksEOF": ("ExecutedCleanCallPublishedWhenUIDone",),
        "WaiterUsesStoreIdentityOnly": ("WaiterKeepsContextIdentity", "CheckpointPendingKeepsIdentity"),
    },
}


@dataclass(frozen=True)
class Case:
    name: str
    source: str
    switches: frozenset[str] = field(default_factory=frozenset)
    # None: every safety invariant holds. Otherwise the invariant TLC must report.
    violates: str | None = None
    # A witness checks only its own invariant, which the design must violate.
    witness: str | None = None
    model: str = "lifecycle"

    def invariants(self) -> tuple[str, ...]:
        if self.witness:
            return (self.witness,)
        if self.violates:
            return ("TypeOK", self.violates)
        return MODELS[self.model].safety

    def expected(self) -> str | None:
        return self.witness or self.violates


def _case(name: str, source: str, switches: tuple[str, ...] = (), violates: str | None = None,
          witness: str | None = None, model: str = "lifecycle") -> Case:
    return Case(name, source, frozenset(switches), violates, witness, model)


def _stream(name: str, source: str, switches: tuple[str, ...] = (), violates: str | None = None,
            witness: str | None = None) -> Case:
    return _case(name, source, switches, violates, witness, "stream")


def _scheduling(name: str, source: str, switches: tuple[str, ...] = (), violates: str | None = None,
                witness: str | None = None) -> Case:
    return _case(name, source, switches, violates, witness, "scheduling")


def _consent(name: str, source: str, switches: tuple[str, ...] = (), violates: str | None = None,
             witness: str | None = None) -> Case:
    return _case(name, source, switches, violates, witness, "consent")


def _askpub(name: str, source: str, switches: tuple[str, ...] = (), violates: str | None = None,
            witness: str | None = None, model: str = "ask-publication") -> Case:
    return _case(name, source, switches, violates, witness, model)


CASES: tuple[Case, ...] = (
    _case("wiki-build-current", "extract all source windows, then finish all documents", model="wiki-build"),
    *(_case("wiki-build-" + guard, "seeded: " + guard, (guard,), invariant, model="wiki-build")
      for guard, invariant in (("DocumentBeforeGraph", "GraphFirst"),
                               ("CompleteBeforeDocuments", "DocumentsCovered"),
                               ("LoseDocumentCheckpoint", "KeepPublished"),
                               ("ReviewBeforeLocalScan", "ReviewFollowsLocalScan"),
                               ("ScanWithoutPublication", "LocalCheckpointHasEvidence"),
                               ("LoseLocalCheckpoint", "ReviewFollowsLocalScan"),
                               ("ReviewWithoutDecision", "ReviewCheckpointHasDecision"),
                               ("DocumentWithoutNode", "DocumentsHaveNodes"))),
    _case("wiki-build-paused-scan", "interrupt after one durable local window",
          witness="NeverPausedDuringPartialLocalScan", model="wiki-build"),
    _case("wiki-build-paused-documents", "pause after publishing only part of the article queue",
          witness="NeverPausedPartwayThroughDocuments", model="wiki-build"),
    _case("distillation-progress-current", "accepted batches cover the whole frozen history",
          model="distillation-progress"),
    *(_case("distillation-progress-" + guard, "seeded: " + guard, (guard,), invariant,
            model="distillation-progress")
      for guard, invariant in (("FinishAfterFirstBatch", "CompleteCoversScope"),
                               ("AdvanceBeforeAcceptance", "AcceptBeforeAdvance"),
                               ("ForgetCursorOnResume", "KeepCheckpoint"),
                               ("CountIncompleteRecord", "AcceptBeforeAdvance"),
                               ("ForgetSegmentOnResume", "KeepCheckpoint"))),
    _case("distillation-progress-resumed", "interrupt inside a record, resume and complete",
          witness="NeverCompleteAfterResume", model="distillation-progress"),
    _case("distillation-current", "exclusive manual review and scoped publication receipts",
          model="project-distillation"),
    *(_case("distillation-fault-" + guard, "seeded: " + guard, (guard,), invariant,
            model="project-distillation")
      for guard, invariant in (("AllowConcurrent", "OneManualRun"),
                               ("AllowForeignSource", "ScopedWrites"),
                               ("ReceiptWithoutWrite", "RealReceipts"))),
    _case("distillation-reach-all-targets", "one completed run saved every target",
          witness="NeverThreeTargets", model="project-distillation"),
    _case("knowledge-current", "project-bound revision writes, append-only schema and scoped evidence",
          model="project-knowledge"),
    *(_case("knowledge-fault-" + name, source, (switch,), invariant, model="project-knowledge")
      for name, source, switch, invariant in (
          ("stale-owner", "seeded: commit after its conversation leaves the project",
           "UseStaleOwner", "CurrentOwnerRequired"),
          ("readonly", "seeded: dispatch a write requested in planning mode",
           "IgnoreReadOnly", "CurrentOwnerRequired"),
          ("revision", "seeded: overwrite bytes published by a competing writer",
           "IgnoreRevision", "RevisionRequired"),
          ("partial", "seeded: publish partial bytes after an I/O failure",
           "PublishPartial", "RefusalPreservesLibrary"),
          ("foreign-source", "seeded: attribute history from another project",
           "AllowForeignSource", "SourceMustBeScoped"),
          ("reused-source", "seeded: old citation resolves to a new row at the same locator",
           "ResolveByLocatorOnly", "OriginalIdentityRequired"),
          ("edited-source", "readable user input changes without a new locator or timestamp",
           "IgnoreContentChange", "OriginalIdentityRequired"),
          ("schema-replace", "seeded: an append removes an existing schema definition",
           "ReplaceSchema", "SchemaOnlyAppends"))),
    *(_case("knowledge-reach-" + name, "C-053 behavior reachability", witness=witness,
            model="project-knowledge")
      for name, witness in (
          ("write", "NeverAttributedWrite"),
          ("read", "NeverOriginalRead"),
          ("stale-refusal", "NeverStaleRefusal"),
          ("readonly-refusal", "NeverReadOnlyRefusal"),
          ("reused-unavailable", "NeverReusedLocatorUnavailable"),
          ("edited-unavailable", "NeverEditedSourceUnavailable"),
          ("append", "NeverSchemaAppend"))),
    _case("task-panel-cleanup-owner", "seeded: old producer cleanup changes successor metadata", ("CleanupOtherOwner",), "CleanupKeepsOwner", model="task-panel"),
    _case("task-panel-orphan", "seeded: startup leaves interrupted producer running", ("SkipRecovery",), "NoOrphanAfterRestore", model="task-panel"),
    _case("task-panel-connection-budget", "observed two-window draft PUT starvation when separate panel SSE occupies all six H1 slots", ("SeparatePanelConnection",), "SaveConnectionAvailable", model="task-panel"),
    _case("visible-history-current", "scanned tail and older cursors, detail expansion and rewind", model="visible-history"),
    _case("visible-history-visible-cursor", "seeded: SSE starts from the last rendered row",
          ("UseVisibleCursor",), "NoDeferredReplay", model="visible-history"),
    _case("visible-history-skipped-older", "seeded: the older cursor skips the previous visible message",
          ("SkipOlderMessage",), "PreviousMessageReached", model="visible-history"),
    _case("visible-history-stale-work", "seeded: rewind keeps a deleted range",
          ("KeepRewoundWork",), "WorkStillStored", model="visible-history"),
    _case("visible-history-reach-live", "witness", witness="NeverOpenAndCatchUp", model="visible-history"),
    _case("visible-history-reach-rewind", "witness", witness="NeverPageExpandRewind", model="visible-history"),
    _stream("stream-panel-cursor", "seeded: panel cursor emitted as chat SSE id", ("PanelCursorAsChatID",), "PanelCursorIsIndependent"),
    _stream("stream-work-revision", "seeded: work revision emitted as chat SSE id", ("WorkRevisionAsChatID",), "PanelCursorIsIndependent"),
    _stream("stream-panel-reach", "named panel frame reaches shared SSE without a chat cursor", witness="NeverPanelFrame"),
    _case("task-panel-current", "atomic owned publication and idempotent delivery", model="task-panel"),
    _case("task-panel-late-owner", "seeded: deleted or stopped producer publishes", ("IgnoreOwner",), "OwnerRequired", model="task-panel"),
    _case("task-panel-before-commit", "seeded: publish before durable commit", ("DeliverBeforeCommit",), "CommittedBeforeDelivery", model="task-panel"),
    _case("task-panel-duplicate", "seeded: duplicate operation consumes another revision", ("ApplyDuplicate",), "Idempotent", model="task-panel"),
    _case("task-panel-stale-base", "seeded: first writer commits its captured base after another writer", ("IgnoreBaseRevision",), "BaseRevisionRequired", model="task-panel"),
    _case("task-panel-changed-body", "seeded: an operation identity is reused with a changed body", ("IgnoreOperationDigest",), "OperationBodyStable", model="task-panel"),
    *(_case("task-panel-reach-" + name, "publication and lifecycle reachability", witness=witness, model="task-panel")
      for name, witness in (("publication", "NeverPublished"), ("competing-writer", "NeverCompetingWriter"),
                            ("restore", "NeverRestored"), ("two-windows", "NeverTwoWindows"))),
    _case("panel-instance-current", "committed observations and source lifecycle control visibility", model="panel-instance"),
    _case("panel-instance-opening", "observed: the initial snapshot appears as data", ("ShowInitial",), "VisibilityRequiresEvidence", model="panel-instance"),
    _case("panel-instance-closed", "seeded: a closed instance remains eligible after publication", ("IgnoreClose",), "ClosedStaysHidden", model="panel-instance"),
    _case("panel-instance-task", "seeded: closing a source stops the task", ("StopTurnOnClose",), "TaskUnaffected", model="panel-instance"),
    *(_case("panel-instance-reach-" + name, "instance lifecycle reachability", witness=witness, model="panel-instance")
      for name, witness in (("running", "NeverRunningEmpty"), ("zero", "NeverRetainedZero"),
                            ("close", "NeverClosedRunning"), ("empty-finish", "NeverSettledEmpty"))),
    _case("panel-close-ordering-current", "durable close, exact cancellation, independent source exit", model="panel-close-ordering"),
    *(_case("panel-close-ordering-" + name, "guard mutation at the host close boundary", (switch,), invariant,
            model="panel-close-ordering")
      for name, switch, invariant in (
          ("rollback", "CancelBeforeCommit", "FailedCloseDoesNotCancel"),
          ("ack", "AckBeforeCancel", "AckAfterCancellation"),
          ("sibling", "CancelSibling", "CloseCancelsOnlyOwner"),
          ("publication", "AcceptLateWrite", "NoLatePublication"),
          ("journal", "CloseWithoutDeletion", "CloseRecordsDeletion"),
          ("restart", "ForgetCloseOnRestart", "CloseSurvivesRestart"),
          ("scope", "AcceptForeignClose", "ForeignDoesNotClose"),
      )),
    *(_case("panel-close-ordering-reach-" + name, "host ordering reachability", witness=witness,
            model="panel-close-ordering")
      for name, witness in (
          ("retry", "NeverRetryAfterWriteFailure"), ("unwinding", "NeverAckBeforeJoin"),
          ("crash", "NeverCrashBeforeCancel"), ("publish-first", "NeverPublishBeforeClose"),
          ("close-first", "NeverRejectCapturedPublish"),
      )),
    _case("panel-close-delivery-current", "close acknowledgement fences delayed projection replies", model="panel-close-delivery"),
    *(_case("panel-close-delivery-" + name, "guard mutation at the client close boundary", (switch,), invariant,
            model="panel-close-delivery")
      for name, switch, invariant in (
          ("ack", "IgnoreCloseAck", "AckHidesCurrentInstance"),
          ("stale-payload", "ApplyOldPayload", "OldPayloadCannotRevive"),
          ("stale-ack", "ApplyOldAck", "OldAckCannotAffectCurrentVisit"),
      )),
    *(_case("panel-close-delivery-reach-" + name, "client delivery reachability", witness=witness,
            model="panel-close-delivery")
      for name, witness in (("ack-crossing", "NeverCrossedAck"), ("old-visit", "NeverReturnedOldAck"),
                            ("reconnect", "NeverReconnectedClosed"))),
    _case("panel-ownership-current", "owned running source and per-conversation recovery isolation", model="panel-ownership"),
    _case("panel-ownership-steal", "observed: another publisher erased the live source owner and running marker", ("StealRunningSource",), "RunningOwnerKept", model="panel-ownership"),
    _case("panel-ownership-recovery", "observed: one malformed optional panel aborted recovery of unrelated conversations", ("AbortAllRecovery",), "HealthyThreadsRecover", model="panel-ownership"),
    *(_case("panel-ownership-reach-" + name, "owned source and independent recovery reachability", witness=witness, model="panel-ownership")
      for name, witness in (("running", "NeverRunningSource"), ("refusal", "NeverRejectedOtherPublisher"),
                            ("recovery", "NeverHealthyRecovery"))),
    _case("panel-mcp-current", "frozen turn bindings, actual worker execution scope and sampled call admission", model="panel-mcp-admission"),
    *(_case("panel-mcp-" + name, source, (switch,), invariant, model="panel-mcp-admission")
      for name, source, switch, invariant in (
          ("latest-settings", "guard: resolveMCPSurface must use the captured MCPAdmission", "BindLatestSettings", "TurnBindingFrozen"),
          ("restore-retarget", "guard: bindPanelMCP compares the recorded destination digest", "RestoreOtherDestination", "RestoreDestinationKept"),
          ("source-policy", "historical before 387f62c: panel source bypassed the turn's MCP admission", "SkipSourcePolicy", "SourcePolicyRequired"),
          ("worker-scope", "historical before b71b592: panel source borrowed tools absent from the calling worker", "IgnoreWorkerScope", "WorkerScopeRequired"),
          ("session-admission", "guard: Manager.acquire checks enabled destination fingerprints", "SkipSessionAdmission", "SessionAdmissionRequired"),
          ("remote-recheck", "guard: callOnce checks live tool switches again after session/catalog acquisition", "SkipRemoteRecheck", "RemoteRecheckRequired"))),
    *(_case("panel-mcp-reach-" + name, "sampled admission and late-revocation reachability", witness=witness, model="panel-mcp-admission")
      for name, witness in (
          ("direct", "NeverDirectWorkerCall"), ("proxy", "NeverProxyWorkerCall"),
          ("list-refusal", "NeverListOnlyRefusal"), ("late-role", "NeverLateRoleIgnored"),
          ("restricted", "NeverRestrictedSource"), ("restore-refusal", "NeverRestoreDestinationRefused"),
          ("remote-refusal", "NeverRemoteRecheckRefused"), ("session-refusal", "NeverSessionAdmissionRefused"),
          ("late-disable", "NeverLateRemoteDisableCompletes"), ("late-policy", "NeverAdmittedPolicyChangeCompletes"),
          ("retired-session", "NeverRetiredSessionCompletes"), ("restored", "NeverRestoredCall"))),
    _case("easm-collection-current", "serial same-query retries, partial totals and query replacement", model="easm-collection"),
    _case("easm-collection-interleaving-current", "concurrent apply/publication, cancellation and independent failed writes", model="easm-collection-interleaving"),
    *(_case("easm-collection-" + name, source, (switch,), invariant, model=model)
      for name, source, switch, invariant, model in (
          ("old-query-data", "guard: planMetrics clears data when the query changes", "KeepOldQueryData", "QueryDataOwned", "easm-collection"),
          ("repeat-done", "guard: planMetrics only retries unfinished same-query steps", "RetryCompleted", "DoneStepsNotRepeated", "easm-collection"),
          ("partial-totals", "seeded: cumulative totals from separate responses imply summary completion", "MergePartialTotals", "SummaryDoneHasWholeReply", "easm-collection"),
          ("false-completion", "historical before 4937728: a settled source always claimed complete aggregations", "ClaimCompleteOnSettled", "CompletionClaimRequiresAllSteps", "easm-collection"),
          ("early-return", "guard: collect waits for its started goroutines", "ReturnBeforeJoin", "ReturnWaitsForJoin", "easm-collection-interleaving"),
          ("unlocked-publish", "guard: flush holds mu through its synchronous publisher", "PublishUnlocked", "LockedPublicationStable", "easm-collection-interleaving"),
          ("failed-persistence", "guard: a failed publication leaves durable state unchanged", "InventFailedPersistence", "FailedPublishKeepsDurable", "easm-collection-interleaving"),
          ("stored-spinner", "historical before 4937728: stale running snapshots kept loading after interruption", "TrustStoredRunning", "TerminalObservationSettles", "easm-collection-interleaving"),
          ("no-query", "guard: batches without a query merge findings without collecting", "CollectWithoutQuery", "NoQueryStartsNoCalls", "easm-collection"))),
    *(_case("easm-collection-reach-" + name, "collection progress, partial snapshots and failed-write reachability", witness=witness, model=model)
      for name, witness, model in (
          ("retry-keeps-done", "NeverRetryKeepsDone", "easm-collection"),
          ("partial-retry", "NeverPartialSummaryRetried", "easm-collection"),
          ("changed-query", "NeverChangedQueryCleared", "easm-collection"),
          ("no-query", "NeverUnqueriedBatch", "easm-collection"),
          ("missing-step", "NeverSettledWithMissing", "easm-collection"),
          ("complete", "NeverAllSucceeded", "easm-collection-interleaving"),
          ("partial-publication", "NeverAppliedBeforeOutcome", "easm-collection-interleaving"),
          ("failed-write", "NeverFailedPublication", "easm-collection-interleaving"),
          ("failed-finish", "NeverFailedFinishReturns", "easm-collection-interleaving"),
          ("interrupt-writes-fail", "NeverBothInterruptWritesFail", "easm-collection-interleaving"),
          ("interrupted-reload", "NeverInterruptedReload", "easm-collection-interleaving"))),
    _case("panel-frame-current", "separate sent/applied/acknowledged revisions and retain hidden resynchronization", model="panel-frame"),
    *(_case("panel-frame-" + name, source, (switch,), invariant, model="panel-frame")
      for name, source, switch, invariant in (
          ("hidden-resync", "observed: a hidden rejected callback lost its only snapshot retry", "ForgetHiddenResync", "RecoveryRemainsSendable"),
          ("early-ack", "seeded: acknowledge a revision before the plugin applies it", "AckBeforeApply", "AcknowledgedWasApplied"),
          ("incompatible", "seeded: execute an incompatible pinned definition", "ExecuteIncompatible", "OnlyCompatibleExecutes"),
          ("silent-timeout", "seeded: a callback timeout has no visible failure", "SilentTimeout", "FailureIsVisible"))),
    *(_case("panel-frame-reach-" + name, "iframe delivery reachability", witness=witness, model="panel-frame")
      for name, witness in (("hidden-failure", "NeverHiddenFailure"), ("hidden-recovery", "NeverHiddenRecovery"),
                            ("inflight", "NeverAdvanceInflight"), ("timeout", "NeverTimeout"))),
    _case("panel-projection-current", "per-panel revision/tombstone merge of atomic snapshots across live updates", model="panel-projection"),
    *(_case("panel-projection-" + name, source, (switch,), invariant, model="panel-projection")
      for name, source, switch, invariant in (
          ("stale-cursor", "observed: Q advancing caused P's only definition snapshot to be discarded", "RejectStaleCursor", "EligibleSnapshotAdmitted"),
          ("delete", "seeded: an older GET resurrects a newer deletion", "ForgetDelete", "DeletedNeverResurrects"),
          ("overwrite", "seeded: an older GET overwrites a newer applied revision", "OverwriteNewer", "NewerRevisionRetained"),
          ("visit", "seeded: a previous conversation visit changes the selected projection", "IgnoreVisit", "PreviousVisitIgnored"),
          ("omitted", "observed: authoritative reconnect absence left a missing observation causing endless GETs", "KeepCoveredObservation", "CoveredAbsenceSettles"))),
    *(_case("panel-projection-reach-" + name, "snapshot/live stream crossing reachability", witness=witness, model="panel-projection")
      for name, witness in (("known-update", "NeverCrossedKnownUpdate"), ("new-panel", "NeverNewPanelMerged"),
                            ("delete", "NeverDeleteDuringGet"), ("visit", "NeverVisitDuringGet"),
                            ("omitted", "NeverReconnectAbsence"))),
    _case("panel-producer-current", "owned collector/workers join after reducing terminal HTTP observations", model="panel-producer"),
    _case("panel-producer-stop", "observed: Stop discarded a real HTTP response before reducer delivery", ("DropObservedResult",), "ObservedResultsRetained", model="panel-producer"),
    _case("panel-producer-stop-return", "observed cancellation branch cannot lose a response from the final reducer", ("DropObservedResult",), "ReturnedObservationsApplied", model="panel-producer"),
    _case("panel-producer-early-return", "seeded: source returns before joining owned workers", ("FinishBeforeJoin",), "ReturnWaitsForJoin", model="panel-producer"),
    _case("panel-producer-unapplied-return", "seeded: early return omits queued terminal observations", ("FinishBeforeJoin",), "ReturnedObservationsApplied", model="panel-producer"),
    _case("panel-producer-origin-identity", "observed: omitted versus explicit default origin ports generated two checks for one asset", ("NonCanonicalIdentity",), "RequestsAtMostOnce", model="panel-producer"),
    _case("panel-producer-reach-complete", "real observation and duplicate row reach the joined reducer", witness="NeverCompletedWitness", model="panel-producer"),
    _case("panel-producer-reach-stop", "observed response survives cancellation before receipt delivery", witness="NeverCancelledObservationWitness", model="panel-producer"),
    _case("mobile-panel-current", "immutable bounded transfers, current visit ownership and independent chat replay", model="mobile-panel"),
    *(_case("mobile-panel-" + name, source, (switch,), invariant, model="mobile-panel")
      for name, source, switch, invariant in (
          ("mixed-revision", "seeded: a later manifest revision replaces bytes during a transfer", "MixTransferRevision", "FrozenTransferRevision"),
          ("late-visit", "refinement of observed late-visit class: A to B to A accepts the first A's old reply", "AcceptLateVisit", "CurrentVisitOwnsApply"),
          ("partial", "seeded: render after one of two chunks", "ApplyPartialBody", "CompleteBodyBeforeApply"),
          ("unvalidated", "seeded: a partial body bypasses whole-body validation", "ApplyPartialBody", "ValidatedBodyBeforeApply"),
          ("old-close", "seeded: a stale transfer cancellation deletes its successor", "CloseOtherTransfer", "OldCloseKeepsSuccessor"),
          ("chat-seq", "seeded: panel delivery overwrites the conversation replay cursor", "UsePanelCursorForChat", "PanelCursorKeepsChatSeq"),
          ("refresh", "seeded: newer manifest cancels an unfinished read", "RefreshCancelsTransfer", "ManifestKeepsActiveTransfer"))),
    *(_case("mobile-panel-reach-" + name, "mobile snapshot, patch and visit recovery reachability", witness=witness, model="mobile-panel")
      for name, witness in (("snapshot", "NeverSnapshotApplied"), ("patch", "NeverPatchApplied"),
                            ("visit", "NeverVisitRecovered"), ("refresh", "NeverRefreshCompleted"))),
    _case("inspector-registry-current", "owned registered tabs, mounted iframe, and fresh automatic intent",
          model="inspector-registry"),
    *(_case("inspector-registry-" + name, source, (switch,), invariant, model="inspector-registry")
      for name, source, switch, invariant in (
          ("sidebar", "seeded: a standalone conversation borrows the sidebar project", "BorrowSidebarProject", "ProjectScopeCorrect"),
          ("thread", "seeded: a task tab binds to another conversation's projection", "IgnoreThreadScope", "ThreadScopeCorrect"),
          ("foreign-row", "seeded: an eligible tab renders another conversation's row along with its own", "ExposeForeignTaskRows", "EveryMountedRowIsOwned"),
          ("history", "seeded: disabling Nuwa hides the recorded project", "ForgetHistory", "HistoryRemainsVisible"),
          ("fallback", "observed: a selected task tab disappeared without selecting Agents", "BlankFallback", "FallbackSelectsAvailable"),
          ("frame", "seeded: switching to Collaboration destroys the mounted task frame", "UnmountHiddenFrame", "HiddenFrameRetained"),
          ("project-status", "observed: a new project briefly committed the previous project's error and connection status", "ExposeForeignProjectStatus", "ProjectStatusOwned"),
          ("signal-owner", "seeded: a captured signal focuses its tab after conversation selection changed", "IgnoreSignalOwner", "AutoOpenOwnerMatches"),
          ("signal-replaced", "seeded: an older captured signal focuses a tab after another instance was discovered", "IgnoreSupersededSignal", "AutoOpenUsesLatestSignal"),
          ("signal-deleted", "seeded: the only signaled instance was removed before the callback", "AcceptDeletedSignal", "AutoOpenRequiresPresentInstance"),
          ("signal-replayed", "seeded: restoring a known instance focuses the task tab again", "ReplayStealsFocus", "ReplayDoesNotOpen"),
          ("work-mode", "the old project-mode condition hides work in a standalone conversation", "RequireNuwaMode", "OpenConversationHasWork"),
          ("work-focus", "seeded: work registration steals inspector focus", "WorkStealsFocus", "WorkAvailabilityKeepsFocus"))),
    *(_case("inspector-registry-reach-" + name, "registered render and focus reachability",
            witness="Never" + witness, model="inspector-registry")
      for name, witness in (("both", "BothAvailable"), ("history", "HistorySelected"), ("fallback", "SafeFallback"),
                            ("retained", "HiddenIframe"), ("filtered", "FilteredForeignRow"),
                            ("standalone-work", "StandaloneWork"),
                            ("open", "AutoOpened"), ("stale-owner", "RefusedStaleOwner"),
                            ("superseded", "RefusedSuperseded"), ("deleted", "RefusedDeleted"), ("replay", "Replayed"))),
    _case("speech-capture-current", "auto MP4 normalizes after the final chunk; cancellation and limits remain active",
          model="speech-capture"),
    *(_case(name, source, (switch,), invariant, model="speech-capture")
      for name, source, switch, invariant in (
        ("speech-capture-known-mp4", "observed 2026-11-17 iPhone MP4 rejected by ASR; real capture regression fails without normalization",
         "AutoMP4Unchanged", "ASRCompatible"),
        ("speech-capture-missing-tail", "seeded: snapshot before the final dataavailable event",
         "SnapshotBeforeTail", "FinalChunkRetained"),
        ("speech-capture-oversize-wav", "seeded: converted WAV bypasses the input byte limit",
         "SkipConvertedLimit", "ConvertedBytesBounded"),
        ("speech-capture-late-cancelled", "seeded: publish a conversion after its owner was cancelled",
         "IgnoreCancelledConversion", "CancelledNeverPublished"),
    )),
    *(_case(name, "witness", witness=witness, model="speech-capture") for name, witness in (
        ("speech-capture-reach-wav", "NeverWAVReady"),
        ("speech-capture-reach-native", "NeverNativeReady"),
        ("speech-capture-reach-size-refusal", "NeverConvertedLimitRefusal"),
        ("speech-capture-reach-cancel-conversion", "NeverCancelledConversion"),
        ("speech-capture-reach-decode-failure", "NeverDecodeFailure"),
    )),
    _case("speech-playback-current", "all segments and samples drain across unrelated paired-phone catalog refreshes",
          model="speech-playback"),
    *(_case(name, source, (switch,), invariant, model="speech-playback")
      for name, source, switch, invariant in (
        ("speech-playback-known-refresh", "observed 2026-11-17 provider revision cancels paired playback; real HTTP/player regression reproduces it",
         "RefreshCancelsPaired", "PairedRefreshKeepsOwner"),
        ("speech-playback-skip-drain", "seeded: advance the segment loop before queued sources end",
         "AdvanceBeforeDrain", "NextPartWaitsForDrain"),
        ("speech-playback-drop-tail", "seeded: normal completion before the last text segment",
         "DropLastSegment", "SuccessIncludesAllAudio"),
        ("speech-playback-ignore-revocation", "seeded: preserve queued audio after actual authority loss",
         "IgnoreAuthorityLoss", "AuthorityLossStopsAudio"),
    )),
    *(_case(name, "witness", witness=witness, model="speech-playback") for name, witness in (
        ("speech-playback-reach-complete", "NeverCompleteAfterRefresh"),
        ("speech-playback-reach-queued-refresh", "NeverRefreshWithQueuedAudio"),
        ("speech-playback-reach-between-refresh", "NeverRefreshBetweenParts"),
        ("speech-playback-reach-pending-refresh", "NeverRefreshWhileWaiting"),
        ("speech-playback-reach-local-refresh-cancel", "NeverStandaloneCatalogCancel"),
        ("speech-playback-reach-cancel-queued", "NeverCancelQueuedAudio"),
    )),
    _case("model-mode-current", "mode changes hold exclusive source admission", model="model-mode"),
    _case("model-mode-sampled", "seeded idle sample races a new source check",
          ("SampleThenCommit",), "SwitchIsIdle", model="model-mode"),
    _case("model-mode-reach", "witness", witness="NeverSwitchWithBoundRequest", model="model-mode"),
    _case("model-mode-drop-bound-catalog", "historical de0d69b: gateway refresh drops the running LOCAL source",
          ("DropBoundCatalog",), "BoundCatalogSurvives", model="model-mode"),
    _case("model-mode-rewrite-recovery", "historical de0d69b: recovery resolves the current preset instead of the durable binding",
          ("RewriteRecoveredModel",), "RecoveryKeepsBinding", model="model-mode"),
    _case("model-mode-reach-recovery", "witness", witness="NeverRecoverAfterSwitch", model="model-mode"),
    _case("model-mode-recover-before-discovery", "historical de0d69b: startup restores an account binding before its catalog arrives",
          ("RecoverBeforeDiscovery",), "RecoveryWaitsForDiscovery", model="model-mode"),
    _case("model-mode-ignore-revocation", "seeded: retained catalogs ignore actual source revocation",
          ("IgnoreRevocation",), "RevocationClosesSource", model="model-mode"),
    _case("mcp-call-recovery-current", "one renewed session for a verified MCP rejection",
          model="mcp-call-recovery"),
    _case("mcp-call-recovery-uncertain", "seeded: replays a call whose outcome is unknown",
          ("RetryUncertain",), "VerifiedBeforeRetry", model="mcp-call-recovery"),
    _case("mcp-call-recovery-ignores-block", "historical f83aeeb: starts more work after failure",
          ("IgnoreBlock",), "NoWorkAfterBlock", model="mcp-call-recovery"),
    _case("mcp-call-recovery-unlimited-renewal", "seeded: repeatedly renews an expired session",
          ("RenewAgain",), "OneReplacement", model="mcp-call-recovery"),
    _case("mcp-call-recovery-leaves-manager-running", "seeded: the active manager request survives the worker failure",
          ("LeaveManagerRunning",), "ManagerCancellationRequested", model="mcp-call-recovery"),
    _case("mcp-call-recovery-blocks-deadline", "historical 9b799b2: a per-call deadline blocks the task",
          ("BlockDeadline",), "DeadlineKeepsAdmission", model="mcp-call-recovery"),
    _case("mcp-call-recovery-continues-after-deadline", "witness", witness="NeverNextWorkAfterDeadline",
          model="mcp-call-recovery"),
    _case("mcp-call-recovery-blocks-missing-argument", "historical 9b799b2: missing input dispatches and blocks work",
          ("DispatchMissing", "BlockMissing"), "MissingKeepsAdmission", model="mcp-call-recovery"),
    _case("mcp-call-recovery-dispatches-missing-argument", "seeded: sends the invalid request before reporting it",
          ("DispatchMissing",), "MissingNeverDispatches", model="mcp-call-recovery"),
    _case("mcp-call-recovery-allows-remote-tool-error", "seeded: gives a remote error the same admission as a local rejection",
          ("AllowToolError",), "RemoteErrorBlocks", model="mcp-call-recovery"),
    _case("mcp-call-recovery-continues-after-missing-argument", "witness", witness="NeverAdmitsWorkAfterMissing",
          model="mcp-call-recovery"),
    _case("mcp-call-recovery-blocks-rate-limit", "historical 27b1543 (#179): a rate limit blocks the task",
          ("BlockRateLimit",), "RateLimitKeepsAdmission", model="mcp-call-recovery"),
    _case("mcp-call-recovery-continues-after-rate-limit", "witness", witness="NeverAdmitsWorkAfterRateLimit",
          model="mcp-call-recovery"),
    _case("mcp-call-recovery-blocks-wrong-type", "historical 27b1543 (#180): wrong input type dispatches and blocks work",
          ("DispatchWrongType", "BlockWrongType"), "WrongTypeKeepsAdmission", model="mcp-call-recovery"),
    _case("mcp-call-recovery-dispatches-wrong-type", "seeded: sends the wrong input type before reporting it",
          ("DispatchWrongType",), "WrongTypeNeverDispatches", model="mcp-call-recovery"),
    _case("mcp-call-recovery-continues-after-wrong-type", "witness", witness="NeverAdmitsWorkAfterWrongType",
          model="mcp-call-recovery"),
    _case("mcp-call-recovery-reach-success", "witness", witness="NeverRecovered", model="mcp-call-recovery"),
    _case("mcp-call-recovery-reach-block", "witness", witness="NeverBlocked", model="mcp-call-recovery"),
    _case("concurrency-cap-current", "one task's running sub-agents stay within the live cap",
          model="concurrency-cap"),
    _case("concurrency-cap-shrink-keeps-holders",
          "historical, shipped through v0.1.77: lowering the cap left workers that already held a slot running",
          ("ShrinkKeepsHolders",), "HoldersWithinCap", model="concurrency-cap"),
    _case("concurrency-cap-normalize-invalid-save",
          "historical d62315b: Settings acknowledged an invalid cap and applied the default",
          ("NormalizeInvalidSave",), "InvalidSaveRefused", model="concurrency-cap"),
    _case("concurrency-cap-reach-refusal", "witness", witness="NeverRefused", model="concurrency-cap"),
    _case("concurrency-cap-reach-preempt", "witness", witness="NeverPreempted", model="concurrency-cap"),
    _case("phone-roster-current", "a watchdog finish stays failed and the running count stays one row per worker",
          model="phone-roster"),
    _case("phone-roster-ignore-timeout",
          "seeded: a watchdog finish is stored as an error the phone does not retire",
          ("IgnoreTimeout",), "DeadStaysDead", model="phone-roster"),
    _case("phone-roster-error-kills",
          "seeded: a parent error removes a worker the phone still shows as running",
          ("ErrorKillsRunning",), "ErrorDoesNotRetire", model="phone-roster"),
    _case("phone-roster-retain-revives",
          "seeded: workers_retained puts the timed-out run back in the running roster",
          ("RetainRevives",), "DeadStaysDead", model="phone-roster"),
    _case("phone-roster-count-each-spawn",
          "seeded: a worker that was finished and spawned again occupies two running rows",
          ("CountEachSpawn",), "CountAgrees", model="phone-roster"),
    _case("phone-roster-ready-moves-cursor",
          "seeded: the ready snapshot pulls Earlier back to a sidecar spawn",
          ("ReadyMovesCursor",), "CursorStable", model="phone-roster"),
    *(_case("phone-roster-reach-" + name, "phone roster reachability", witness=witness, model="phone-roster")
      for name, witness in (
          ("timeout", "NeverTimedOut"),
          ("peer", "NeverPeerSurvives"),
          ("respawn", "NeverRespawned"),
          ("cursor", "NeverCursorHeld"),
      )),
    _case("ios-startup-current", "iOS binds static cookie and HTTP flags before the page starts",
          model="ios-startup"),
    _case("ios-startup-sync-prompt",
          "historical, shipped through v0.1.73: the startup script waits on a synchronous config prompt",
          ("SyncConfigPrompt",), "StartupPromptReturns", model="ios-startup"),
    _case("ios-startup-install-after-navigation",
          "seeded: the page runs the bridge script before static config is bound",
          ("InstallAfterNavigation",), "ConfigBeforeScript", model="ios-startup"),
    _case("ios-startup-reach-ready", "witness", witness="NeverReady", model="ios-startup"),
    _case("backup-current", "restore waits until idle; backup may omit work that stays running", model="backup"),
    _case("backup-while-busy", "seeded: removing the idle guard lets a transfer start during other work",
          ("AllowWhileBusy",), "RestoringImpliesIdle", model="backup"),
    _case("backup-skip-rollback", "seeded: a failed full restore keeps the half-applied snapshot and advances the generation",
          ("SkipRollback",), "SnapshotNeverLost", model="backup"),
    _case("backup-foreign-identity", "seeded: a project restore writes an identity another project already owns",
          ("AllowForeignIdentity",), "ForeignUnchanged", model="backup"),
    _case("backup-reach-commit", "witness", witness="NeverCommitted", model="backup"),
    _case("backup-reach-skip", "witness", witness="NeverOmitted", model="backup"),
    _case("side-chat-current", "side questions stay independent of the running task", model="side-chat"),
    _case("side-chat-main-send", "seeded: sending a side question queues it on the main task",
          ("UseMainSend",), "SideLeavesMainUntouched", model="side-chat"),
    _case("side-chat-rebound-context", "seeded: navigation rebinds the source of a dispatched request",
          ("RebindContext",), "ModelReadsBoundThread", model="side-chat"),
    _case("side-chat-stale-reply", "seeded: a reply settles whichever panel is currently selected",
          ("SkipReplyOwner",), "ReplyMatchesActiveOwner", model="side-chat"),
    _case("side-chat-tools", "seeded: the side model inherits the task's tool surface",
          ("EnableSideTools",), "SideHasNoTools", model="side-chat"),
    _case("side-chat-holds-main", "seeded: a side model call holds the main task's progress lock",
          ("HoldMainDuringModel",), "MainCanAdvanceWhileSideWaits", model="side-chat"),
    *(_case(name, "witness", witness=witness, model="side-chat") for name, witness in (
        ("side-chat-reach-main-progress", "NeverAnswerAcrossMainProgress"),
        ("side-chat-reach-follow-up", "NeverTwoSideAnswers"),
        ("side-chat-reach-late-reply", "NeverLateReplyWithNewRequest"),
        ("side-chat-reach-error-recovery", "NeverRecoverAfterSideFailure"),
    )),
    _case("collection-view-current",
          "a live collection pause stays actionable, and a decline closes until the turn is idle",
          model="collection-view"),
    _case("collection-view-transcript-gate",
          "seeded: the card required the transcript running flag, so a live pause with an idle transcript was stopped",
          ("GateOnTranscriptRunning",), "RunningPauseStaysActionable", model="collection-view"),
    _case("collection-view-decline-idle",
          "seeded: a stored decline used the idle stopped sentence while the turn was still running",
          ("DeclineShownAsIdle",), "StoppedViewIsIdle", model="collection-view"),
    _case("collection-view-decline-closing",
          "seeded: a stored decline did not stay in the closing view while the turn was still running",
          ("DeclineShownAsIdle",), "DeclineWhileRunningIsClosing", model="collection-view"),
    *(_case(name, "witness", witness=witness, model="collection-view") for name, witness in (
        ("collection-view-reach-divergent", "NeverDivergentPause"),
        ("collection-view-reach-closing", "NeverClosing"),
        ("collection-view-reach-idle", "NeverIdleStop"),
    )),
    _case("followup-delivery-current", "One turn-scoped operation inserts and interrupts", model="followup-delivery"),
    _case("followup-delivery-split", "A delayed phone RPC targets the successor",
          ("SplitInterrupt",), "InterruptMatchesDelivery", model="followup-delivery"),
    *(_case(name, "witness", witness=witness, model="followup-delivery") for name, witness in (
        ("followup-delivery-reach-now", "NeverInsertNow"),
        ("followup-delivery-reach-next-call", "NeverInsertWithoutInterrupt"),
        ("followup-delivery-reach-other-row", "NeverKeepOtherRow"),
    )),
    _case("collection-admission-current",
          "resume withdraws its waiter before the lock drops, and a later decline is refused",
          model="collection-admission"),
    _case("collection-admission-release-later",
          "observed: the resume receipt drops the runtime lock before the waiter is withdrawn",
          ("ReleaseAdmissionLater",), "AdmissionClosedAfterResume", model="collection-admission"),
    _case("collection-admission-decline-skips-log",
          "observed: a human decline appends its receipt without reading an already stored resume",
          ("ReleaseAdmissionLater", "DeclineSkipsLog"), "NoDeclineAfterResume", model="collection-admission"),
    _case("collection-admission-stop-ignored",
          "seeded: a durable Stop does not block the automatic resume receipt",
          ("StopIgnored",), "StopPrecedesResume", model="collection-admission"),
    *(_case(name, "witness", witness=witness, model="collection-admission") for name, witness in (
        ("collection-admission-reach-resume", "NeverResumeCommitted"),
        ("collection-admission-reach-decline", "NeverDeclineWins"),
        ("collection-admission-reach-late-decline", "NeverLateDeclineRefused"),
        ("collection-admission-reach-stop", "NeverStopBlocksResume"),
    )),
    _case("route-current", "three confirmed route refusals fail the call without a model retry",
          model="route"),
    _case("route-unbounded", "historical, shipped through v0.1.69: a confirmed route refusal kept the same call waiting",
          ("UnboundedRouteRecovery",), "RouteRefusalStops", model="route"),
    _case("route-exhaustion-retries", "seeded: a capped route refusal is classified as another model retry",
          ("ExhaustionStartsModelRetry",), "NoRetryAfterExhaustion", model="route"),
    *(_case(name, "witness", witness=witness, model="route") for name, witness in (
        ("route-reach-failed", "NeverFailed"),
        ("route-reach-recovered", "NeverRecovered"),
        ("route-reach-cancelled", "NeverCancelled"),
        ("route-reach-other", "NeverOtherError"),
    )),
    _case("renderer-recovery-current",
          "a flat visible body fails paint; an unknown capture does not; one foreground reload precedes a successor",
          model="renderer-recovery"),
    _case("renderer-recovery-ignore-presented",
          "historical, shipped through v0.1.77: a matching WebKit snapshot counted as healthy paint while the visible body stayed flat",
          ("IgnorePresentedFrame",), "VisibleFlatIsNotHealthy", model="renderer-recovery"),
    _case("renderer-recovery-unknown-as-flat",
          "seeded: a missed, hidden or refused capture counts as a blank body",
          ("UnknownCountsAsFlat",), "UnknownMatchStaysHealthy", model="renderer-recovery"),
    _case("renderer-recovery-skip-reload",
          "seeded: a flat body opens a successor without the one reload",
          ("BlankSkipsReload",), "ReplacementFollowsReload", model="renderer-recovery"),
    _case("renderer-recovery-background-spends",
          "seeded: a failure spends the recovery budget while the window is away",
          ("BackgroundSpendsRecovery",), "RecoveryStaysInForeground", model="renderer-recovery"),
    *(_case(name, "witness", witness=witness, model="renderer-recovery") for name, witness in (
        ("renderer-recovery-reach-content", "NeverHealthyContent"),
        ("renderer-recovery-reach-blank", "NeverBlank"),
        ("renderer-recovery-reach-reload", "NeverReloaded"),
        ("renderer-recovery-reach-replace", "NeverReplaced"),
        ("renderer-recovery-reach-unknown", "NeverUnknownHealthy"),
    )),
    _case("diagram-paint-current",
          "a Mermaid SVG becomes an image only when every paint axis is inside its budget",
          model="diagram-paint"),
    *(_case(name, source, (switch,), invariant, model="diagram-paint")
      for name, source, switch, invariant in (
        ("diagram-paint-ignore-edge", "seeded: an edge past the budget is still mounted",
         "IgnoreEdge", "MountedEdge"),
        ("diagram-paint-ignore-pixels", "seeded: a legal edge with too much area is still mounted",
         "IgnorePixels", "MountedPixels"),
        ("diagram-paint-ignore-elements", "seeded: an element tree past the budget is still mounted",
         "IgnoreElements", "MountedElements"),
        ("diagram-paint-ignore-text", "seeded: a text run past the budget is still mounted",
         "IgnoreText", "MountedText"),
        ("diagram-paint-ignore-bytes", "seeded: a document past the byte budget is still mounted",
         "IgnoreBytes", "MountedBytes"),
    )),
    *(_case(name, "witness", witness=witness, model="diagram-paint")
      for name, witness in (
        ("diagram-paint-reach-mount", "NeverMounted"),
        ("diagram-paint-reach-source", "NeverSource"),
    )),
    _case("installed-skills-current", "validate, publish and detach whole project skill bundles",
          model="installed-skills"),
    *(_case(name, source, (switch,), invariant, model="installed-skills")
      for name, source, switch, invariant in (
        ("installed-skills-unvalidated", "seeded: publish an unvalidated stage",
         "PublishWithoutValidation", "VisibleBundleValidated"),
        ("installed-skills-overwrite", "seeded: a second installer overwrites the published package",
         "PublishOverExisting", "ExistingBundlePreserved"),
        ("installed-skills-cleanup-visible", "seeded: keep the retired package visible during cleanup",
         "CleanupBeforeHide", "RemovedBundleHidden"),
        ("installed-skills-cross-project", "seeded: load falls back to another project",
         "ReadOtherProject", "ReadIsProjectScoped"),
        ("installed-skills-learned-fallback", "seeded: load falls back to a same-named learned skill",
         "LoadFallsBackToLearned", "LoadIsExternalOnly"),
        ("installed-skills-list-body", "seeded: listing includes the instruction body",
         "ListIncludesBody", "ListDisclosesMetadataOnly"),
        ("installed-skills-review-rewrites", "seeded: learned review rewrites an installed bundle",
         "ReviewTouchesInstall", "ReviewPreservesInstalled"),
        ("installed-skills-publish-after-project-delete", "seeded: a staged upload ignores project deletion",
         "PublishAfterProjectDeletion", "ProjectDeletionIsFinal"),
    )),
    *(_case(name, "witness", witness=witness, model="installed-skills") for name, witness in (
        ("installed-skills-reach-list", "NeverInstalledList"),
        ("installed-skills-reach-load", "NeverInstalledLoad"),
        ("installed-skills-reach-invalid", "NeverInvalidStage"),
        ("installed-skills-reach-install-race", "NeverCompetingInstalls"),
        ("installed-skills-reach-learned-coexistence", "NeverSameNameLearnedAndInstalled"),
        ("installed-skills-reach-learned-load-miss", "NeverLearnedOnlyLoadMiss"),
        ("installed-skills-reach-two-projects", "NeverSameNameAcrossProjects"),
        ("installed-skills-reach-hidden-view", "NeverDetachedLookup"),
        ("installed-skills-reach-reinstall", "NeverReinstalledWithPendingCleanup"),
        ("installed-skills-reach-delete-with-stage", "NeverDeletedWithStagedUpload"),
    )),
    _case("peer-inventory-current", "UID separates reused PIDs; terminal claims still required", model="peer-inventory"),
    _case("peer-inventory-numeric-pid", "old numeric PID attributes foreign children to dead queue",
          ("NumericPIDOwnsChildren",), "ForeignReuseDoesNotBlock", model="peer-inventory"),
    _case("peer-inventory-claims", "missing or live effect receipt is ignored",
          ("IgnoreClaims",), "ClaimsRequired", model="peer-inventory"),
    _case("peer-inventory-owned-children", "same-UID ambiguous children are ignored",
          ("IgnoreSameUIDChildren",), "SameUIDChildrenRefuse", model="peer-inventory"),
    _case("peer-inventory-active", "an active controller is mistaken for dead history",
          ("IgnoreActiveController",), "ActiveControllerBusy", model="peer-inventory"),
    _case("peer-inventory-reach-quiet", "witness", witness="NeverForeignQuiet", model="peer-inventory"),
    _case("peer-inventory-reach-refusal", "witness", witness="NeverUnresolvedRefused", model="peer-inventory"),
    _case("device-login-current", "validated manual login and explicit host browser launch", model="device-login"),
    _case("device-login-browser-required", "old browser gate refuses a headless login",
          ("RequireBrowser",), "ManualLoginAvailable", model="device-login"),
    _case("device-login-unsafe-url", "unvalidated Gateway URL reaches local status",
          ("PublishUnsafe",), "OnlyValidatedURL", model="device-login"),
    _case("device-login-terminal-url", "terminal status retains a pending link",
          ("RetainTerminalURL",), "TerminalHidesURL", model="device-login"),
    _case("device-login-bypass", "credentials activate without human approval",
          ("BypassApproval",), "ApprovalRequired", model="device-login"),
    _case("device-login-unrequested-launch", "observed: login invokes an available host launcher without launch intent",
          ("LaunchWithoutOptIn",), "BrowserLaunchRequiresOptIn", model="device-login"),
    *(_case(name, "witness", witness=witness, model="device-login") for name, witness in (
        ("device-login-reach-manual", "NeverManualAuthorized"),
        ("device-login-reach-default-with-launcher", "NeverDefaultAuthorizedWithLauncher"),
        ("device-login-reach-opted-in-launch", "NeverOptedInLaunch"),
        ("device-login-reach-opted-in-fallback", "NeverOptedInFallback"),
        ("device-login-reach-unsafe", "NeverUnsafeRejected"),
        ("device-login-reach-cancel", "NeverCancelledPending"),
    )),
    _case("linux-release-current", "two validated architecture archives; exact runtime receipts and immutable public bytes", model="linux-release"),
    _case("linux-release-arm-only", "original release only supports arm64", ("ArmOnly",), "BothArchitecturesSupported", model="linux-release"),
    _case("linux-release-machine", "archive name disagrees with ELF machine", ("IgnoreMachine",), "MachineMatchesName", model="linux-release"),
    _case("linux-release-receipt", "new upload lacks matching architecture/source/bytes evidence", ("IgnoreReceipt",), "ReceiptMatchesNewUpload", model="linux-release"),
    _case("linux-release-rebuild", "partial publication rebuilds existing public bytes", ("RebuildPublic",), "PublicBytesPreserved", model="linux-release"),
    *(_case(name, "witness", witness=witness, model="linux-release") for name, witness in (
        ("linux-release-reach-amd64", "NeverAMD64Uploaded"),
        ("linux-release-reach-arm64", "NeverARM64Uploaded"),
        ("linux-release-reach-retry", "NeverPublicRetry"),
    )),
    _case("account-current", "publish rechecks the credential and login or logout seals the catalog",
          model="account"),
    _case("account-stale-observation", "a fetch publishes the credential it observed after that credential changed",
          ("StaleObservation",), "PublishedAgrees", model="account"),
    _case("account-unsealed-logout", "logout changes the credential and leaves the previous catalog published",
          ("UnsealedLogout",), "PublishedAgrees", model="account"),
    *(_case(name, "witness", witness=witness, model="account") for name, witness in (
        ("account-reach-sign-out", "NeverSignedOut"),
        ("account-reach-in-flight", "NeverInFlight"),
        ("account-reach-other-account", "NeverOtherAccount"),
    )),
    _case("default-source-current", "the settings badge matches the engine starting source and a ready server catalog is selectable",
          model="default-source"),
    _case("default-source-badge-ignores-source", "the own-provider list marked models.default while new conversations started on the server",
          ("BadgeIgnoresSource",), "BadgeMatchesStart", model="default-source"),
    _case("default-source-choice-hidden", "a ready server catalog had no selectable default row beside the own providers",
          ("ServerChoiceHidden",), "ServerChoiceVisible", model="default-source"),
    _case("default-source-official-keeps-own", "official presentation still started a new conversation on the stored own provider",
          ("OfficialKeepsOwnProviders",), "OfficialStartsOnServer", model="default-source"),
    _case("default-source-official-lists-own", "official presentation still offered the own providers beside the official model",
          ("OfficialListsOwnProviders",), "OfficialHidesOwnProviders", model="default-source"),
    *(_case(name, "witness", witness=witness, model="default-source") for name, witness in (
        ("default-source-reach-ready", "NeverReady"),
        ("default-source-reach-provider", "NeverProvider"),
        ("default-source-reach-server", "NeverServerStart"),
        ("default-source-reach-official", "NeverOfficial"),
    )),
    _case("manager-commentary-current", "admit ordinary commentary before its tool",
          model="manager-commentary"),
    _case("manager-commentary-historical-defer", "historical 56ae732: defer ordinary commentary until the run ends",
          ("DeferOrdinary",), "OrdinaryBeforeTool", model="manager-commentary"),
    _case("manager-commentary-publish-private", "publish a private candidate before terminal admission",
          ("PublishPrivate",), "PrivateProseNotPublic", model="manager-commentary"),
    _case("manager-commentary-private-history", "keep unadmitted prose in the next model request",
          ("KeepPrivateInHistory",), "PrivateProseNotInHistory", model="manager-commentary"),
    _case("manager-commentary-no-evidence", "accept a terminal claim without matching durable evidence",
          ("AcceptWithoutEvidence",), "ClaimAdmissionHasEvidence", model="manager-commentary"),
    _case("manager-commentary-after-stop", "append commentary after Stop acknowledged",
          ("PublishAfterStop",), "StopKeepsPublishedPrefix", model="manager-commentary"),
    *(_case(name, "witness", witness=witness, model="manager-commentary") for name, witness in (
        ("manager-commentary-reach-two-rounds", "NeverTwoOrdinaryRounds"),
        ("manager-commentary-reach-private-claim-tool", "NeverPrivateClaimAndToolProgress"),
        ("manager-commentary-reach-terminal-verified-claim", "NeverTerminalVerifiedClaim"),
    )),
    # Real UI-ID callback scheduling witness; target remains a pre-code model gate.
    _askpub("askpub-knownfault-publication", "observed clean EOF lost after actual checkpoint/cancel",
            ("LateConsumerCancelMasksEOF", "WaiterUsesStoreIdentityOnly"), "ExecutedCleanCallPublishedWhenUIDone"),
    _askpub("askpub-knownfault-waiter-identity", "actual compose ID present but waiter/store ID empty",
            ("LateConsumerCancelMasksEOF", "WaiterUsesStoreIdentityOnly"), "WaiterKeepsContextIdentity"),
    _askpub("askpub-knownfault-checkpoint-identity", "actual pending-ask checkpoint kept an empty ID",
            ("LateConsumerCancelMasksEOF", "WaiterUsesStoreIdentityOnly"), "CheckpointPendingKeepsIdentity"),
    _askpub("askpub-target-current", "proposed target: source fact and actual context identity retained"),
    _askpub("askpub-control-external", "external effect still needs a genuine result receipt", model="ask-publication-external"),
    _askpub("askpub-control-finish", "finish-present excludes missing-finish classification branch",
            ("LateConsumerCancelMasksEOF",), model="ask-publication-finish"),
    _askpub("askpub-current-finish", "current finish-present input checks every safety property",
            model="ask-publication-finish"),
    _askpub("askpub-control-source-error", "source failure cannot invent a tool call", model="ask-publication-error"),
    _askpub("askpub-control-incomplete", "incomplete arguments cannot invent a complete call", model="ask-publication-incomplete"),
    _askpub("askpub-reach-executed-before-publication", "witness", witness="NeverExecutedBeforePublication"),
    _askpub("askpub-reach-accepted-before-publication", "target reachability, not an observed answer-ACK fault",
            witness="NeverAcceptedBeforePublication"),
    _askpub("askpub-reach-late-owner-cancel", "witness", witness="NeverLateOwnerCancel"),
    _askpub("askpub-reach-pending-checkpoint", "witness", witness="NeverPendingCheckpoint"),
    _askpub("askpub-reach-external-unknown", "witness", witness="NeverExternalUnknown", model="ask-publication-external"),
    _askpub("askpub-reach-finish-late-drain", "witness",
            witness="NeverFinishLateDrain", model="ask-publication-finish"),
    _askpub("askpub-reach-source-error", "witness", witness="NeverErrorDrain", model="ask-publication-error"),
    _askpub("askpub-reach-incomplete", "witness", witness="NeverIncompleteDrain", model="ask-publication-incomplete"),
    _case("generation-recovery-current", "joined typed-cap runner before same-Registry recovery", model="generation-recovery"),
    _case("generation-recovery-no-join", "known scheduler-injected old AfterModel SetHistory overwrites a later real Registry/HTTP RunWith",
          ("NoJoin",), "RecoveredHistoryOwnsEpoch", model="generation-recovery"),
    _case("generation-recovery-reach-recovery", "witness", witness="NeverRecovery", model="generation-recovery"),
    _case("generation-recovery-reach-cancel-after-claim", "witness", witness="NeverCancelledAfterClaim", model="generation-recovery"),
    _case("generation-recovery-reach-second-cap", "witness", witness="NeverSecondCap", model="generation-recovery"),
    _case("work-pruning-current", "work pruning and dependency review protocol", model="work-pruning"),
    _case("work-pruning-deletes-record", "seeded: pruning deletes a persisted record",
          ("PruneDeletesRecord",), "StoredIdentitySurvivesPruning", model="work-pruning"),
    _case("work-pruning-direct-only", "seeded: only immediate prerequisites of an active record are retained",
          ("DropTransitivePrerequisite",), "InactiveAncestorsStayVisible", model="work-pruning"),
    _case("work-pruning-keeps-all", "seeded: the default graph renders every inactive record",
          ("KeepEveryInactive",), "UnneededInactiveRecordsCollapse", model="work-pruning"),
    _case("work-pruning-sticky", "seeded: a stored pruning mark keeps a restored record hidden",
          ("RememberPruned",), "ActiveRecordsAppear", model="work-pruning"),
    _case("work-pruning-reconfirm-on-restore", "seeded: restoring a source silently acknowledges its new version",
          ("ReconfirmOnRestore",), "PinsRequireExplicitReconfirmation", model="work-pruning"),
    _case("work-pruning-reconfirm-on-patch", "seeded: editing unrelated fields silently acknowledges a prerequisite",
          ("ReconfirmOnPatch",), "PinsRequireExplicitReconfirmation", model="work-pruning"),
    _case("work-pruning-ignores-version", "seeded: an active prerequisite's new version does not mark a dependent stale",
          ("IgnorePinnedVersion",), "WarningsReflectStatusAndVersion", model="work-pruning"),
    _case("work-pruning-ignores-inactive", "seeded: reconfirming an inactive version hides its warning",
          ("IgnoreInactiveSource",), "WarningsReflectStatusAndVersion", model="work-pruning"),
    _case("work-pruning-reach-collapsed-branch", "an entirely inactive branch folds out of the default graph",
          witness="NeverCollapseWholeBranch", model="work-pruning"),
    _case("work-pruning-reach-inactive-chain", "an active result retains both levels of inactive prerequisites",
          witness="NeverRetainInactiveChain", model="work-pruning"),
    _case("work-pruning-reach-restored-id", "a collapsed record returns with its original identity",
          witness="NeverRestoreOriginalID", model="work-pruning"),
    _case("work-pruning-reach-restored-stale", "after restoration a dependent still warns on the old version",
          witness="NeverRestoreWithStaleDependent", model="work-pruning"),
    _case("work-pruning-reach-confirmed", "explicit reconfirmation of an active restored prerequisite clears the warning",
          witness="NeverClearConfirmedDependency", model="work-pruning"),
    _case("work-pruning-reach-reveal", "the full view can show a record omitted from the default graph",
          witness="NeverRevealCollapsedRecord", model="work-pruning"),
    _case("nuwa-current", "Nuwa current protocol", model="nuwa"),
    _case("nuwa-rebind-on-restore", "pre-fix WorkerCheckpoint omitted the selected model",
          ("RebindOnRestore",), "BindingStable", model="nuwa"),
    _case("nuwa-parent-only-stop", "seeded descendant escapes Registry cancellation",
          ("CancelParentOnly",), "StopOwnsDescendants", model="nuwa"),
    _case("nuwa-late-result", "seeded callback writes after acknowledged Stop",
          ("WriteAfterStop",), "NoLateFact", model="nuwa"),
    _case("nuwa-cross-project", "seeded model-supplied evidence changes project ownership",
          ("CrossProjectEvidence",), "ProjectScoped", model="nuwa"),
    _case("nuwa-uncommitted-revision", "seeded revision publication before the write commits",
          ("PublishBeforeCommit",), "VisibleIsCommitted", model="nuwa"),
    _case("nuwa-rewind-stale-source", "rewind erases a source without invalidating collaboration",
          ("RewindSkipsRevision",), "SourceChangeHasRevision", model="nuwa"),
    _case("nuwa-delete-moved-stale-source", "deleting a moved author leaves the original project revision stale",
          ("DeleteMovedSkipsRevision",), "SourceChangeHasRevision", model="nuwa"),
    _case("nuwa-promote-without-closure", "seeded: a shared record still depends on private work",
          ("PromoteWithoutClosure",), "ProjectDependencyClosure", model="nuwa"),
    _case("nuwa-private-leak", "seeded: thread work leaks to another reader",
          ("PrivateLeak",), "PrivateWorkIsOwned", model="nuwa"),
    _case("nuwa-reach-peer-promotion", "a peer can see both committed shared nodes",
          witness="NeverPeerSeesPromotion", model="nuwa"),
    _case("nuwa-reach-standalone-work", "standalone conversation can read its own work",
          witness="NeverStandaloneReadsOwn", model="nuwa"),
    _case("nuwa-merge-stale-thread", "seeded: confirm against an older conversation revision",
          ("PromoteIgnoresThreadRevision",), "MergeAdmissionUsesSnapshot", model="nuwa"),
    _case("nuwa-merge-stale-project", "seeded: confirm against an older project revision",
          ("PromoteIgnoresProjectRevision",), "MergeAdmissionUsesSnapshot", model="nuwa"),
    _case("nuwa-merge-moved-owner", "seeded: confirmation rebinds to a different project after a move",
          ("PromoteIgnoresMove",), "MergeAdmissionUsesSnapshot", model="nuwa"),
    _case("nuwa-merge-stale-generation", "seeded: restored data accepts a pre-restore confirmation with equal counters",
          ("PromoteIgnoresDataGeneration",), "MergeAdmissionUsesSnapshot", model="nuwa"),
    _case("nuwa-merge-partial-commit", "seeded: a prerequisite or its revision survives a failed transaction",
          ("PromotePartiallyCommits",), "MergeRollbackKeepsCommittedState", model="nuwa"),
    _case("nuwa-merge-early-publication", "seeded: a peer sees a staged prerequisite before commit",
          ("PromotePublishesStaged",), "MergePublicationIsCommitted", model="nuwa"),
    _case("nuwa-merge-open-closure", "seeded: root commit lacks a required dependency and revision",
          ("PromoteWithoutClosure",), "MergeCommitsClosureAndRevisions", model="nuwa"),
    _case("nuwa-merge-reach-publication", "the committed closure and both revisions can become visible",
          witness="NeverCommittedMergeIsVisible", model="nuwa"),
    _case("nuwa-merge-reach-rollback", "a prerequisite write can be followed by rollback",
          witness="NeverMergeRollsBackFirstWrite", model="nuwa"),
    _case("nuwa-merge-reach-stale-thread", "a private change can refuse a confirmed merge",
          witness="NeverMergeRejectsStaleThread", model="nuwa"),
    _case("nuwa-merge-reach-stale-project", "a peer's published work can refuse a confirmed merge",
          witness="NeverMergeRejectsStaleProject", model="nuwa"),
    _case("nuwa-merge-reach-moved-owner", "a move before submit can refuse a confirmed merge",
          witness="NeverMergeRejectsMovedOwner", model="nuwa"),
    _case("nuwa-merge-reach-restore", "a restored equal revision pair can still refuse a confirmed merge",
          witness="NeverMergeRejectsRestore", model="nuwa"),
    _case("nuwa-merge-reach-standalone", "a valid snapshot can refuse promotion without a destination",
          witness="NeverMergeRefusesStandalone", model="nuwa"),
    _case("nuwa-reach-restore", "witness", witness="NeverRestored", model="nuwa"),
    _case("nuwa-reach-result", "witness", witness="NeverSharedResult", model="nuwa"),
    _case("nuwa-reach-delegation", "witness", witness="NeverDelegated", model="nuwa"),
    _case("headless-web-unheld", "pre-170: serveWeb reports ready but never holds presence",
          ("HeadlessWaiterUnheld",), "WindowHasEngine"),
    _case("current", "current design: every switch off"),
    _case("restore-owner-checked-late", "tagged project checked only after runtime occupation",
          ("RestoreOwnerCheckedLate",), "RestoreOwnsProject"),
    _case("reach-refused-foreign-runtime", "witness", witness="NeverRefusedForeignRuntime"),
    _case("reach-refused-panel-recovery", "panel storage/identity failure refuses only this conversation before runtime occupancy", witness="NeverRefusedPanelRecovery"),
    # Historical: the shipped code before these commits had the behaviour.
    _case("pre-d77e8d4", "historical, before d77e8d4: the restart deadline bounded the replacement "
          "child, which restored every task before publishing its lease",
          ("DeadlineKillsChild", "RestoreBeforeListen"), "CheckpointHasOwner"),
    _case("deadline-kills-child", "historical part of d77e8d4: only the deadline kill",
          ("DeadlineKillsChild",), "CheckpointHasOwner"),
    _case("pre-f341e05", "historical, before f341e05: the window opened before holding presence "
          "and idle exit acted on an earlier sample",
          ("PresenceAfterWindow", "IdleCommitNonAtomic"), "WindowHasEngine"),
    _case("hold-without-atomic-idle", "historical part of f341e05: holding presence first is not "
          "enough while idle exit acts on an earlier sample",
          ("IdleCommitNonAtomic",), "WindowHasEngine"),
    _case("window-before-presence", "historical part of f341e05: an atomic idle commit is not "
          "enough while the window opens before holding presence",
          ("PresenceAfterWindow",), "WindowHasEngine"),
    # Design hazards: guards the current code has, each removed in turn.
    _case("stop-races-restore", "guard added with d77e8d4: restoreMu serializes Stop with one "
          "conversation's restoration once the replacement serves before restoring",
          ("StopRacesRestore",), "StopIsFinal"),
    _case("finish-ungated", "guard from 5d7b305: handoff waits for a normal finalization "
          "(lockFinish)", ("FinishUngated",), "WorkHasLiveOwner"),
    _case("summary-uses-write-budget", "observed blocked tools leave too little of the request budget for final persistence",
          ("SummaryUsesWriteBudget",), "SummaryLeavesWriteBudget"),
    _case("parked-not-checkpointed", "guard from 5d7b305: the checkpoint covers parked registries",
          ("ParkedNotCheckpointed",), "WorkHasLiveOwner"),
    _case("idle-counts-only-managers", "guard required by f341e05: idle load includes parked "
          "workers, restoration and handoff (Engine.ActiveWork)",
          ("IdleCountsOnlyManagers",), "WorkHasLiveOwner"),
    _case("parked-successor-detached", "seeded split choose/take then occupy: retained workers "
          "temporarily disappear from the runtime before successor admission",
          ("ParkedAdmissionNonAtomic",), "RegistryAlwaysAccounted"),
    _case("parked-owner-checked-late", "same runtime accepts a successor from another project",
          ("ParkedOwnerCheckedLate",), "ParkedSuccessorOwnsProject"),
    # Witnesses: the scenarios behind the invariants are reachable.
    _case("reach-restored-window", "witness", witness="NeverRestoredIntoWindow"),
    _case("reach-parked-checkpoint", "witness", witness="NeverParkedCheckpoint"),
    _case("reach-parked-successor", "witness", witness="NeverParkedSuccessor"),
    _case("reach-refused-parked-successor", "witness", witness="NeverRefusedParkedSuccessor"),
    _case("reach-refused-foreign-parked-successor", "witness", witness="NeverRefusedForeignParkedSuccessor"),
    _case("reach-stop-before-restore", "witness", witness="NeverStopBeforeRestore"),
    _case("reach-opened-after-deadline", "witness", witness="NeverOpenedAfterDeadline"),
    _case("reach-idle-exit-after-close", "witness", witness="NeverIdleExitAfterClose"),
    _case("reach-relaunch-after-idle-commit", "witness", witness="NeverRelaunchAfterIdleCommit"),
    _case("reach-finish-waits-for-handoff", "witness", witness="NeverFinishWaitsForHandoff"),
    _case("reach-ack-with-unknown-tool", "witness", witness="NeverAckWithUnknownTool"),
    # C-037 refinement: a saved project read is not an owner claim.
    _case("project-move-current", "serialize Move with claim and explicit goal resume", model="project-move"),
    _case("project-move-stale-claim", "Move commits after tagged owner lookup, before Registry occupation",
          ("UnorderedClaim",), "ClaimOwnsProject", model="project-move"),
    _case("project-move-false-goal-resume", "Move commits after goal preflight, before its durable receipt",
          ("UnorderedGoal",), "ResumeReceiptOwnsProject", model="project-move"),
    _case("project-move-refusal-drops-parked", "foreign owner refusal discards the parked registry",
          ("RefusalDropsParked",), "RefusalKeepsParked", model="project-move"),
    *(_case(name, "witness", witness=witness, model="project-move") for name, witness in (
        ("project-move-reach-wake", "NeverWakeClaim"),
        ("project-move-reach-goal", "NeverGoalClaim"),
        ("project-move-reach-refusal", "NeverMoveBeforeRefusal"),
        ("project-move-reach-after-claim", "NeverMoveAfterClaim"),
    )),
    # C-003 event stream.
    _stream("stream-current", "current design: every switch off"),
    _stream("stream-v0.1.45-lost-marker", "historical, shipped through v0.1.45: the rewind marker was "
            "broadcast after recordMu with seq 0 and never stored, so a slow or reconnecting reader "
            "kept the truncated rows", ("MarkerUnstored", "MarkerAfterUnlock"), "ConvergedView"),
    _stream("stream-v0.1.45-seq-reuse", "historical, shipped through v0.1.45: with nothing stored at "
            "the cut, a restart reseeded the counter below seqs already issued",
            ("MarkerUnstored", "MarkerAfterUnlock"), "SeqIdentity"),
    _stream("stream-marker-unstored", "historical part of the v0.1.45 design: an unstored marker alone loses "
            "the cut for a slow reader", ("MarkerUnstored",), "ConvergedView"),
    _stream("stream-marker-unstored-reuse", "historical part of the v0.1.45 design: an unstored marker alone "
            "lets a restart reuse seqs", ("MarkerUnstored",), "SeqIdentity"),
    _stream("stream-marker-after-unlock", "historical part of the v0.1.45 design: a marker broadcast after "
            "recordMu races a concurrent row, which the reader then drops or erases",
            ("MarkerAfterUnlock",), "ConvergedView"),
    _stream("stream-broadcast-after-unlock", "guard since cb0edb5: Engine.record broadcasts under "
            "recordMu, so stored and streamed order agree", ("BroadcastAfterUnlock",), "ConvergedView"),
    _stream("stream-drop-when-slow", "guard since cb0edb5: a stored row that does not fit a slow "
            "subscriber flags it lagged", ("DropWhenSlow",), "ConvergedView"),
    _stream("stream-reach-catch-up-across-rewind", "witness", witness="NeverCatchUpAcrossRewind"),
    _stream("stream-reach-record-after-restart", "witness", witness="NeverRecordAfterRestart"),
    # C-009 / C-012: human cancellation and manager arming authority.
    _scheduling("schedule-current", "required scheduling authorization protocol: every switch off"),
    _scheduling("schedule-pre-cancel-authority", "historical d0a6d83: human deletion cancelled one row without withdrawing waiting authority",
                ("CancelDoesNotRevoke",), "EveryArmAuthorized"),
    _scheduling("schedule-cancel-receipt-required", "guard: every first human cancellation records its authority",
                ("CancelDoesNotRevoke",), "CancellationIsDurable"),
    _scheduling("schedule-claimed-cancel-ignored", "guard found by this model: a claimed one-shot is done before its running check ends, but the first human cancellation must still revoke waiting",
                ("ClaimedCancelIgnored",), "ClaimedCancellationStopsReport"),
    _scheduling("schedule-manager-cancel-masks-human", "guard found by this model: a manager-cancelled row must not mask the first real human cancellation",
                ("ManagerCancelledMasksHuman",), "EveryArmAuthorized"),
    _scheduling("schedule-continue-grants", "guard: ordinary human continuation is unchanged, not permission",
                ("ContinueGrantsPermission",), "EveryArmAuthorized"),
    _scheduling("schedule-late-allow", "guard: classification completion cannot reorder original human decisions",
                ("LateAllowOverridesCancel",), "EveryArmAuthorized"),
    _scheduling("schedule-stale-policy-read", "guard: create, replacement and report rearm each recheck policy in their write transaction",
                ("CommitSkipsPolicyRecheck",), "EveryArmAuthorized"),
    _scheduling("schedule-restart-forgets", "guard: restart reads durable human authority independently of manager replay",
                ("RestartForgetsPolicy",), "EveryArmAuthorized"),
    _scheduling("schedule-compact-forgets", "guard: compaction does not replace durable human authority with its summary",
                ("CompactForgetsPolicy",), "EveryArmAuthorized"),
    _scheduling("schedule-split-cancellation", "guard: cancellation and the typed human receipt commit atomically",
                ("CancelReceiptNotAtomic",), "CancellationIsAtomic"),
    _scheduling("schedule-cancel-moved-target", "guard: the human cancellation's selected destination must still match at mutation",
                ("CancelDestinationUnchecked",), "CancellationKeepsSelectedTarget"),
    _scheduling("schedule-retarget-after-terminal", "guard found by this model: PATCH must revalidate editable status in its mutation transaction",
                ("RetargetIgnoresStatus",), "TerminalDestinationStable"),
    _scheduling("schedule-repeat-delete-revokes", "guard: repeating an old human DELETE cannot revoke a later explicit allowance",
                ("RepeatedDeleteRevokes",), "DuplicateDeleteKeepsLaterAllowance"),
    *(_scheduling(name, "witness", witness=witness) for name, witness in (
        ("schedule-reach-claimed-cancel", "NeverCancelClaimed"),
        ("schedule-reach-manager-then-human-cancel", "NeverCancelManagerCancelled"),
        ("schedule-reach-fresh-after-cancel-allow", "NeverFreshAfterCancellation"),
        ("schedule-reach-repeat-after-allow", "NeverRepeatAfterAllowance"),
        ("schedule-reach-denied-arm", "NeverRefuseDeniedArm"),
        ("schedule-reach-moved-cancel-refusal", "NeverRefuseMovedCancel"),
        ("schedule-reach-cancel-storage-refusal", "NeverRefuseCancelStorage"),
        ("schedule-reach-terminal-edit-refusal", "NeverRefuseTerminalEdit"),
        ("schedule-reach-claimed-edit-refusal", "NeverRefuseClaimedEdit"),
        ("schedule-reach-restart-compact", "NeverRetainAcrossRestartAndCompact"),
        ("schedule-reach-human-active-denied", "NeverHumanActiveWhileDenied"),
        ("schedule-reach-replace", "NeverReplace"),
        ("schedule-reach-report", "NeverReport"),
    )),
    # Orthogonal multi-event authority timeline; row lifecycle stays above.
    _consent("consent-current", "current source-ordered human consent protocol"),
    _consent("consent-pre-cancel-authority", "historical d0a6d83: human cancellation did not supply durable waiting authority",
             ("HumanCancelDoesNotWithdraw",), "EveryArmAuthorized"),
    _consent("consent-completion-order-arm", "seeded guard removal: classification return order replaces original input order",
             ("ClassificationCompletionOverridesSequence",), "EveryArmAuthorized"),
    _consent("consent-completion-order-policy", "seeded guard removal: settled policy must retain the latest original decisive source",
             ("ClassificationCompletionOverridesSequence",), "SettledPolicyMatchesLatestIntent"),
    _consent("consent-cancel-dedupe-arm", "seeded guard removal: another schedule ID inherits the conversation's old cancellation receipt",
             ("CancelDeduplicatesByThread",), "EveryArmAuthorized"),
    _consent("consent-cancel-dedupe-policy", "seeded guard removal: a second schedule ID must supply its own decisive human cancellation",
             ("CancelDeduplicatesByThread",), "SettledPolicyMatchesLatestIntent"),
    _consent("consent-duplicate-delete-policy", "seeded guard removal: replaying an old DELETE appends a new denial after later permission",
             ("DuplicateDeleteAppendsDeny",), "LaterAllowanceSurvivesDuplicate"),
    _consent("consent-duplicate-delete-receipt", "seeded guard removal: the exact old human cancellation must have one receipt",
             ("DuplicateDeleteAppendsDeny",), "ExactIDReceiptUniqueness"),
    _consent("consent-pending-arm", "seeded guard removal: unresolved human input cannot admit a manager mutation",
             ("PendingInputDoesNotBlock",), "EveryArmAuthorized"),
    *(_consent(name, "witness", witness=witness) for name, witness in (
        ("consent-reach-full-timeline", "NeverFullHumanTimeline"),
        ("consent-reach-out-of-order-classification", "NeverOutOfOrderClassification"),
        ("consent-reach-late-allow-retains-cancel", "NeverLateAllowPreservesCancellation"),
        ("consent-reach-two-reauthorizations", "NeverTwoRoundsOfReauthorization"),
        ("consent-reach-second-id-cancel", "NeverSecondIDRevokesLaterAllowance"),
        ("consent-reach-repeat-after-allow", "NeverDuplicatePreservesLaterAllowance"),
        ("consent-reach-pending-refusal", "NeverPendingRefusal"),
        ("consent-reach-storage-refusal", "NeverStorageRefusal"),
        ("consent-reach-continue-retains-deny", "NeverOrdinaryContinuePreservesDenial"),
        ("consent-reach-second-allow-survives-late-result", "NeverSecondAllowSurvivesEarlierResult"),
    )),
    _case("wake-backfill-current", "per-call budget and a fresh write-time policy check", model="wake-backfill"),
    _case("wake-backfill-pre-fix", "observed same-turn retry makes no progress after its first batch",
          ("PerTurnBudget",), "PendingBackfillAdvances", model="wake-backfill"),
    _case("wake-backfill-stale-write", "seeded write ignores a new human instruction after verification",
          ("SkipWritePolicyRecheck",), "EveryArmAuthorized", model="wake-backfill"),
    *(_case("wake-backfill-reach-" + name, "same-turn backfill reachability", witness=witness, model="wake-backfill")
      for name, witness in (
          ("pending", "NeverBatchPending"),
          ("arm", "NeverSameTurnArm"),
          ("late-input", "NeverWritePending"),
          ("denial", "NeverDenialSettles"),
      )),
)


def render_config(case: Case) -> str:
    model = MODELS[case.model]
    lines = ["SPECIFICATION Spec", "CONSTANTS"]
    for name in model.switches:
        lines.append(f"  {name} = {'TRUE' if name in case.switches else 'FALSE'}")
    for name, value in model.constants:
        lines.append(f"  {name} = {value}")
    lines.append("INVARIANTS " + " ".join(case.invariants()))
    if model.properties and not case.switches and case.expected() is None:
        lines.append("PROPERTIES " + " ".join(model.properties))
    return "\n".join(lines) + "\n"


_admission_model, _admission_cases = provider_admission_cases(ROOT, Model, _case)
MODELS[_admission_model.name] = _admission_model
CASES += _admission_cases

@dataclass(frozen=True)
class Outcome:
    passed: bool  # TLC finished without finding an error
    violated: str | None  # invariant TLC reported, if any
    distinct: int | None  # distinct states found
    error: str | None  # anything TLC reported that is neither


_VIOLATED = re.compile(r"^Error: Invariant (\w+) is violated\.", re.M)
_DISTINCT = re.compile(r"([\d,]+) distinct states found")


def parse_output(text: str) -> Outcome:
    violated = _VIOLATED.search(text)
    # Progress lines may use grouped numbers and precede the final total.
    # The completed exploration, not its first progress sample, is evidence.
    counts = _DISTINCT.findall(text)
    passed = "Model checking completed. No error has been found." in text
    error = None
    if not passed and not violated:
        error = next((ln for ln in text.splitlines() if ln.startswith("Error:")), "TLC did not finish")
    return Outcome(passed, violated.group(1) if violated else None,
                   int(counts[-1].replace(",", "")) if counts else None, error)


def judge(case: Case, outcome: Outcome) -> str | None:
    """Returns why the outcome is not the expected one, or None."""
    if outcome.error:
        return outcome.error
    want = case.expected()
    if want is None:
        return None if outcome.passed else f"expected no violation, TLC reported {outcome.violated}"
    if outcome.violated == want:
        return None
    if outcome.passed:
        return f"expected {want} to be violated, TLC found no error"
    return f"expected {want} to be violated, TLC reported {outcome.violated}"


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def find_jar(explicit: str | None, fetch: bool) -> Path:
    for candidate in (explicit, os.environ.get("TLA2TOOLS_JAR")):
        if candidate:
            path = Path(candidate).expanduser()
            if not path.is_file():
                raise SystemExit(f"tla2tools.jar not found at {path}")
            return path
    if not CACHE_JAR.is_file():
        if not fetch:
            raise SystemExit(f"tla2tools.jar not found; pass --fetch to download {TLA_VERSION} "
                             f"into {CACHE_JAR}, or set TLA2TOOLS_JAR")
        CACHE_JAR.parent.mkdir(parents=True, exist_ok=True)
        partial = CACHE_JAR.with_suffix(".part")
        with urllib.request.urlopen(TLA_URL, timeout=120) as resp, partial.open("wb") as out:
            shutil.copyfileobj(resp, out)
        if sha256(partial) != TLA_SHA256:
            partial.unlink()
            raise SystemExit(f"downloaded tla2tools.jar does not match the pinned SHA-256 {TLA_SHA256}")
        partial.replace(CACHE_JAR)
    elif sha256(CACHE_JAR) != TLA_SHA256:
        raise SystemExit(f"{CACHE_JAR} does not match the pinned SHA-256 {TLA_SHA256}")
    return CACHE_JAR


def write_private_text(path: Path, text: str) -> None:
    with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600), "w") as stream:
        stream.write(text)


def captured_text(value: str | bytes | None) -> str:
    return value.decode("utf-8", errors="replace") if isinstance(value, bytes) else value or ""


def run_case(case: Case, jar: Path, out: Path, timeout: int = 600) -> tuple[Outcome, Path]:
    # TLC resolves the config beside the module, so each case gets a copy.
    spec = MODELS[case.model].spec
    workdir = out / case.name
    workdir.mkdir(parents=True)
    # SANY extracts standard modules into java.io.tmpdir. Concurrent JVMs
    # must not read another check's partially written Naturals/Sequences files.
    java_tmp = workdir.resolve() / "java-tmp"
    java_tmp.mkdir()
    shutil.copy(spec, workdir / spec.name)
    write_private_text(workdir / "case.cfg", render_config(case))
    failure = None
    try:
        proc = subprocess.run(
            ["java", "-Djava.io.tmpdir=" + str(java_tmp), "-XX:+UseParallelGC", "-cp", str(jar), "tlc2.TLC", "-deadlock", "-workers", "auto",
             "-metadir", str(workdir / "states"), "-config", "case.cfg", spec.name],
            cwd=workdir, capture_output=True, text=True, timeout=timeout,
        )
        output = proc.stdout + proc.stderr
        if proc.returncode != 0 and "Model checking completed. No error has been found." in output:
            failure = f"Error: TLC exited with status {proc.returncode} after reporting completion."
    except subprocess.TimeoutExpired as exc:
        # TimeoutExpired may contain bytes even with text=True. Save complete
        # original streams before reporting failure; a partial proof is not a pass.
        output = captured_text(exc.stdout) + captured_text(exc.stderr)
        failure = f"Error: TLC timed out after {timeout}s."
    if failure:
        output += "\n" + failure + "\n"
    log = workdir / "tlc.out"
    write_private_text(log, output)
    shutil.rmtree(workdir / "states", ignore_errors=True)
    outcome = parse_output(output)
    if failure:
        outcome = Outcome(False, outcome.violated, outcome.distinct, failure)
    return outcome, log


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("cases", nargs="*", help="case names (default: all)")
    ap.add_argument("--jar", help="path to tla2tools.jar")
    ap.add_argument("--fetch", action="store_true", help=f"download TLC {TLA_VERSION} if missing")
    ap.add_argument("--out", help="directory for per-case configs and TLC output")
    ap.add_argument("--list", action="store_true", help="list cases and exit")
    args = ap.parse_args(argv)

    by_name = {c.name: c for c in CASES}
    if args.list:
        for c in CASES:
            print(f"{c.name:38} {c.model:9} expect {c.expected() or 'pass':26} {c.source}")
        return 0
    unknown = [n for n in args.cases if n not in by_name]
    if unknown:
        ap.error(f"unknown case: {', '.join(unknown)}")
    selected = [by_name[n] for n in args.cases] or list(CASES)

    jar = find_jar(args.jar, args.fetch)
    out = Path(args.out) if args.out else Path(tempfile.mkdtemp(prefix="zwai-model-check-"))
    out.mkdir(parents=True, exist_ok=True)

    failures = 0
    for case in selected:
        outcome, log = run_case(case, jar, out)
        problem = judge(case, outcome)
        result = "pass" if outcome.passed else f"violates {outcome.violated}"
        states = f"{outcome.distinct} states" if outcome.distinct is not None else "-"
        print(f"{'ok  ' if problem is None else 'FAIL'} {case.name:38} {result:30} {states}")
        if problem:
            failures += 1
            print(f"     {problem}; see {log}")
    print(f"{len(selected) - failures}/{len(selected)} cases as expected; output in {out}")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
