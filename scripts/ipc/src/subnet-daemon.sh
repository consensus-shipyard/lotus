#!/usr/bin/env bash

set -e

if [ $# -ne 1 ]
then
    echo "Provide the subnet ID as first argument for the script"
    exit 1
fi

SUBNETID=$1
echo "[*] Populating config"

echo '
[ChainStore]
  EnableSplitstore = true
[API]
  ListenAddress = "/ip4/0.0.0.0/tcp/1234/http"
[Fevm]
  EnableEthRPC = true
' > $LOTUS_PATH/config.toml

echo "[*] Generate genesis for subnet deterministically"
if [[ "$SUBNETID" == "/r31415926" ]]; then
    eudico genesis new --subnet-id=$SUBNETID --template=/genesis-test.json --out=subnet.car
else
    eudico genesis new --subnet-id=$SUBNETID --template=/genesis.json --out=subnet.car
fi

echo "[*] Starting daemon"
eudico mir daemon --genesis=subnet.car --mir-validator --bootstrap=false
