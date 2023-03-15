#!/usr/bin/env bash
set -e

if [ $# -ne 1 ]
then
    echo "Provide the port where lotus will be listening as first argument"
    exit 1
fi

PORT=$1

echo "[*] Running docker container for root in port $PORT"
img=`docker run -dit -p $PORT:1234 --entrypoint "/scripts/ipc/entrypoints/eudico-root-single.sh" eudico`
echo "[*] Waiting for the daemon to start"
sleep 60
token=`docker exec -it $img  eudico auth create-token --perm admin`
echo ">>> Token to /root daemon: $token"
wallet=`docker exec -it $img  eudico wallet default`
echo ">>> Default wallet: $wallet"
