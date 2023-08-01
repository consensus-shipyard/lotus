#!/usr/bin/env bash

if [ $# -lt 1 ] || [ $# -gt 2 ]
then
    echo "Provide the running container for the validator as an argument."
    echo "mine-subnet.sh [--debug] containerID"
    exit 1
fi

if [ "$1" = "--debug" ]; then
  if [ "$2" = "" ]; then
    echo "Empty containerID"
    exit
  fi
    echo "[*] Starting to mine on validator in debug mode"
    docker exec -e MIR_INTERCEPTOR_OUTPUT="mir-event-logs" -it $2 eudico mir validator run --membership=onchain --nosync --ipcagent-url=http://host.docker.internal:3030/json_rpc
else
    echo "[*] Starting to mine on validator"
    docker exec -it $1 eudico mir validator run --membership=onchain --nosync --ipcagent-url=http://host.docker.internal:3030/json_rpc
fi


