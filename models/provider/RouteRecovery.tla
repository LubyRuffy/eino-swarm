---------------------------- MODULE RouteRecovery ----------------------------
EXTENDS Naturals

(* One logical model call and confirmed route refusals only. Cap is the
   consecutive refusal budget. UnboundedRouteRecovery is the pre-cap loop:
   another refusal leaves the call waiting. ExhaustionStartsModelRetry treats
   the capped refusal as an ordinary model retry and clears the count.
   This is finite safety, not the shared cooldown clock or Go atomicity. *)
CONSTANTS
    UnboundedRouteRecovery,
    ExhaustionStartsModelRetry,
    Cap,
    MaxProbes

ModelRetryBudget == 2

VARIABLES status, refusals, probes, modelRetries

vars == <<status, refusals, probes, modelRetries>>

Init ==
    /\ status = "ready"
    /\ refusals = 0
    /\ probes = 0
    /\ modelRetries = 0

CanProbe == status \in {"ready", "waiting"} /\ probes < MaxProbes

Refuse ==
    /\ CanProbe
    /\ (UnboundedRouteRecovery \/ refusals < Cap)
    /\ probes' = probes + 1
    /\ IF ~UnboundedRouteRecovery /\ refusals + 1 >= Cap
       THEN IF ExhaustionStartsModelRetry /\ modelRetries < ModelRetryBudget
            THEN /\ status' = "ready"
                 /\ refusals' = 0
                 /\ modelRetries' = modelRetries + 1
            ELSE /\ status' = "failed"
                 /\ refusals' = refusals + 1
                 /\ UNCHANGED modelRetries
       ELSE /\ status' = "waiting"
            /\ refusals' = refusals + 1
            /\ UNCHANGED modelRetries

Succeed ==
    /\ status \in {"ready", "waiting"}
    /\ refusals < Cap
    /\ probes < MaxProbes
    /\ probes' = probes + 1
    /\ status' = "recovered"
    /\ refusals' = 0
    /\ UNCHANGED modelRetries

Stop ==
    /\ status \in {"ready", "waiting"}
    /\ status' = "cancelled"
    /\ UNCHANGED <<refusals, probes, modelRetries>>

OtherError ==
    /\ status \in {"ready", "waiting"}
    /\ probes < MaxProbes
    /\ probes' = probes + 1
    /\ status' = "other"
    /\ UNCHANGED <<refusals, modelRetries>>

Idle ==
    /\ status \in {"failed", "recovered", "cancelled", "other"}
    /\ UNCHANGED vars

Next == Refuse \/ Succeed \/ Stop \/ OtherError \/ Idle
Spec == Init /\ [][Next]_vars

TypeOK ==
    /\ status \in {"ready", "waiting", "failed", "recovered", "cancelled", "other"}
    /\ refusals \in 0..MaxProbes
    /\ probes \in 0..MaxProbes
    /\ modelRetries \in 0..ModelRetryBudget

RouteRefusalStops == status # "waiting" \/ refusals < Cap
NoRetryAfterExhaustion == modelRetries = 0

NeverFailed == status # "failed"
NeverRecovered == status # "recovered"
NeverCancelled == status # "cancelled"
NeverOtherError == status # "other"
=============================================================================
