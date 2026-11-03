# Contract samples

`members.json` is the example response in the Edge project's `specs/WARDLINE-CONTRACT.md` (section "GET /api/v1/edge/members"),
copied here, as the spec asks, so Wardline's tests can check the shape of what it sends. The Edge repo's own
`testdata/contract/` folder did not exist when this was made. When it does, copy its files over these.
Do not import code across the two repos.

Wardline adds one field to each member that the contract's example does not have: `reaches_all` (true or false). It is true for a machine holding the
reach-all permission (it may reach every other machine through the server and be reached by every other machine, with the network-wide setting off).
It is an additional field, `roles` is unchanged, and the Edge may ignore it. Member changes to it arrive as `member_updated` events.
