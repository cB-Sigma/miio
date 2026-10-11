// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package miio speaks the local protocol of Xiaomi Mi Home Wi-Fi devices:
// miIO packets on UDP port 54321 with the token-derived encryption
// (Client, Discover), the legacy JSON-RPC methods, the MIoT property and
// action requests (GetProperties, SetProperties, DoAction) with the
// models' MIoT specifications (HTTPSpecs, or SpecMirror for sites
// without internet, with their translations) mapped to names (Model),
// discovery by mDNS through the application's browser (DiscoverMDNS),
// helpers
// for vacuums, lights, plugs and air purifiers (Vacuum, Light, Plug,
// Purifier; the vacuum also with the Roborock consumables, history,
// schedule and settings), Wi-Fi setup (ConfigureWiFi), gateway
// sub-devices (Gateway), firmware update (Update),
// device pushes (PushServer), and the Mi account client that lists an
// account's devices with their tokens, completes its verification steps
// and controls devices through the account (Cloud).
//
// Xiaomi does not document the local protocol or the account API; this
// package follows public descriptions (see PROVENANCE.md). The module
// github.com/cB-Sigma/miio-sim simulates devices, gateways, the account
// and the specification service for tests.
package miio
