# Provenance

## How the code was written

Written for SigmaM2M, a oneM2M platform, in Go. No code was ported or
copied from another implementation, and no source file of another
project was translated.

Xiaomi documents neither the miIO protocol nor the Mi account API. The
facts this module uses come from public descriptions. The main one for
the local protocol, python-miio, is GPL-3.0, so the rule for this code
was facts only: the packet layout, the key derivation, method names,
request shapes and error codes were taken; code structure, identifiers,
comments and control flow were not. The Go types, the client's retries
and request IDs, the error handling, the account client's structure and
the simulators are our own design.

History:

- The miIO protocol (proto.go) came with SigmaM2M's first commit
  (2026-09-27), built from described facts.
- Random request IDs with a jump after an unanswered request were added
  on 2026-10-05.
- MIoT, the specification fetch and the Mi account client (cloud.go)
  were added on 2026-10-05, from miot-spec.org's published format and
  Xiaomi-cloud-tokens-extractor's token_extractor.py (MIT).
- miot.go and spec.go were taken out of SigmaM2M's adapter for this
  module; GetProperties, SetProperties, DoAction, CheckSet, CheckAction
  and PropError were written for it.
- 2026-10-11: the simulators, their command and the tests that drive
  them moved to the miio-sim module (with `git mv`), so the library does
  not depend on its simulators. The interop module was added and run
  against python-miio's own simulators. One difference was found and
  fixed: an answer without "result" is now returned whole, as python-miio
  takes it (`TestAnswerWithoutResult`).

## Sources

| Area (files) | Followed | Read for facts only (no code taken) |
|---|---|---|
| miIO packets and client (proto.go) | none published | python-miio (GPL-3.0): the 32-byte header (magic 0x2131, length, unknown, device ID, stamp, MD5 checksum with the token in its place), AES-128-CBC with key MD5(token) and IV MD5(key, token), the hello, the JSON-RPC body |
| MIoT requests (miot.go) | none published | python-miio (GPL-3.0): get_properties, set_properties and action with did, siid, piid or aiid, and the per-property codes; miot-spec.org: the specification JSON (services, properties with format, access, unit, value range and list, actions) |
| Specification fetch (spec.go) | miot-spec.org's public service | /instances?status=released and /instance?type= |
| Mi account (cloud.go) | none published | Xiaomi-cloud-tokens-extractor (MIT) token_extractor.py: serviceLogin, serviceLoginAuth2 with the upper-case MD5 of the password, the location step and serviceToken cookie, the per-call nonce and SHA-256 signed nonce, RC4 with 1024 bytes dropped, the SHA-1 request signature, the API paths for homes and device lists, the regions |
| Interop peer (interop/peer) | our own Dockerfile and made-up specification | python-miio's simulators, run at test time |

No specification or vendor text is reproduced.

## Notes for reviewers

The places closest to a public source, each a fact an implementation
must match to interoperate:

- `Hello`, `Keys`, `Packet` and `Open` in proto.go: the packet layout and
  key derivation as python-miio describes them.
- `Cloud.init`, `Cloud.login` and `Cloud.call` in cloud.go, with
  `SignedNonce`, `EncSignature`, `RC4Encrypt`: the order of the sign-in
  steps, the parameter names and the user agent's shape follow
  token_extractor.py (MIT).

The project owner decided on 2026-10-06 that no line-by-line comparison
with python-miio or the extractor is needed, the facts-only rule and
these notes being the record.

## Test vectors and where they come from

| Test | Source |
|---|---|
| proto_test.go TestMiIOPackets | packets built and opened by this module; checksum and token checks against a changed byte and another token |
| proto_test.go TestAnswerWithoutResult | a fake device in the test, answering as python-miio's simulator does |
| proto_test.go TestMiAccountCrypto | made-up ssecurity and nonce; RC4 round trip and the signature's length |
| interop/ | python-miio's simulators at test time |

All tokens, device IDs, accounts and addresses are made-up demo values.
