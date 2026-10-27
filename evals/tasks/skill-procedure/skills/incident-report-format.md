---
name: incident-report-format
description: Use when writing an incident summary, outage report or postmortem file in this workspace.
---

## Preconditions

The raw notes or timeline for the incident are available.

## Steps

1. Write the report to `reports/INCIDENT-<YYYYMMDD>-<slug>.md`, where the date is the day the incident started (UTC) and the slug is a short lowercase hyphenated name.
2. The first line of the file is exactly `format: ir-3`.
3. Use these three sections, in this order: `## Impact`, `## Timeline`, `## Follow-ups`.
4. Each timeline entry is one bullet: `- HH:MM UTC — what happened`.
5. Each follow-up is a checkbox bullet with an owner: `- [ ] action (owner: name)`.

## Verification

Read the file back and confirm the first line, the three headings and the bullet formats.
