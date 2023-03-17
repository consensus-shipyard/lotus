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

eudico wait-api --timeout=300
echo "[*] Importing validator key"
eudico wallet import --as-default --format=hex-lotus <<< $VAL_KEY
eudico mir validator config init
# eudico mir validator config validator-addr
# eudico mir validator run --membership=onchain --nosync
