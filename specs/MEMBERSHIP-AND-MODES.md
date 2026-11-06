# Membership log and group modes (Phases 6 to 9)

Status: PROPOSED 2026-10-29. Do not start until Dylan says go. Phases 1 and 2 are built; this spec adds to them without changing what they do.

## 1. Why this exists
Wardline today has one kind of group: **managed**. A control plane decides who is in, servers hold the hub, devices connect to servers. Dylan needs a second kind for airgapped and similar networks where there is no always-on authority and machines join at different times.

Decided by Dylan (2026-10-29):
- Repos are **separate**: Wardline (this repo) and a separate **Edge** repo (browser-facing HTTPS). Wardline must never import Edge code. Edge uses Wardline only through the read-only API in section 9.
- **Each group has its own control plane.** There is no global one. Federation between groups is out of scope.
- **Public / always-on groups: one central authority**, built to be always on (section 8).
- **Airgapped groups: anyone can invite anyone** ("open" mode). **Any member may remove any member.**
- Machines in an airgap are assumed not to be remotely compromised. The design still has to survive lost, decommissioned, or mistakenly invited machines, which is why removal and an invite record exist.

## 2. One design, three settings
Build a single **membership log**: a signed, append-only record of who is in the group. A group's **admission policy** decides who may add a member. Each node derives its WireGuard peer list from the log.

| Mode (policy) | Who can add a member | Who can remove a member | Used for |
|---|---|---|---|
| `authority` (UI: **Managed group**) | Only the authority key (the control plane) | The authority | Public, always-on groups. This is today's behaviour. |
| `open` (UI: **Open group**) | Any current member | **Any current member** (Dylan's decision) | Airgapped and similar |
| `admins_k_of_n` | k of n admin keys | Any admin | Designed for, not built until asked |

Admin role: admins (the founder at first) can change roles and policy-level settings. Any member can add and remove members in `open` mode. The mode is chosen when the group is created and does not change later.

Spec note for Dylan (no action unless you disagree): in `open` mode a single member can remove everyone, including the founder. That is what was decided. A "protected members" setting could be added later if wanted; do not build it now.

## 3. Vocabulary and terms
Internal terms may be technical. The UI follows `specs/UX.md`.

| Say in the UI | Internal |
|---|---|
| Managed group / Open group | policy `authority` / `open` |
| Invite someone | create an invite |
| Remove from group | `remove_member` entry |
| Who added whom | invite tree |
| Group activity | the log (plus the existing Activity feed) |

Mapping to what exists: a **server** is a node with the gateway role; a **device** is a node without it. Do not rename existing tables. Add new ones (section 11).

## 4. The membership log

### 4.1 Entries
Each entry has: `id` (SHA-256 of the canonical bytes), `group_id`, `author` (member id), `parents` (hashes of the log heads the author had seen), `type`, `body`, `observed_at` (author's wall clock, informational only, never used to decide validity), `sig` (Ed25519 over the canonical bytes).

Types:
- `genesis`: creates the group, fixes the policy and the founder's keys.
- `join`: admits a new member. Carries the invite token, the joiner's keys, the joiner's proof of possession.
- `remove_member`: `subject`, `cascade` (bool).
- `set_role`: promote or demote admin.
- `set_node_roles`: `relay`, `gateway`, `voter` flags (used from Phase 8).
- `member_key_rotate`: a member replaces its WireGuard key (signed by its member key).
- `revoke_invite`: cancels an unused invite.

### 4.2 Identities and keys
- Every member has two separate key pairs. A **member signing key** (Ed25519, used for log entries) and a **WireGuard key** (X25519, used for tunnels). WireGuard keys cannot sign (see the Phase 2 notes in `BLOCKED.md`), which is why the two exist.
- Member id = SHA-256 of the member's public signing key.
- Private keys never leave the node and are stored the same way the agent key already is (OS-protected storage; 0600 file for a dedicated service user on Linux). The existing no-private-keys scan is extended to cover the log, invites, and bundles.
- Hashing is SHA-256. Use the standard library (`crypto/ed25519`, `crypto/sha256`).
- Canonical encoding: undecided. Recommend deterministic CBOR (RFC 8949 core deterministic encoding) or canonical JSON. Decide in task 6.1 and record the choice and the reason in `docs/MEMBERSHIP-DECISIONS.md`.

### 4.3 Validity and merging (the rules most likely to be got wrong)
Every node must reach the **same group state from the same set of entries, in any order of arrival.** Rules:
1. An entry is valid if its signature checks, its parents exist (or are queued until they arrive), and the author was an active member **in the entry's causal past** with permission under the policy.
2. State is computed by ordering entries topologically, breaking ties by entry id, and applying them one by one.
3. **Remove wins.** A removal is permanent for that member id. Re-admitting a machine means a fresh join with new keys.
4. `cascade: true` removes the subject and everyone they transitively invited, evaluated over the **final merged** invite tree (so people they invited concurrently are included).
5. Entries authored by a member who is later removed stay valid (they were valid when written); the member simply stops being active from the removal onward.
6. Two concurrent role or policy changes: the one with the greater (causal depth, entry id) wins.
7. Entries that fail validation are stored as rejected, never applied, and never propagated as valid.
8. Wall clocks are never used to order entries. Invite expiry uses the time observed by the node validating it, with a documented tolerance; see section 10.

## 5. Invites and joining

### 5.1 Invite token
Contents: `group_id`, `genesis_hash`, `inviter` member id, `created_at`, optional `expires_at`, optional `max_uses`, random `nonce`, the sponsor's WireGuard public key and a list of endpoint hints, and `sig` by the inviter's signing key. Shown as a link, a QR code, and a file (file is required for airgapped use).

### 5.2 Join flow (open mode)
1. The joiner generates both key pairs locally.
2. The joiner contacts a reachable member (the sponsor named in the token, or any member found through hints) and presents the token plus its public keys plus a signature proving it holds the signing key.
3. The sponsor verifies the token against its copy of the log: the inviter is an active member, the token is not expired or revoked, and `max_uses` is not exceeded.
4. The sponsor appends a `join` entry. The inviter does not have to be online: the token is the evidence.
5. The joiner receives the current log and the member list, then configures its WireGuard peers.

Join channel: design in task 7.1 and record in `docs/MEMBERSHIP-DECISIONS.md`. Recommended: the joiner dials the sponsor over WireGuard using the sponsor key in the token, and the sponsor admits it into a restricted "joining" scope until its entry has propagated. WireGuard needs no certificate authority, which is what makes this work in an airgap.

### 5.3 Limits in open mode
`max_uses` is best effort when the group is partitioned (two sponsors may each accept the last use). Document this in the UI as "may be used slightly more than the limit if the network is split". In `authority` mode it is exact.

### 5.4 Managed mode
The authority is the only inviter. The existing enrollment flow keeps working unchanged. The log records membership changes for consistency and export. Do not rewrite the enrollment flow.

## 6. Addressing
- **Managed mode:** unchanged (existing addressing and quarantine rules).
- **Open mode:** no central address allocator exists, so addresses are **derived**. Each group gets an IPv6 unique-local /48 derived from the group id. A member's address is the prefix plus 64 bits taken from the hash of its WireGuard public key. Allowed addresses in WireGuard are /128. On the (negligible) chance of a collision, the later entry in the deterministic order is rejected as invalid.
- IPv4 inside the tunnel for open mode is not built now. If an old application needs it later, use per-member delegated blocks and write a spec first.
- The underlying transport can be IPv4; only addresses inside the tunnel are IPv6.

## 7. Sync
- **Anti-entropy gossip** over the overlay: nodes periodically pick a few peers, exchange log heads and a compact summary, and fetch missing entries by hash. Entries verify themselves, so gossip needs no trusted transport.
- **Offline bundles:** `wardline group export --since <head>` writes a file; `wardline group import <file>` applies it. Bundles are untrusted input: every entry is validated like any other. Required for airgaps that move data on removable media.
- **Revocation** is just a `remove_member` entry, so it spreads like any entry. When a node learns of a removal it removes the peer from its WireGuard config immediately (existing idempotent apply). A machine that was offline learns of removals when it next syncs.
- **Log growth:** compaction is undecided. Do not build it. Note the size growth in docs and revisit if logs get large.

## 8. Replicated control plane (Phase 8)
For managed groups that must be always on, and for open groups, the control data must survive any one machine failing.
- **Managed, always on:** at least two control instances behind failover, with replicated state. The signing key sits behind a `Signer` interface with a file implementation now and room for a hardware module or cloud key service later. Existing enrolled nodes keep working when the authority is briefly down; only new joins and renewals wait.
- **Voters and followers:** a small odd number of voters (3 or 5) agree on writes; all other nodes are followers that cache the member list and keep working offline. Nodes can be promoted and demoted automatically. Never make every node a voter.
- **Replication mechanism:** undecided. Options: Raft with the existing SQLite state (for example hashicorp/raft plus a snapshot), or rqlite. Write the options, trade-offs and the choice in `docs/MEMBERSHIP-DECISIONS.md` at the start of Phase 8. Keep the store behind an interface so this does not leak into the rest of the code.
- Open mode does not need voters: every node holds the full log and gossip is the replication.

## 9. Interface for the Edge (separate repo)
Wardline publishes a read-only API. The Edge repo consumes it. Wardline never calls the Edge.

- `GET /api/v1/edge/members` returns a list of: `id`, `name`, `hostname_label` (lowercase letters, digits, hyphen; unique in the group), `overlay_address`, `roles`, `status`, `last_seen_at`, `labels`.
- `GET /api/v1/edge/events` is a server-sent event stream: `member_added`, `member_removed`, `member_status_changed`, each with the member record.
- Auth: a read-only token with scope `edge:read`, created from the admin app (Advanced). It can read only these two endpoints.
- This is built in Phase 6 (task 6.8), because it is useful in managed mode already.

## 10. Airgap constraints (apply to open mode and to everything in this spec)
- No call to an outside network, ever. No DNS lookups of public names, no update checks, no telemetry.
- Do not depend on a trustworthy clock. Use causal order, not timestamps. Expiry checks use the validating node's clock with a tolerance (default 24 hours, configurable) and documented consequences.
- Everything needed to join (the token, the group's genesis, the software) can travel as files.
- Time sync across the group is Dylan's to plan; note it in install docs.

## 11. Data model additions (SQLite)
- **groups:** id, name, mode (`authority` | `open` | `admins_k_of_n`), genesis_hash, created_at. One row per database in practice.
- **membership_entries:** id (hash), group_id, author, type, body (canonical bytes), sig, depth, received_at, status (`applied` | `queued` | `rejected`), reject_reason.
- **members (projection):** member_id, group_id, name, hostname_label, signing_pubkey, wireguard_pubkey, overlay_address, roles, status (`active` | `removed`), added_by, added_entry, removed_entry.
- **invites:** id (token hash), group_id, inviter, expires_at, max_uses, uses_seen, state (`active` | `revoked` | `used_up`).
- **api_tokens:** extend with a `scope` column for `edge:read`.
Existing tables (`devices`, `servers`, `people`, and the rest) stay. In managed mode they are the primary data and the log is the record. In open mode `members` is the primary data and the per-device tables are filled from it where the UI needs them.

## 12. UX additions (still plain language; follow `specs/UX.md`)
- **Create a group:** one plain question first: "Will this network always be connected to the internet, with one person in charge? (Managed group) Or will it run on its own, where anyone in it can add people? (Open group)." Explain each in one sentence.
- **Invite someone:** shows a QR code, a link, and a file to save. Say plainly that an invite lets that person in, and who invited them is recorded.
- **Who added whom:** a simple tree on a group page.
- **Remove from group:** confirm dialog with a checkbox "Also remove everyone they added". In open mode say plainly: "Anyone in this group can remove anyone."
- **Open group warning:** shown once at creation and visible on the group page.
- Roles and technical entries live under Advanced. The banned-word lint continues to apply to primary strings.

## 13. Phases and tasks

### Phase 6: membership log, managed mode, Edge API (no behaviour change for existing groups)
| ID | Task | Test file |
|---|---|---|
| 6.0 | Decision record: canonical encoding, entry format, `docs/MEMBERSHIP-DECISIONS.md` | none |
| 6.1 | `internal/membership`: entry types, canonical encoding, hashing, signing and verification | `internal/membership/entry_test.go` |
| 6.2 | Policy evaluator for `authority` and `open` (and the `admins_k_of_n` interface stubbed) | `internal/membership/policy_test.go` |
| 6.3 | Merge and convergence: same entries in any order give the same state; remove wins; cascade; concurrent changes. Seeded randomized tests using the standard library | `internal/membership/merge_test.go` |
| 6.4 | Projection: log to members and peer set. Managed mode records its membership changes in the log. **All Phase 1 and 2 tests pass unchanged.** | `internal/membership/projection_test.go`, existing suites |
| 6.5 | `Signer` interface with a file implementation | `internal/membership/signer_test.go` |
| 6.6 | UI: group mode chosen at creation (Managed is the only enabled choice until Phase 7), plain-language strings | `web/tests/group-mode.spec.ts` |
| 6.7 | Extend the no-private-keys scan to the log, invites and bundles | `internal/api/noprivatekeys_test.go` |
| 6.8 | Edge API (section 9) and the `edge:read` token | `internal/api/edge_test.go` |
| 6.9 | Reach-all permission per machine (BUSINESS-RULES): firewall rules for it on every backend, a switch on the machine's page, `reaches_all` in the Edge API, Activity. **All earlier tests pass unchanged.** | `internal/wg/reachall_test.go`, `internal/gateway/reachall_test.go`, `internal/api/reachall_test.go`, lab steps in `e2e/agent_lab_test.go`, `web/tests/reach-all.spec.ts` |
Phase 6 is done when the section A items below pass. Then stop and wait for Dylan.

### Phase 7: open mode (airgapped)
| ID | Task | Test file |
|---|---|---|
| 7.0 | Decision record: join channel | none |
| 7.1 | Invite tokens and the join flow (sections 5.1 to 5.3) | `internal/membership/invite_test.go`, `internal/gateway/join_test.go` |
| 7.2 | Derived IPv6 addressing (section 6) | `internal/membership/addressing_test.go` |
| 7.3 | Gossip anti-entropy sync and offline bundle export and import | `internal/membership/sync_test.go`, `cmd/wardline/bundle_test.go` |
| 7.4 | Removal by any member, cascade, immediate peer removal on every node | `internal/membership/remove_test.go` |
| 7.5 | UI: create an open group, invite, who-added-whom tree, remove | `web/tests/open-group.spec.ts` |
| 7.6 | Lab end-to-end: at least five nodes, staggered joins, a network partition and rejoin, convergence, removal propagating, a bundle moved by hand | `e2e/open_group_test.go` via `scripts/e2e.ps1` |

### Phase 8: replicated control plane
Task table written when the phase starts, after the decision record in section 8. Includes failover tests in the lab (stop a control instance, enrolled nodes keep working, new joins resume when quorum returns).

### Phase 9: mesh and NAT traversal
Separate spec written at the start of the phase. Known requirements: endpoint discovery using members with public addresses, UDP hole punching, relay fallback through members with the `relay` role, path selection, key rotation and roaming. **Non-negotiable 10 in `CLAUDE.md` (hub-and-spoke only) is lifted for this phase only, and only after Dylan confirms at the start of Phase 9.**

## 14. Acceptance

### A. Phase 6
1. All Phase 1 and Phase 2 acceptance items still pass with no changes to those tests.
2. Convergence: for randomized sets of entries delivered in many different orders, all nodes reach identical state.
3. Remove-wins, cascade, and concurrent-change rules each have a passing test.
4. A tampered entry, a bad signature, an unknown parent, or an author who was not a member is rejected, with a reason.
5. The Edge endpoints return the member list and stream events; a token without `edge:read` is refused; the token cannot reach any other endpoint.
6. No private key appears in the database, logs, API responses, bundles, or invites.

### B. Phase 7
1. A node joins through a sponsor while the inviter is offline.
2. In the lab, split five nodes into two partitions, add members on each side, reconnect, and all nodes converge on the same member list and peer set.
3. Any member removes any member; the removed node loses access on every node within the gossip interval; an offline node removes it when it next syncs.
4. A group state moved by an export file on a "removable media" volume imports on another node with no network between them.
5. No outside network call is made by any open-mode code path (test with network egress blocked in the lab).
6. Plain-language check: a non-technical person creates an open group and invites a second device without help (manual check M7).

### C. Later phases
Written when each phase starts.

## 15. Not in scope
Federation between groups, a global control plane, trust bundles across groups, log compaction, IPv4 in open mode, an iOS app (Phase 3 is deferred indefinitely), any code for the Edge. Do not build these; if a task seems to need one, stop and ask.

## 16. Addendum (2026-10-29): Edge contract details
The Edge repo (`01 Projects/Edge/app`, working name `lsl-edge`) holds the detailed contract in its `specs/WARDLINE-CONTRACT.md`. Task 6.8 must implement exactly that, which adds to section 9:
- Responses carry `"version": "v1"`, `group_id`, `generated_at`, and an opaque `cursor`.
- `status` values are `connected`, `not_seen_recently`, `waiting`, `removed`.
- Events are `member_added`, `member_removed`, `member_status_changed`, `member_updated`. Each event has an `id` (the cursor). The stream honours `Last-Event-ID` and answers `410 Gone` when the cursor is too old. Heartbeat comment every 25 seconds.
- Labels the Edge reads: `edge.upstream_port`, `edge.scheme`, `edge.enabled`. Wardline stores and returns labels; it does not interpret them.
- Add a contract test in Wardline that checks response shape against a copy of the sample fixtures from the Edge repo's `testdata/contract/` (copy the files in; do not import across repos).
