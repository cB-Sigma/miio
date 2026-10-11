# Changelog

All notable changes to this module. Versions follow semantic versioning;
before v1 the API may change in minor versions.

## v0.2.0 (2026-10-11)

Additions only; v0.1.0 code keeps working.

- `Model` (`NewModel`, `ModelFor`): a MIoT specification mapped to
  "service.name" names, with `Get`, `Set`, `SetMany` and `Do` by name;
  `Property.Check` tests format, value range and step, value list and
  access before anything is sent; `Describe` and `ValueOf` turn
  value-list numbers into descriptions and back.
- Family helpers `Vacuum`, `Light`, `Plug` and `Purifier`: through a
  `Model` for MIoT devices, or the legacy per-model methods (Roborock,
  Yeelight, Philips, chuangmi, zhimi) without one. `ErrUnsupported` for a
  function the model lacks.
- `Gateway`: Zigbee sub-devices of lumi gateways (both list methods,
  properties read one by one or together, written, battery, unpairing);
  `GatewaySubDevices` picks them from the account's list, whose
  `CloudDevice` now carries `ParentID` and `ParentModel`.
- Firmware update: `Update`, `UpdateProgress`, `UpdateState`,
  `WaitUpdate`.
- Pushes: `PushServer` installs local scenes on devices
  (`Subscribe`, `Unsubscribe`) and receives their events; `TokenEnc`.
- Mi account: a captcha or identity check now comes as `*VerifyError`
  (still `errors.Is(err, ErrCloudVerify)`), completed with `Captcha` and
  `CaptchaCode`, or `SendCode` and `VerifyCode` by e-mail or SMS; device
  control through the account with `CloudGetProperties`,
  `CloudSetProperties`, `CloudDoAction` and `CloudCall`. `Action` has
  `Description` and `Out`.
- The text of `ErrCloudVerify` no longer says a server cannot complete
  the check.
- Interop: runs on python:3.12.10-slim; the specification mapping against
  python-miio's MIoT simulator and the push server against python-miio's
  packet code were added.

## v0.1.0 (2026-10-11)

First release as a module of its own, split from SigmaM2M: the miIO
client (`Client`, `Discover`, `Hello`, `Packet`, `Open`, `Keys`), MIoT
requests (`GetProperties`, `SetProperties`, `DoAction`), specifications
(`HTTPSpecs`), and the Mi account device list (`Cloud`).

- `Client.Call` returns an answer that has neither "result" nor "error"
  whole, as python-miio does (python-miio's simulators answer miIO.info
  so); before, it returned an empty result.
- The simulators live in their own module,
  [miio-sim](https://github.com/cB-Sigma/miio-sim), with the tests that
  drive them.
- Interop tests against python-miio's device simulators in a nested
  test-only module, `interop`.
