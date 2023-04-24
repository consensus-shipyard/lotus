#!/usr/bin/env bash
echo "[*] Re-compiling"
make spacenet
echo "[*] Generating spacenet genesis"
./eudico genesis new --template=./eudico-core/genesis/genesis.json --out=./build/genesis/spacenet.car
echo "[*] Generating test genesis"
./eudico genesis new --template=./eudico-core/genesis/genesis-test.json --out=./scripts/mir/genesis.car
