#!/usr/bin/env bash

set -e

if [ $# -ne 2 ]
then
    echo "Provide the subnet ID as first argument and the default validator script as the second one"
    exit 1
fi

SUBNETID=$1
VAL_KEY=$2

tmux new-session -d -s "mir" \; \
     new-window   -t "mir" \; \
     split-window -t "mir:0" -v \; \
     \
     send-keys -t "mir:0" "
        /scripts/ipc/src/subnet-daemon.sh $SUBNETID" Enter \; \
            send-keys -t "mir:0.0" "
        /scripts/ipc/src/subnet-validator.sh $VAL_KEY" Enter \; \
            \
            attach-session -t "mir:0"
