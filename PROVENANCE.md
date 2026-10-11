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
- 2026-10-11, v0.2.0: model.go (specification mapping and value checks),
  families.go (vacuum, light, plug and purifier helpers), gateway.go,
  ota.go, push.go, cloudverify.go and cloudrpc.go were written for this
  module, each from facts read in the sources below, with our own types,
  names and control flow. python-miio's source at commit 9a00e08 was
  read for behaviour only (device.py for the firmware update request and
  states; the lumi gateway integration for the sub-device methods and
  the device_list layout; the push_server package for the scene layout,
  the target token and the answer to a push; the roborock, chuangmi,
  yeelight, philips and zhimi integrations for legacy method names). The
  identity-check steps were read from token_extractor.py (MIT) on its
  master branch. The interop runs moved to python:3.12.10-slim and were
  repeated; python-miio's MIoT simulator and its own packet code were
  added as peers for the specification mapping and the push server.
  Two observations, not differences: python-miio's push server hands
  its handler the action with the listener model's prefix still on
  ("chuangmi.plug.v3.open"), so `PushServer` strips that prefix when
  present and accepts it absent; python-miio's own push server builds
  its answer header with an integer device ID where its packet layout
  takes four bytes, so our test peer passes bytes.

- 2026-10-12, v0.3.0: specmirror.go, mdns.go, device.go, vacuum.go and
  cmd/miiospec were written for this module, and spec.go was extended,
  each from facts read in the sources below, with our own types, names
  and control flow. Read for facts only: python-miio's source at commit
  9a00e08 (GPL-3.0; the roborock vacuum integration for the legacy
  method names, parameters and answer fields of consumables, clean
  summary and records, do-not-disturb, timers, time zone, sound volume,
  custom mode and carpet mode; discovery.py for the mDNS service type
  and instance name; device.py for miIO.config_router and the MIoT
  probe; miot_cloud.py and micloud's miotspec.py for how a client walks
  the index); hass-xiaomi-miot v1.1.5 (Apache-2.0;
  core/miot_spec.py for the full index with status=all, the
  multiLanguage translations path, its answer's "data" member and the
  shape of its keys, and the second host spec.miot-spec.com). No
  per-model data was taken: python-miio's fan speed preset tables are
  left out on purpose. The interop runs found one observation, not a
  difference: python-miio's own Roborock simulator file nests its second
  timer inside the first record.

## Sources

| Area (files) | Followed | Read for facts only (no code taken) |
|---|---|---|
| miIO packets and client (proto.go) | none published | python-miio (GPL-3.0): the 32-byte header (magic 0x2131, length, unknown, device ID, stamp, MD5 checksum with the token in its place), AES-128-CBC with key MD5(token) and IV MD5(key, token), the hello, the JSON-RPC body |
| MIoT requests (miot.go) | none published | python-miio (GPL-3.0): get_properties, set_properties and action with did, siid, piid or aiid, and the per-property codes; miot-spec.org: the specification JSON (services, properties with format, access, unit, value range and list, actions) |
| Specification fetch (spec.go) | miot-spec.org's public service | /instances?status=released and /instance?type= |
| Mi account (cloud.go) | none published | Xiaomi-cloud-tokens-extractor (MIT) token_extractor.py: serviceLogin, serviceLoginAuth2 with the upper-case MD5 of the password, the location step and serviceToken cookie, the per-call nonce and SHA-256 signed nonce, RC4 with 1024 bytes dropped, the SHA-1 request signature, the API paths for homes and device lists, the regions |
| Specification mapping (model.go) | miot-spec.org's specification format | the names are the type URNs' name parts; the "service.property" naming, short names and the value checks are our own design |
| Family helpers (families.go) | none published | python-miio (GPL-3.0): Roborock app_start, app_stop, app_pause, app_charge, get_status and its state and battery fields; Yeelight set_power, set_bright, set_ct_abx, set_rgb; Philips set_power, set_bright, set_cct; chuangmi set_on/set_off (v1) and set_power, the on/power property per model; zhimi set_power, set_mode and the power, mode, aqi, humidity, temp_dec properties. The MIoT names (vacuum start-sweep, battery start-charge, light brightness and so on) are the standard URN names of miot-spec.org |
| Gateway sub-devices (gateway.go) | none published | python-miio (GPL-3.0): get_device_prop with lumi.0 and device_list (five values per device), get_device_list on lumi.gateway.mgl03, get_device_prop_exp, set_device_prop, get_battery, remove_device; seven type numbers of common sub-devices; parent_id in the account's device list (python-miio's micloud fixture) |
| Firmware update (ota.go) | none published | python-miio (GPL-3.0) device.py: miIO.ota with mode, install, app_url, file_md5 and proc; miIO.get_ota_progress; miIO.get_ota_state and its four states |
| Pushes (push.go) | none published | python-miio (GPL-3.0) push_server: the listener posing as a device, send_data_frame with a scene (trigger with did, extra, key, model, timespan, token; target with command, did, ip, model, the token encrypted with itself), the command's "<model>.<action>:<source>" shape, the answer {"result": 0}, miIO.xdel, the 49-character command limit |
| Account verification (cloudverify.go) | none published | Xiaomi-cloud-tokens-extractor (MIT) token_extractor.py: notificationUrl and its context, identity/list, sendEmailTicket, verifyEmail with _flag 8, identity/result/check, serviceLoginAuth2/end with the extension-pragma header, the captcha URL, the ick cookie, captCode and code 87001. The SMS step (sendPhoneTicket, verifyPhone, _flag 4) mirrors the e-mail one by the same public descriptions and is the least certain |
| Account device control (cloudrpc.go) | none published | public descriptions of open-source Mi account clients: miotspec/prop/get, miotspec/prop/set, miotspec/action, home/rpc/<did> |
| Specification mirror and translations (specmirror.go, spec.go, cmd/miiospec) | miot-spec.org's public service: /instances, /instance?type= and /instance/v2/multiLanguage?urn= | hass-xiaomi-miot (Apache-2.0) v1.1.5 core/miot_spec.py: status=all, the translations path and "data" member, the key shape service:NNN[:property:NNN[:valuelist:NNN]] and service:NNN:action:NNN with the value's position in the list, the host spec.miot-spec.com. The directory layout, the fetch-through, the index filter and the handler are our own design |
| mDNS discovery (mdns.go) | DNS-SD service type _miio._udp | python-miio (GPL-3.0) discovery.py: the instance name is the model with dashes, "_miio" and the device ID |
| Device requests (device.go) | none published | python-miio (GPL-3.0) device.py: miIO.config_router with ssid, passwd and uid; probing MIoT with one get_properties of service 1 property 1; get_prop asked in slices |
| Vacuum extras (vacuum.go) | none published | python-miio (GPL-3.0) roborock integration: get_consumable and its four counters, reset_consumable; get_clean_summary (list or object: time, area in mm², count, record IDs); get_clean_record (list or object: begin, end, duration, area, error, complete); get_dnd_timer and its fields, set_dnd_timer, close_dnd_timer; get_timer, set_timer, upd_timer, del_timer and the record layout; get_timezone; get_sound_volume, change_sound_volume, test_sound_volume; get_custom_mode, set_custom_mode; get_carpet_mode, set_carpet_mode and its five fields; find_me; app_spot. The MIoT names (identify, fan-level) are miot-spec.org's standard names |
| Interop peer (interop/peer) | our own Dockerfile, made-up specification, push.py and spec.py | python-miio's simulators and packet code, run at test time |

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
| model_test.go | a made-up specification |
| push_test.go | packets built by this module; the token-encryption rule from python-miio's description |
| specmirror_test.go, cmd/miiospec/main_test.go | a made-up specification service and made-up specifications and translations |
| mdns_test.go | made-up instance names in the published shape |
| interop/ | python-miio's simulators (with its own Roborock file), packet code and specification client at test time |

All tokens, device IDs, accounts and addresses are made-up demo values.
