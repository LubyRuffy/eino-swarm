------------------------- MODULE InspectorRegistry -------------------------
EXTENDS Naturals

CONSTANTS
    BorrowSidebarProject,
    IgnoreThreadScope,
    ExposeForeignTaskRows,
    ForgetHistory,
    BlankFallback,
    UnmountHiddenFrame,
    ExposeForeignProjectStatus,
    IgnoreSignalOwner,
    IgnoreSupersededSignal,
    AcceptDeletedSignal,
    ReplayStealsFocus,
    WorkStealsFocus

NoOwner == "none"
Threads == {"A", "B"}
Projects == {"P", "Q"}
ThreadKeys == Threads \cup {NoOwner, "standalone"}
ProjectKeys == Projects \cup {NoOwner}
PanelIDs == {"one", "two"}
Tabs == {"agents", "work", "task"}

ProjectOf(thread) ==
    CASE thread = "A" -> "P" [] thread = "B" -> "Q" [] OTHER -> NoOwner
RowThread(panel) == IF panel = "one" THEN "A" ELSE "B"
ExpectedProject(thread, sidebar) ==
    IF thread = NoOwner THEN sidebar ELSE ProjectOf(thread)

ViewInputs ==
    [thread: ThreadKeys, sidebar: ProjectKeys, mode: {"off", "enabled", "history"},
     store: Threads \cup {NoOwner}, rows: SUBSET PanelIDs, wanted: Tabs,
     statusOwner: ProjectKeys]

BlankView == [thread |-> NoOwner, sidebar |-> NoOwner, mode |-> "off",
              store |-> NoOwner, rows |-> {}, wanted |-> "agents", statusOwner |-> NoOwner]

LiveThreads == Threads \cup {NoOwner}
SignalIDs == PanelIDs \cup {NoOwner}
NoSignal == [owner |-> NoOwner, panel |-> NoOwner, replay |-> FALSE]
NoReceipt == [checked |-> FALSE, opened |-> FALSE, owned |-> FALSE,
              latest |-> FALSE, present |-> FALSE, replay |-> FALSE]
BlankFocus == [active |-> "A", store |-> "A", rows |-> {},
               seen |-> {}, signal |-> NoOwner, queued |-> FALSE,
               pending |-> NoSignal, replayed |-> FALSE, receipt |-> NoReceipt, step |-> 0]

VARIABLES branch, view, focus
vars == <<branch, view, focus>>

Init ==
    /\ branch \in {"render", "focus"}
    /\ view = BlankView /\ focus = BlankFocus

BoundProject ==
    IF BorrowSidebarProject /\ ProjectOf(view.thread) = NoOwner
    THEN view.sidebar ELSE ExpectedProject(view.thread, view.sidebar)
WorkAvailable ==
    \/ view.thread # NoOwner
    \/ BoundProject # NoOwner /\
       (view.mode = "enabled" \/ (view.mode = "history" /\ ~ForgetHistory))
TaskAvailable ==
    /\ view.thread \in Threads
    /\ IF IgnoreThreadScope THEN view.rows # {}
       ELSE view.store = view.thread /\ \E row \in view.rows : RowThread(row) = view.thread
Available ==
    {"agents"} \cup (IF WorkAvailable THEN {"work"} ELSE {})
               \cup (IF TaskAvailable THEN {"task"} ELSE {})
Displayed ==
    IF WorkStealsFocus /\ view.thread # NoOwner /\ WorkAvailable THEN "work"
    ELSE IF BlankFallback \/ view.wanted \in Available THEN view.wanted ELSE "agents"
MountTask == TaskAvailable /\ (Displayed = "task" \/ ~UnmountHiddenFrame)
MountedRows ==
    IF ~MountTask THEN {}
    ELSE IF ExposeForeignTaskRows THEN view.rows
    ELSE {row \in view.rows : RowThread(row) = view.thread}
StatusVisible ==
    Displayed = "work" /\ view.statusOwner # NoOwner /\
    (ExposeForeignProjectStatus \/ view.statusOwner = BoundProject)

(* The store's deduplication is per thread and instance. There is at most one
   captured callback and at most six ordered actions before receiving a receipt.
   The two ID names here are local to each thread, unlike the render fixtures. *)
Discover(panel) ==
    /\ focus.store \in Threads
    /\ <<focus.store, panel>> \notin focus.seen
    /\ focus' = [focus EXCEPT !.rows = @ \cup {panel},
          !.seen = @ \cup {<<focus.store, panel>>}, !.signal = panel,
          !.queued = TRUE, !.step = @ + 1]

Capture ==
    /\ focus.queued /\ focus.pending = NoSignal
    /\ focus' = [focus EXCEPT !.pending =
          [owner |-> focus.store, panel |-> focus.signal, replay |-> FALSE],
          !.queued = FALSE, !.step = @ + 1]

Navigate(thread) ==
    /\ thread # focus.active
    /\ focus' = [focus EXCEPT !.active = thread, !.step = @ + 1]

Reconnect ==
    /\ focus.active # focus.store
    /\ focus' = [focus EXCEPT !.store = focus.active, !.rows = {},
          !.signal = NoOwner, !.queued = FALSE, !.step = @ + 1]

Delete(panel) ==
    /\ panel \in focus.rows
    /\ focus' = [focus EXCEPT !.rows = @ \ {panel}, !.step = @ + 1]

Replay ==
    /\ focus.receipt.opened /\ ~focus.replayed
    /\ focus.pending = NoSignal /\ focus.signal \in focus.rows
    /\ focus' = [focus EXCEPT !.replayed = TRUE,
          !.receipt = NoReceipt,
          !.pending = IF ReplayStealsFocus
                      THEN [owner |-> focus.store, panel |-> focus.signal, replay |-> TRUE]
                      ELSE NoSignal,
          !.step = @ + 1]

Deliver ==
    /\ focus.pending # NoSignal
    /\ LET owned == focus.active = focus.pending.owner /\ focus.store = focus.pending.owner
           latest == focus.signal = focus.pending.panel
           present == focus.pending.panel \in focus.rows
       IN focus' = [focus EXCEPT !.pending = NoSignal, !.step = @ + 1,
             !.receipt = [checked |-> TRUE,
                 opened |-> (owned \/ IgnoreSignalOwner) /\
                            (latest \/ IgnoreSupersededSignal) /\
                            (present \/ AcceptDeletedSignal),
                 owned |-> owned, latest |-> latest, present |-> present,
                 replay |-> focus.pending.replay]]

FocusStep ==
    /\ branch = "focus" /\ focus.step < 6
    /\ UNCHANGED <<branch, view>>
    /\ IF focus.receipt.checked THEN Replay
       ELSE Capture \/ Reconnect \/ Deliver
            \/ (\E thread \in LiveThreads : Navigate(thread))
            \/ (\E panel \in PanelIDs : Discover(panel) \/ Delete(panel))

Render ==
    /\ branch = "render" /\ view = BlankView /\ view' \in ViewInputs
    /\ UNCHANGED <<branch, focus>>

Next == Render \/ FocusStep

Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ branch \in {"render", "focus"} /\ view \in ViewInputs
    /\ focus \in [active: LiveThreads, store: LiveThreads, rows: SUBSET PanelIDs,
       seen: SUBSET (Threads \X PanelIDs), signal: SignalIDs, queued: BOOLEAN,
       pending: [owner: LiveThreads, panel: SignalIDs, replay: BOOLEAN],
       replayed: BOOLEAN, receipt: [checked: BOOLEAN, opened: BOOLEAN, owned: BOOLEAN,
       latest: BOOLEAN, present: BOOLEAN, replay: BOOLEAN], step: 0..6]

ProjectScopeCorrect ==
    branch = "render" /\ WorkAvailable => BoundProject = ExpectedProject(view.thread, view.sidebar)
ThreadScopeCorrect ==
    branch = "render" /\ TaskAvailable =>
    view.store = view.thread /\ (\E row \in view.rows : RowThread(row) = view.thread)
EveryMountedRowIsOwned ==
    branch = "render" => \A row \in MountedRows : RowThread(row) = view.thread
HistoryRemainsVisible ==
    branch = "render" /\ view.thread = NoOwner /\ BoundProject # NoOwner
    /\ view.mode = "history" => WorkAvailable
WorkAvailabilityKeepsFocus ==
    branch = "render" /\ view.wanted \in Available => Displayed = view.wanted
FallbackSelectsAvailable == branch = "render" => Displayed \in Available
HiddenFrameRetained == branch = "render" /\ TaskAvailable => MountTask
ProjectStatusOwned ==
    branch = "render" /\ StatusVisible => view.statusOwner = ExpectedProject(view.thread, view.sidebar)
AutoOpenOwnerMatches == focus.receipt.opened => focus.receipt.owned
AutoOpenUsesLatestSignal == focus.receipt.opened => focus.receipt.latest
AutoOpenRequiresPresentInstance == focus.receipt.opened => focus.receipt.present
ReplayDoesNotOpen == focus.receipt.opened => ~focus.receipt.replay

NeverStandaloneWork ==
    ~(branch = "render" /\ view.thread = "standalone" /\ WorkAvailable
      /\ view.wanted = "agents" /\ Displayed = "agents")
NeverBothAvailable == ~(branch = "render" /\ WorkAvailable /\ TaskAvailable)
NeverHistorySelected == ~(branch = "render" /\ view.thread = NoOwner /\ view.mode = "history" /\ Displayed = "work")
NeverSafeFallback == ~(branch = "render" /\ view.wanted = "task" /\ Displayed = "agents")
NeverHiddenIframe == ~(branch = "render" /\ Displayed = "work" /\ MountTask)
NeverFilteredForeignRow == ~(branch = "render" /\ MountTask /\ MountedRows # {}
    /\ (\E row \in view.rows : RowThread(row) # view.thread))
NeverAutoOpened == ~focus.receipt.opened
NeverRefusedStaleOwner == ~(focus.receipt.checked /\ ~focus.receipt.owned /\ focus.receipt.latest /\ focus.receipt.present)
NeverRefusedSuperseded == ~(focus.receipt.checked /\ focus.receipt.owned /\ ~focus.receipt.latest /\ focus.receipt.present)
NeverRefusedDeleted == ~(focus.receipt.checked /\ focus.receipt.owned /\ focus.receipt.latest /\ ~focus.receipt.present)
NeverReplayed == ~focus.replayed
=============================================================================
