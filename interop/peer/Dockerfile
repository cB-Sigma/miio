# Copyright 2026 Abhinav Gautam
# SPDX-License-Identifier: Apache-2.0
#
# python-miio (GPL-3.0, github.com/rytilahti/python-miio) as an interop
# peer: its miio and MIoT device simulators (miiocli devtools). It is
# downloaded and installed at build time inside the image only; nothing
# of it is copied into this repository. The simulators are not in a PyPI
# release yet, so a fixed commit of the master branch is used.
FROM python:3.12-slim
ARG MIIO_COMMIT=9a00e0888ef85f8d8d84888cabd9e46d1b0a18df
ADD https://github.com/rytilahti/python-miio/archive/${MIIO_COMMIT}.tar.gz /python-miio.tgz
RUN mkdir /python-miio && tar -xzf /python-miio.tgz -C /python-miio --strip-components=1 && rm /python-miio.tgz \
	&& pip install --no-cache-dir /python-miio
COPY spec.json /peer/spec.json
WORKDIR /peer
