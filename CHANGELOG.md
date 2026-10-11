# Changelog

All notable changes to this module. Versions follow semantic versioning;
before v1 the API may change in minor versions.

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
