# miio

Clients for Xiaomi Mi Home Wi-Fi devices in pure Go: the miIO local protocol, MIoT requests, specifications from miot-spec.org and the Mi account device list; not affiliated with Xiaomi.

[![Go Reference](https://pkg.go.dev/badge/github.com/cB-Sigma/miio.svg)](https://pkg.go.dev/github.com/cB-Sigma/miio) [![test](https://github.com/cB-Sigma/miio/actions/workflows/test.yml/badge.svg)](https://github.com/cB-Sigma/miio/actions/workflows/test.yml) [![Licence: Apache-2.0](https://img.shields.io/badge/licence-Apache--2.0-blue.svg)](LICENSE) [![Go](https://img.shields.io/badge/go-1.22-00ADD8.svg)](go.mod)

It talks miIO on UDP 54321 (token-encrypted JSON-RPC, the hello
handshake and discovery), sends MIoT get_properties, set_properties and
action requests, fetches a model's MIoT specification, and signs in to a
Mi account to list its devices with their tokens and local addresses, so
tokens need not be extracted by hand. Standard library and
[lanbcast](https://github.com/cB-Sigma/lanbcast) only, no cgo. It was
written for SigmaM2M, a oneM2M platform, whose Xiaomi adapter is built
on it.

## Features

| Feature | Status |
|---|---|
| miIO packets, hello handshake, stamps, JSON-RPC with random request IDs and one retry | yes |
| Hello discovery (broadcast to every LAN interface through lanbcast) | yes |
| MIoT get_properties (batched by 15), set_properties, action, with per-property codes | yes |
| MIoT specifications from miot-spec.org (newest released version per model) | yes; none bundled |
| Mi account sign-in, homes, shared homes and device lists in every region | yes |
| Mi account identity verification (e-mail or SMS ticket, captcha) | reported as `ErrCloudVerify`, not completed |
| Mapping specifications to a device model, legacy per-model methods | no; that is the caller's |
| Bluetooth and Zigbee sub-devices of gateways, firmware update, the Mi account's other APIs | no |

## Choosing a Go miIO library

Compared from each project's source at the version named, on
2026-10-11. Check the projects themselves before relying on a row.

| | this module | mberatsanli/miio (2026-09-01) | vkorn/go-miio (2023-03-16) | nickw444/miio-go (2019-08-25) | python-miio master 9a00e08 (Python) |
|---|---|---|---|---|---|
| miIO packets, handshake, discovery | yes | yes | yes | yes | yes |
| MIoT get/set properties, actions | yes | yes | no | no | yes |
| MIoT specifications from miot-spec.org | yes | no (a written-up spec for one model) | no | no | yes |
| Mi account device and token list | yes | yes | no | no | through micloud |
| Mi account e-mail verification completed | no (reported) | yes | no | no | no |
| Per-model helpers (vacuums, gateways, lights...) | no | one Roborock model | Aqara gateway, sensors, vacuum | Yeelight, plugs | very many |
| Device, account and specification simulators | yes, separate module | no | no | a device simulator | miio and MIoT device simulators |
| Interop evidence | against python-miio both ways (below) | none stated | none stated | none stated | the reference |
| Go version, dependencies | 1.22; lanbcast | 1.25; cobra, x/term (CLI) | dep era, no go.mod | 1.12; several | n/a |
| Licence | Apache-2.0 | MIT | MIT | MIT | GPL-3.0 |

Pick this module when you want a small protocol layer with MIoT and the
account's token list under your own device model, with simulators for
your tests. Pick mberatsanli/miio when your account asks for e-mail
verification and you want the library to complete it, or for its
Roborock driver. Use python-miio outside Go, or when you need its many
per-model integrations.

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
```

`Example` and `ExampleGetProperties` in example_test.go show the same
(they need an account and a device, so they are not run); `ExampleKeys`
runs. [miio-sim](https://github.com/cB-Sigma/miio-sim) runs them against
simulated devices and a simulated account.

## Documentation

API reference: https://pkg.go.dev/github.com/cB-Sigma/miio

| Package | What it does |
|---|---|
| `miio` (module root) | miIO: `Client` (`Call`, `DeviceID`), `Discover`, `Hello`, `Packet`, `Open`, `Keys`. MIoT: `GetProperties`, `SetProperties`, `DoAction`, `PropRef`, `PropResult`, `PropError`; specifications: `Spec`, `HTTPSpecs`, `SpecSource`, `URNName`. Mi account: `Cloud` (`Login`, `Devices`), `CloudDevice`, `Regions` |
| `interop` (nested module, tests only) | this client against python-miio's miio and MIoT device simulators, in a container |

Changes per version are in [CHANGELOG.md](CHANGELOG.md).

## Testing

```
go test -race ./...
cd interop && SIGMA_MIIO_INTEROP=1 go test -v ./...   # needs Docker
```

Tests carry `// Covers: SIG-...` comments: requirement IDs of the
SigmaM2M project's compliance report, with no meaning outside it. The
client, MIoT, account and specification tests against the simulators
are in miio-sim.

`interop/` is a nested module with its own go.mod. Its tests build an
image from `interop/peer` (python:3.12-slim with python-miio from a fixed
commit, 9a00e08, of its master branch: its simulators are not in a PyPI
release yet) and are skipped unless `SIGMA_MIIO_INTEROP=1` is set and
Docker answers.

### Interop tested against

- python-miio's device simulators (master 9a00e08, 2026-10-11, Docker
  Desktop on arm64; `interop`):
  - `miiocli devtools miio-simulator` with the zhimi.fan file python-miio
    ships: hello handshake (device ID as python-miio derives it),
    miIO.info, get_prop, set_power and the changed value, an unknown
    method refused, a wrong token unanswered, discovery by hello. Passed.
  - `miiocli devtools miot-simulator` with a made-up specification
    (`interop/peer/spec.json`): get_properties with codes (an unknown
    property refused), set_properties and the values read back, a
    read-only property refused, an action. Passed.
  - The first run found that the simulator answers miIO.info without a
    "result" member; `Client.Call` now returns such an answer whole, as
    python-miio does (`TestAnswerWithoutResult`).
- python-miio's client against the simulators, in
  [miio-sim](https://github.com/cB-Sigma/miio-sim).
- Not yet: a real Xiaomi device, a real Mi account, the live
  miot-spec.org service.

## Related repositories

- [miio-sim](https://github.com/cB-Sigma/miio-sim): simulated miIO and
  MIoT devices, a simulated Mi account and specification service, as a
  Go package and a command.
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
