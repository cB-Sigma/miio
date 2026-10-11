# Copyright 2026 Abhinav Gautam
# SPDX-License-Identifier: Apache-2.0
#
# A device push built with python-miio's own packet code, sent to the Go
# push server, for interop tests:
#
#   python push.py <host> <port> <token hex> <device id>
#
# Sends a probe (so the test learns the address the packets come from),
# waits two seconds, sends a hello and a push request, and prints
# "PEER <step> <json>" lines with what came back.

import datetime
import json
import socket
import sys
import time

from miio.protocol import Message


def say(step, obj=None):
    print("PEER " + step + ("" if obj is None else " " + json.dumps(obj, sort_keys=True, default=str)), flush=True)


host, port, token, did = sys.argv[1], int(sys.argv[2]), bytes.fromhex(sys.argv[3]), int(sys.argv[4])
addr = (host, port)
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.settimeout(5)
s.sendto(b"probe", addr)
time.sleep(2)

s.sendto(bytes.fromhex("21310020" + "ff" * 28), addr)
data, _ = s.recvfrom(1024)
say("hello", {"length": len(data), "device_id": int.from_bytes(data[8:12], "big")})

msg = {
    "data": {"value": {"id": 31, "method": "chuangmi.plug.v3.open:lumi_158d0001aaaa02", "params": ["open"]}},
    "header": {"value": {"length": 0, "unknown": 0, "device_id": did.to_bytes(4, "big"), "ts": datetime.datetime.now()}},
    "checksum": 0,
}
s.sendto(Message.build(msg, token=token), addr)
data, _ = s.recvfrom(4096)
m = Message.parse(data, token=token)
say("answer", {"data": m.data.value, "device_id": int.from_bytes(m.header.value.device_id, "big")})
say("done")
