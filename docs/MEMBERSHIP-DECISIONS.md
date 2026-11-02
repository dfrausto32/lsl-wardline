# Membership decisions

Decisions taken while building specs/MEMBERSHIP-AND-MODES.md. Each says what was decided, why, and what it costs.

## 6.0 Canonical encoding (decided 2026-11-02)

**Canonical JSON**, written by Wardline's own small encoder, not by `encoding/json` on arbitrary values.

Rules (all of them, so another program can reproduce the bytes):
- Objects: keys sorted by their UTF-8 bytes, no spaces or newlines anywhere.
- Allowed values: object, array, string, integer, boolean. **No floats, no null.** A field that has no value is left out.
- Integers are written in decimal with no leading zeros or plus sign.
- Strings must be valid UTF-8. Only `"` and `\` and control characters below 0x20 are escaped (as `\"`, `\\`, `\n`, `\r`, `\t`, or `\u00XX`); everything else, including `<`, `>`, `&` and non-ASCII, is written as is.
- Arrays keep their order. Where order carries no meaning (an entry's parents), the writer sorts them and the validator rejects an unsorted list, so there is one spelling.

Why not CBOR: the standard library has no CBOR, and the log would need a dependency. Canonical JSON can be read by a person, by `jq`, and by the Edge project in any language. The costs: bytes are larger than CBOR, and binary values (keys, signatures) are base64 text.

The bytes that are signed and hashed are the canonical form of the entry **without** `id` and `sig`:
`group_id` (left out for the genesis entry), `author`, `parents`, `type`, `body`, `observed_at`.
`id` is the lowercase hex SHA-256 of those bytes; `sig` is the Ed25519 signature over those same bytes.
The group's id is the id of its genesis entry.

## Member ids (decided 2026-11-02)

The spec says a member id is the SHA-256 of its signing public key. Computers in a managed group do not have signing keys yet (agents are unchanged in Phase 6, and the rules forbid changing Phase 1 and 2 behaviour). So:
- A member **with** a signing key: id must equal the hex SHA-256 of that key. The validator checks it.
- A member **without** one (only allowed in a managed group, where the control plane authors every entry): its id is the device's or server's own id, and `signing_pub` is empty. When agents get signing keys (needed in Phase 7), they can be added with a `member_key_rotate`-style entry; nothing here prevents it.
- In a managed group, the control plane's own signing key is the authority. Its member id is the SHA-256 of that key, and it is the founder in the genesis entry.

## Entries that arrive before their parents (decided 2026-11-02)

The spec says an entry whose parents have not arrived is queued (section 4.3 rule 1), and the acceptance list says it is "rejected, with a reason" (section 14 A.4). Built: the entry is **queued with a reason** ("waiting for ..."), is never applied or passed on as valid, and is applied the moment its parents arrive. An entry whose parent was **rejected** is rejected too.

## Checking an entry's author (decided 2026-11-02)

An entry is valid only if its author was an active member **in the entry's causal past** (the entry's ancestors), has permission under the group's policy, and the signature checks against the key that member had in that past. The state used for each check is rebuilt from the ancestors only, so two nodes with the same entries always agree. The cost is work that grows with the square of the log's length; for the group sizes this is built for (hundreds of members) it takes milliseconds. Revisit before logs reach tens of thousands of entries.

## Open-mode joins in Phase 6 (decided 2026-11-02)

The policy evaluator already understands open groups, as the spec asks. Until Phase 7 builds invites, an open-mode `join` entry names its `inviter`, and is valid if both the author (the sponsor) and the inviter were active members in the entry's causal past. Invite tokens, expiry and `max_uses` are Phase 7.1.

## Labels and the Edge (decided 2026-11-02)

Wardline stores labels on members and returns them unchanged; it never reads them. They are set with `PUT /api/devices/{id}/labels` (admin only). Servers can have labels later. The Edge repo's `testdata/contract/` folder did not exist when Phase 6 was built, so the contract test uses sample files copied from the example in the Edge repo's `specs/WARDLINE-CONTRACT.md`. When the Edge repo has its own fixtures, copy them over `testdata/contract/` here.

## Edge event cursor (decided 2026-11-02)

The cursor is the number of the last event, as text. The last 1000 events are kept; a cursor older than that gets `410 Gone`. Changes are found by comparing the current member list with the last one stored (every 5 seconds), so a restart does not lose changes made while it was down.
