# Pipeline Smoke Test

This file is a deliberate smoke test to validate the autonomous pipeline
(OpenSpec propose → `sync-openspec-change.sh` → GitHub issue → hourly
`loop-issue-processing` systemd timer → implementation → tests → `pm-qa`
review → commit → Done) end-to-end against a real issue, before trusting it
with real work.

It was created by the autonomous `loop-issue-processing` loop on 2026-07-11.

This file (and the OpenSpec change `pipeline-smoke-test`) should be
deleted/archived once the pipeline is validated — this is scaffolding, not a
permanent doc.
