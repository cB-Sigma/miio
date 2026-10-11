# Copyright 2026 Abhinav Gautam
# SPDX-License-Identifier: Apache-2.0
#
# python-miio's MIoT specification client (MiotCloud, with micloud's
# MiotSpec under it) against a specification mirror, for interop tests:
#
#   python spec.py <base URL ending in /miot-spec-v2> <model>
#
# Prints "PEER <step> <json>" after each step and "PEER done" at the end.

import json
import sys

from micloud.miotspec import MiotSpec

from miio.miot_cloud import MiotCloud


def say(step, obj=None):
    print("PEER " + step + ("" if obj is None else " " + json.dumps(obj, sort_keys=True, default=str)), flush=True)


base, model = sys.argv[1], sys.argv[2]
MiotSpec.BASE_URL = base
mc = MiotCloud()
info = mc.get_release_list().info_for_model(model)
say("release", {"model": info.model, "type": info.type, "version": info.version})
dm = mc.get_device_model(model)
say("model", {"description": dm.description, "services": sorted(s.siid for s in dm.services),
              "properties": sum(len(s.properties) for s in dm.services)})
say("done")
