#!/usr/bin/env bash

set -e

if [ $# -ne 1 ]
then
    echo "Provide the subnet ID as first argument"
    exit 1
fi

SUBNETID=$1

tmux new-session -d -s "mir" \; \
     new-window   -t "mir" \; \
     split-window -t "mir:0" -v \; \
     \
     send-keys -t "mir:0" "
        /scripts/ipc/src/subnet-daemon.sh $SUBNETID" Enter \; \
            send-keys -t "mir:0.0" "
        /scripts/ipc/src/subnet-validator.sh" Enter \; \
            \
            attach-session -t "mir:0"
