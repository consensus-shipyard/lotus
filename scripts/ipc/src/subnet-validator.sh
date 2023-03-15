#!/usr/bin/env bash

set -e

if [ $# -ne 1 ]
then
    echo "Provide the default validator wallet to import as the first argument"
    exit 1
fi

VAL_KEY=$1
# TODO: Optionally we can set the ipc-agent enpdoint
# Right now lets use the default one
# AGENT=$2

sleep 20
eudico wait-api
echo "[*] Importing validator key"
eudico wallet import --format=hex-lotus --as-default <<< $VAL_KEY
eudico mir validator run --membership=onchain --nosync
