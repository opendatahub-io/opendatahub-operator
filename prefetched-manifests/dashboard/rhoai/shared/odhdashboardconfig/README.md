# OdhDashboardConfig

A resource that declares the **initial** configuration of a RHOAI Dashboard installation.

This file is a general configuration file for RHOAI installations only. ODH generates one off the state of the code.

Some features (perhaps dwindling) in use:
* Feature flags (properties under `dashboardConfig`) -- [learn more here](https://github.com/opendatahub-io/architecture-decision-records/blob/main/documentation/components/dashboard/configuringDashboard.md)
* Jupyter tile (properties under `notebookController`)
* and others that are likely short-lived

## Changes are semi-permanent

Anything added to this CR will be "semi-permanent" -- as in we will not be able to upgrade our way to a new state.

It is the *initial* state on install, and then it becomes **Unmanaged** and the user may adjust it to their desires.

## Things to Avoid

* **Never** state a feature flag as disabled (aka setting a `disable<Flag Name>` to `true`)
    * Why? Because we need the operator to undo this action -- since we do not manage this file it _cannot ever_ be updated by the Dashboard component
    * Okay "never" is not exactly true -- but it's unlikely; there may be an ODH feature not present in RHOAI
* There is almost never a reason to state duplicated feature flags in this file that mirror what is in the blankDashboardCR constant in the backend
