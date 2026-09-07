# ChangeLog

# Upcoming WeKan ® release

- [Preserve user board listing authorization and revoked-access reports](https://github.com/wekan/wekango/commit/ebaacc4).
  Thanks to xet7.
  List active, unarchived boards for the caller or an administrator, with the
  existing helper-title exclusions. Withhold revoked memberships and fold the
  source-compatible medium-severity report without blocking accounts or exposing
  board details. Database, race and actual-source policy tests pass, together
  with Chromium and Firefox authorization checks.

- [Make login query and password verification security boundaries explicit](https://github.com/wekan/wekango/commit/b28f0b6).
  Thanks to xet7.
  Review the saved CodeQL injection and weak-password-hash findings. Login uses
  constant typed BSON selectors and the complete existing Meteor SHA-256 prehash
  plus salted bcrypt verification; neither operator objects nor bare hashes can
  authenticate. Narrow annotations document these two false positives without
  disabling the rules elsewhere. Adversarial JSON/form inputs, literal query
  metacharacters, changed long-password suffixes and invalid verifiers have
  regression tests; Chromium and Firefox reject login operator payloads.
  No remote alert is dismissed; GitHub CodeQL must rerun after publication.

- [Preserve API usage reports in existing event summaries](https://github.com/wekan/wekango/commit/bd49757).
  Thanks to xet7.
  Batch API requests by account and route pattern and write the existing
  eventlog summary format, preserving IDs, first-seen values, actor hashes and
  bounded overflow. Resolve public client addresses and display-only geography;
  flush pending reports before closing SQLite. Actual-source comparisons,
  concurrent database/race tests, native shutdown/export and Chromium/Firefox
  checks pass. The source writer's per-flush counter discrepancy is preserved
  and documented explicitly; exact call totals, security/account-blocking side
  effects, report UI and full application parity remain roadmap work.

- [Fix incomplete migration fixture JavaScript syntax](https://github.com/wekan/wekango/commit/db90485).
  Thanks to xet7.
  The checklist-minicard snapshot was an incomplete object fragment, causing
  CodeQL's JavaScript parser to stop at its async method. Export a complete
  module with the original marker constants and remove the dangling next-object
  opener. CI checks every repository JavaScript file and verifies once-ever
  marker behavior, legacy false-value removal and rejection of the malformed
  fragment. All four regression tests and workflow validation pass locally;
  GitHub's CodeQL scan must rerun after the maintainer publishes the fix.

- [Preserve forwarded client addresses across the embedded proxy](https://github.com/wekan/wekango/commit/edf7190).
  Thanks to xet7.
  Honor HTTP_FORWARDED_COUNT at Caddy's public ingress, preserving Meteor's
  decimal-prefix parsing, right-counted nonempty forwarded chain and original
  socket fallback. Pass the computed address through the private proxy without
  allowing forged or hop-by-hop headers to replace it. Ninety actual-source
  comparisons, 64 Caddy network cases and native executable throttle tests pass,
  along with the full Go suite, affected race suites and Chromium/Firefox login,
  board-resource authorization and startup-dashboard checks. Other client-address
  consumers remain pending with their corresponding DDP/API surfaces.

- [Embed database tools and extend board resource reads](https://github.com/wekan/wekango/commit/b6bc8ee).
  Thanks to xet7.
  All eight MongoDB tools run inside the same executable, retaining upstream
  arguments and formats. Default operations open configured external storage
  or existing FerretDB SQLite paths before application writers start. Add ten
  board/list/swimlane/card read routes with source-specific authorization,
  projections, archive filters and response shapes. Update Azure authentication
  dependencies and use maintained tcell/YAML backends through owned compatibility
  facades. Preserve Apache source notices and verify the complete license graph.
  BSON/archive/Extended JSON/GridFS round trips, terminal tests, Go/race suites,
  Chromium/Firefox authorization and executable restart tests pass. All 25
  binaries cross-compile and verify checksums; dependency scans report no
  imported-package vulnerabilities. FerretDB monitoring command gaps, remaining
  security-event middleware and full product parity remain documented in ROADMAP.

- [Port the twelve current schema upgrades and live progress dashboard](https://github.com/wekan/wekango/commit/416962d).
  Thanks to xet7.
  Run the current WeKan schema pipeline in the background, preserving its
  version gate, once-ever checklist marker, step order, stored choices and
  historical filesystem recovery. Keep unresolved work eligible for retry;
  honor skip/force flags and join the upgrade before closing SQLite. Serve
  escaped HTML and JSON progress at the existing schema-upgrade-status URL.
  Real FerretDB fixtures compare JavaScript and Go documents, counters and
  marker history across initial, gated, forced and new-version runs. Go/race,
  Chromium/Firefox and executable restart checks pass. All 25 platform binaries
  cross-compile with verified checksums. ROADMAP.md records that the older
  Meteor migration chain and full product parity still remain unfinished.

- [Build the Go drop-in compatibility foundation](https://github.com/wekan/wekango/commit/8f320c0).
  Thanks to xet7.
  Caddy, the application server and FerretDB SQLite run in one executable.
  Preserve existing bundle paths and test Meteor local sessions, authorized
  board reads and the exact one-time checklist migration. Copy WeKan docs,
  record remaining parity work in ROADMAP.md, and add platform build/release
  workflows with dependency license, source-notice and vulnerability audits.
  All 25 binaries cross-compile and verify checksums; Go/race and real-browser
  checks pass on Linux ARM64. Full Meteor UI, migrations and attachments remain
  unfinished, so this is a compatibility preview, not a production replacement.
- [Remove the obsolete driver and HTML demo](https://github.com/wekan/wekango/commit/7aad9a3).
  Thanks to xet7. The original demo remains available in git history; the older
  prototype entries below describe work superseded by the new implementation.

Current implementation work and earlier prototype history:

- [Added detecting is database MongoDB 3 or MongoDB 6. Added webserver](https://github.com/wekan/wekango/commit/f61596deed1a89fc11fc2cd7b52c7e73977eba9e).
  Thanks to xet7.
- [Added ChangeLog](https://github.com/wekan/wekango/commit/29b6197844bbf93b7aa6fd7052f3057029e801e1).
  Thanks to xet7.
- [Added translations](https://github.com/wekan/wekango/commit/ccd1dfe81efeec24fed92f77555a054df5d42027).
  Thanks to xet7.
- [Updated dependencies](https://github.com/wekan/wekango/pull/1).
  Thanks to dependabot.
- [Added CODE_OF_CONDUCT.md, CONTRIBUTING.md, GOVERNANCE.md, SECURITY.md](https://github.com/wekan/wekango/commit/e1e5e9e99d42f9549c3ef6941162c561d0ae7242).
  Thanks to xet7.
- [Updated dependencies. Added rebuild-wekan.sh](https://github.com/wekan/wekango/commit/0075e4a4ca85d2ea15179e71de9d9fabdf657063).
  Thanks to xet7.
- [Crosscompiling to many platforms](https://github.com/wekan/wekango/commit/f81dd608f954c07ea8eb32714aa5f2e98b4feaf8).
  Thanks to xet7.
- [More build target platforms](https://github.com/wekan/wekango/commit/5201fdce629749632ed7337d03e1db59c6cd03a6).
  Thanks to xet7.
- [Updated ports](https://github.com/wekan/wekango/commit/b957e2dc3f43e95e0b22f28938f9a0cd0af67b34).
  Thanks to xet7.
- [Add page text](https://github.com/wekan/wekango/commit/9424167154bd558d2b6422e6193d361099416877).
  Thanks to xet7.
- [Added test](https://github.com/wekan/wekango/commit/5fc80d39f4f2666eafa0b1b045a2b1cee707a1e8).
  Thanks to xet7.
- [To rebuild-wekan.sh, added Raspberry Pi OS 32bit at RasPi3](https://github.com/wekan/wekango/commit/975169680151e721683482a5d2298487b4ff8148).
  Thanks to xet7.
- [Updated test](https://github.com/wekan/wekango/commit/a8337d7da5710594fe2b7749c9e1c6b564d1c641).
  Thanks to xet7.
- [Updated test readme](https://github.com/wekan/wekango/commit/3b679db7702514c3cb53f7ba45757763441d6feb).
  Thanks to xet7.
- [Updated test readme](https://github.com/wekan/wekango/commit/83c5d99a177b2cbfb2db341156f9b9cf8cc8d3df).
  Thanks to xet7.

Thanks to above GitHub users for their contributions and translators for their translations.
