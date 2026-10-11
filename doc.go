// Copyright 2026 Abhinav Gautam
// SPDX-License-Identifier: Apache-2.0

// Package miio speaks the local protocol of Xiaomi Mi Home Wi-Fi devices:
// miIO packets on UDP port 54321 with the token-derived encryption
// (Client, Discover), the legacy JSON-RPC methods, the MIoT property and
// action requests (GetProperties, SetProperties, DoAction) with the
// models' MIoT specifications (HTTPSpecs), and the Mi account client that
// lists an account's devices with their tokens (Cloud), so tokens need
// not be extracted by hand.
//
// Xiaomi does not document the local protocol or the account API; this
// package follows public descriptions (see PROVENANCE.md). The sim
// package simulates devices, the account and the specification service
// for tests.
package miio
