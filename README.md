# miio

Clients for Xiaomi Mi Home Wi-Fi devices in pure Go: the miIO local protocol, MIoT requests, specifications from miot-spec.org and the Mi account device list; not affiliated with Xiaomi.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/miio.svg)](https://pkg.go.dev/github.com/cB-Sigma/miio) [![test](https://github.com/cB-Sigma/miio/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/miio/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.22-00ADD8.svg)](go.mod)

It talks miIO on UDP 54321 (token-encrypted JSON-RPC, the hello
handshake and discovery), sends MIoT get_properties, set_properties and
action requests, fetches a model's MIoT specification (or keeps a
mirror of them for sites without internet, served in miot-spec.org's
own shape) and maps it to names with value checks, finds devices by
hello or through the application's mDNS browser, drives vacuums
(with the Roborock consumables, history, schedule and settings),
lights, plugs and air purifiers through one helper per family, reaches the Zigbee sub-devices of
gateways, updates firmware, receives device pushes, and signs in to a Mi
account (completing captcha and e-mail or SMS checks) to list its devices
with their tokens and control them through the account. Standard library and
[lanbcast](https://github.com/cB-Sigma/lanbcast) only, no cgo. It was
written for SigmaM2M, a oneM2M platform, whose Xiaomi adapter is built
on it.

## Features

| Feature | Status |
|---|---|
| miIO packets, hello handshake, stamps, JSON-RPC with random request IDs and one retry | yes |
| Hello discovery (broadcast to every LAN interface through lanbcast) | yes |
| MIoT get_properties (batched by 15), set_properties, action, with per-property codes | yes |
| MIoT specifications from miot-spec.org (newest released version per model; the full index with `Status: "all"`; fallback hosts) | yes; none bundled |
| Offline specification mirror: a directory filled once from miot-spec.org (or carried in, or a site's own), served offline to the library and over HTTP in miot-spec.org's URL and JSON shape; the `miiospec` command fills and serves it | yes (`SpecMirror`, `cmd/miiospec`) |
| Specification translations (the service's multiLanguage texts, by service, property, value and action) | yes (`Translations`, `TextKey`); key shape from hass-xiaomi-miot, not checked against the live service |
| mDNS discovery (`_miio._udp`, model and device ID from the instance name) through the application's own mDNS browser | yes (`DiscoverMDNS`, `Browser`) |
| Wi-Fi setup of a new device, MIoT probe, legacy get_prop in slices, typed device errors | yes (`ConfigureWiFi`, `SupportsMIoT`, `GetProp`, `DeviceError`) |
| Mi account sign-in, homes, shared homes and device lists in every region | yes |
| Mi account captcha and identity check (code by e-mail or SMS) completed with a person's help | yes (`VerifyError`, `Captcha`, `SendCode`, `VerifyCode`); against the simulated account only |
| Specification mapped to names: properties and actions by "service.name", value list, range, step and access checked before sending | yes (`Model`, opt-in) |
| Family helpers: vacuums, lights, plugs, air purifiers, through the specification or the legacy per-model methods (Roborock, Yeelight, Philips, chuangmi, zhimi) | yes (`Vacuum`, `Light`, `Plug`, `Purifier`) |
| Roborock legacy extras: consumables and their reset, clean summary and records (both answer shapes), do-not-disturb, timers, time zone, voice volume, fan speed, carpet mode, find, spot | yes; fan speed and find also through a MIoT specification, with fan presets from its value list |
| Roborock maps, rooms, zones and segments, mop and dock functions, sound packs | not yet |
| Fan speed preset tables of legacy Roborock models | not bundled: they are data of a GPL project; read and set the number (`FanSpeed`, `SetFanSpeed`) |
| Zigbee sub-devices of lumi gateways: list (both list methods), read, write, battery, unpair; sub-devices from the account | yes (`Gateway`, `GatewaySubDevices`) |
| Firmware update: miIO.ota with URL and MD5, progress, state | yes (`Update`, `WaitUpdate`) |
| Device pushes through local scenes (send_data_frame, miIO.xdel) | yes (`PushServer`) |
| Device control through the Mi account (MIoT properties and actions, legacy methods relayed) | yes (`CloudGetProperties`, `CloudSetProperties`, `CloudDoAction`, `CloudCall`); against the simulated account only |
| Bluetooth devices | through the Mi account only (`CloudGetProperties`); no local method is known |
| Mi account scenes, statistics, firmware listing and other account APIs | not yet [vendor account]: building them needs a real Mi account to learn and check the answers against |
| miot-spec.org's standard type definitions (/spec/services and the like) in the mirror | not yet |

## Choosing a Go miIO library

Compared from each project's source at the version named, on
2026-10-12 (Go module proxy for the Go projects; PyPI and GitHub for the
Python ones). Check the projects themselves before relying on a row.
Newest versions found: python-miio 0.5.12 on PyPI (2022-07-18), with
0.6.0.dev0 (2024-03-13) and master at 9a00e08 (2026-07-25); micloud 0.6
(2022-12-08); hass-xiaomi-miot v1.1.5 (2026-09-07); mberatsanli/miio
2e4fd2a (2026-09-01), vkorn/go-miio e59e421 (2023-03-16) and
nickw444/miio-go 379bc4c (2019-08-25), each unchanged since the last
comparison. A search for newer Go code found only command-line tools
(such as miullu/miiot-cli, 2026-06-30, with a specification cache of its
own) and exporters, no library.

| | this module | mberatsanli/miio (2e4fd2a) | vkorn/go-miio (e59e421) | nickw444/miio-go (379bc4c) | python-miio master 9a00e08 (Python) | hass-xiaomi-miot v1.1.5 (Python, Home Assistant) |
|---|---|---|---|---|---|---|
| miIO packets, handshake, discovery | yes, and mDNS through the application's browser | yes | yes | yes | yes, and mDNS (zeroconf) | yes |
| MIoT get/set properties, actions | yes | yes | no | no | yes | yes |
| MIoT specifications from miot-spec.org | yes, with fallback hosts and translations | no (a written-up spec for one model) | no | no | yes | yes, with translations and a second host |
| Specifications kept for offline use | a mirror directory, served over HTTP in the service's shape | no | no | no | a cache that expires after hours | Home Assistant storage, renewed after weeks |
| Mi account device and token list | yes | yes | no | no | through micloud | yes |
| Mi account e-mail verification completed | yes, and SMS and captcha (simulated account only) | yes | no | no | no | yes |
| Mi account scenes, statistics and the like | no | no | no | no | no | some |
| Specification mapped to names with value checks | yes | no | no | no | yes (genericmiot) | yes |
| Per-model helpers (vacuums, gateways, lights...) | four families across MIoT and legacy models, Roborock consumables, history, schedule and settings, gateway sub-devices | one Roborock model with maps and rooms | Aqara gateway, sensors, vacuum | Yeelight, plugs | very many, with model quirks, Roborock maps and docks | very many, by specification and per-model tweaks |
| Wi-Fi setup of a new device | yes | no | no | no | yes | no |
| Firmware update, device pushes | yes | no | no | no | yes | no |
| Device, account and specification simulators | yes, separate module | no | no | a device simulator | miio and MIoT device simulators | no |
| Interop evidence | against python-miio both ways (below) | none stated | none stated | none stated | the reference | none stated |
| Go version, dependencies | 1.22; lanbcast | 1.25; cobra, x/term (CLI) | dep era, no go.mod | 1.12; several | n/a | n/a |
| Licence | Apache-2.0 | MIT | MIT | MIT | GPL-3.0 | Apache-2.0 |

Pick this module for a Go protocol layer with MIoT, specification
mapping and an offline specification mirror, the common device families
with the Roborock legacy extras, gateways, firmware update, pushes and
the account, with simulators for your tests. Pick mberatsanli/miio when
you need its Roborock driver's maps and rooms, or a verification flow
already used against real accounts (ours has run only against our
simulator). Use python-miio outside Go, or when you need the per-model
features of its many integrations (vacuum maps, docks, model-specific
settings); use hass-xiaomi-miot inside Home Assistant, or for the
account's scenes and statistics.

## Install

```
go get github.com/cB-Sigma/miio@latest
```

Requires Go 1.22 or later.

## Quick start

```go
import "github.com/cB-Sigma/miio"

cloud := &miio.Cloud{Username: email, Password: password}
devs, err := cloud.Devices(ctx, []string{"i2"}) // India; miio.Regions lists all
tok, _ := hex.DecodeString(devs[0].Token)
c := &miio.Client{Addr: devs[0].LocalIP + ":54321", Token: tok, Timeout: 5 * time.Second}
info, err := c.Call(ctx, "miIO.info", nil)

// A MIoT device: read power (service 2, property 2) and switch it off.
res, err := miio.GetProperties(ctx, c, []miio.PropRef{{DID: devs[0].DID, SIID: 2, PIID: 2}})
_, err = miio.SetProperties(ctx, c, []miio.PropRef{{DID: devs[0].DID, SIID: 2, PIID: 2, Value: false}})

// The same by name, from the model's specification, and a family helper.
m, err := miio.ModelFor(ctx, &miio.HTTPSpecs{Base: miio.DefaultSpecURL, HTTP: http.DefaultClient}, devs[0].Model)
err = m.Set(ctx, c, devs[0].DID, "air-purifier.mode", "Sleep") // checked against the value list
p := &miio.Purifier{Family: miio.Family{Client: c, DID: devs[0].DID, Model: m}}
st, err := p.Status(ctx)

// A site without internet: fill a mirror once where there is internet,
// carry the directory, then read it offline (or serve m.Handler()).
m := &miio.SpecMirror{Dir: "/var/lib/miot-spec", Upstream: &miio.HTTPSpecs{Base: miio.DefaultSpecURL}}
err = m.Fill(ctx, "zhimi.airpurifier.ma4")
model, err := miio.ModelFor(ctx, &miio.SpecMirror{Dir: "/var/lib/miot-spec"}, "zhimi.airpurifier.ma4")

// When the account asks for a code at sign-in.
var ve *miio.VerifyError
if errors.As(cloud.Login(ctx), &ve) && ve.NotificationURL != "" {
	err = cloud.SendCode(ctx, miio.ByEmail)
	err = cloud.VerifyCode(ctx, miio.ByEmail, codeFromThePerson)
}
```

`Example` and `ExampleGetProperties` in example_test.go show the same
(they need an account and a device, so they are not run); `ExampleKeys`
and `ExampleNewModel` run. [miio-sim](https://github.com/cB-Sigma/miio-sim) runs them against
simulated devices and a simulated account.

## Documentation

API reference: https://pkg.go.dev/github.com/cB-Sigma/miio

| Package | What it does |
|---|---|
| `miio` (module root) | miIO: `Client` (`Call`, `DeviceID`), `Discover`, `Hello`, `Packet`, `Open`, `Keys`. MIoT: `GetProperties`, `SetProperties`, `DoAction`, `PropRef`, `PropResult`, `PropError`; specifications: `Spec`, `HTTPSpecs` (`Index`, `RawIndex`, `RawInstance`, `Translations`, `Fallbacks`, `Status`), `SpecInstance`, `SpecSource`, `SpecMirror` (`Fill`, `FillTranslations`, `FillAll`, `Add`, `AddTranslations`, `Models`, `Stored`, `URN`, `Handler`), `Translations`, `TextKey`, `ParseTranslations`, `URNName`; names: `Model` (`NewModel`, `ModelFor`, `Get`, `Set`, `SetMany`, `Do`), `Property.Check`, `Describe`, `ValueOf`. Discovery and setup: `DiscoverMDNS`, `Browser`, `BrowseFunc`, `MDNSEntry`, `MDNSDevice`, `ParseMDNSInstance`, `ConfigureWiFi`, `SupportsMIoT`, `GetProp`, `DeviceError`. Families: `Family`, `Vacuum` (also `Consumables`, `ResetConsumable`, `CleanSummary`, `CleanRecord`, `DND`, `SetDND`, `DisableDND`, `Timers`, `AddTimer`, `EnableTimer`, `DeleteTimer`, `Timezone`, `Volume`, `SetVolume`, `TestVolume`, `FanSpeed`, `SetFanSpeed`, `FanSpeedPresets`, `CarpetMode`, `SetCarpetMode`, `SetCarpetModeSettings`, `Find`, `Spot`), `Light`, `Plug`, `Purifier`. Gateways: `Gateway`, `SubDevice`, `GatewaySubDevices`. Firmware: `Update`, `UpdateProgress`, `UpdateState`, `WaitUpdate`. Pushes: `PushServer`, `EventInfo`, `Push`, `TokenEnc`. Mi account: `Cloud` (`Login`, `Devices`, `Captcha`, `SendCode`, `VerifyCode`, `CloudGetProperties`, `CloudSetProperties`, `CloudDoAction`, `CloudCall`), `VerifyError`, `CloudDevice`, `Regions` |
| `cmd/miiospec` | Fills a specification mirror directory from miot-spec.org (`-fill models`, `-texts`, `-all n`) and serves it (`-listen :8080`), offline with `-upstream ""` |
| `interop` (nested module, tests only) | this module against python-miio's miio and MIoT device simulators and its packet code, in a container |

Changes per version are in [CHANGELOG.md](CHANGELOG.md).

## Testing

```
go test -race ./...
cd interop && SIGMA_MIIO_INTEROP=1 go test -v ./...   # needs Docker
```

Tests carry `// Covers: SIG-...` comments: requirement IDs of the
SigmaM2M project's compliance report, with no meaning outside it; the
gateway, firmware, push and v0.3.0 tests claim none. The tests of the client,
the families, gateways, firmware update, pushes and the account against
the simulators are in miio-sim.

`interop/` is a nested module with its own go.mod. Its tests build an
image from `interop/peer` (python:3.12.10-slim with python-miio from a
fixed commit, 9a00e08, of its master branch, which installs as
0.6.0.dev0: its simulators are not in a PyPI release yet) and are
skipped unless `SIGMA_MIIO_INTEROP=1` is set and Docker answers.

### Interop tested against

- v0.3.0: python-miio master 9a00e08 (0.6.0.dev0) with micloud 0.6, on
  python:3.12.10-slim, Docker Desktop on arm64, 2026-10-12
  (`interop`; all six tests passed, the four earlier ones repeated):
  - `miiocli devtools miio-simulator` with the Roborock file python-miio
    ships (`simulated_roborock.yaml`, model roborock.vacuum.a15):
    `Vacuum` read the status, consumables, the clean summary in its
    list shape, a clean record in its object shape, do-not-disturb, the
    timers and the time zone as python-miio's simulator answers them; a
    method the file lacks came back as a device error. One observation,
    not a difference: the file's get_timer answer nests its second timer
    inside the first record, so it reads as one timer, in python-miio's
    own Timer as here.
  - python-miio's specification client (`MiotCloud` over micloud's
    `MiotSpec`, its base URL pointed at the mirror) against
    `SpecMirror.Handler` serving offline what `Add` put in: it picked
    the model's release from the index and built its pydantic device
    model from the specification (services and properties as sent).
  - Not covered by a peer: mDNS discovery (Docker Desktop does not pass
    multicast between the host and containers; `DiscoverMDNS` is tested
    with made-up browser answers and the simulator's names), the
    translations endpoint (python-miio does not read it), and the live
    miot-spec.org service.

- python-miio master 9a00e08 (0.6.0.dev0) on python:3.12.10-slim, Docker
  Desktop on arm64, 2026-10-11 (`interop`; all passed):
  - `miiocli devtools miio-simulator` with the zhimi.fan file python-miio
    ships: hello handshake (device ID as python-miio derives it),
    miIO.info, get_prop, set_power and the changed value, an unknown
    method refused, a wrong token unanswered, discovery by hello.
  - `miiocli devtools miot-simulator` with a made-up specification
    (`interop/peer/spec.json`): get_properties with codes (an unknown
    property refused), set_properties and the values read back, a
    read-only property refused, an action. The same specification
    through `Model`: properties set and read by name, a mode set by its
    description, a value outside the list refused before sending, an
    action by name, the purifier helper's mode and status.
  - A push built with python-miio's own packet code (`interop/peer/push.py`)
    to `PushServer`: python-miio read the hello answer and the push's
    answer (result 0 under the server's device ID); the handler got the
    source and action.
  - The v0.1.0 run (python:3.12-slim) found that the simulator answers
    miIO.info without a "result" member; `Client.Call` returns such an
    answer whole, as python-miio does (`TestAnswerWithoutResult`).
- python-miio's client (gateway, firmware update, vacuum with the
  Roborock extras, Wi-Fi setup and the MIoT probe, plug, push server)
  and micloud 0.6 against the simulators, in
  [miio-sim](https://github.com/cB-Sigma/miio-sim).
- Not yet [hardware]: a real Xiaomi device, gateway or sub-device; the
  family helpers against real vacuums, lights, plugs and purifiers;
  firmware update with a vendor image (a wrong image can break a
  device); pushes from a real device's scene (which trigger strings a
  model accepts is found from the vendor app's own scenes).
- Not yet [vendor account]: the captcha and e-mail or SMS check, device
  control through the account and the device list against a real Mi
  account; the live miot-spec.org service (the mirror, fallbacks and
  translations have run only against made-up services).
- Not yet [hardware]: the Roborock extras and Wi-Fi setup against a real
  vacuum or a device in setup mode; mDNS names announced by real
  devices.

## Related repositories

- [miio-sim](https://github.com/cB-Sigma/miio-sim): simulated miIO and
  MIoT devices, gateways with sub-devices, firmware update and pushes, a
  simulated Mi account (with its verification steps) and specification
  service, as a Go package and a command.
- [lanbcast](https://github.com/cB-Sigma/lanbcast): the broadcast helper
  discovery uses.

## Status

v0.x: the API may change before v1.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

See [SECURITY.md](SECURITY.md) to report a vulnerability privately.

## Licence

Apache-2.0 (see [LICENSE](LICENSE) and [NOTICE](NOTICE)). Copyright 2026
Abhinav Gautam. Xiaomi, Mi, Mi Home and MIoT are trademarks of their
owner; the names are used only to say which devices and services this
module talks to.
