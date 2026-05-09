---
name: deployment_storage_compatibility
type: project
created: 2026-05-09
updated: 2026-05-09
source: thread
source_id: 5dc26489c8c9f53831549a45c0483af1
confidence: high
title: Deployment Storage Compatibility
---

Memory storage changes must remain backward-compatible for Docker/VPS, local server, and desktop deployments. Docker/VPS should persist memory under `/data/memory` when `/data` is mounted, local server should not unexpectedly move an existing `./openvibely.db`, and desktop should continue using OS app-data defaults unless the user opts into a shared app-data root.

When debugging local runtime state, verify the active process, port, and database path before assuming `.openvibely/openvibely.db`. In the 2026-05-09 local server incident, the active latest server on port `3001` was using repo-root `openvibely.db`, which contained the memory extraction runs.
