
#!/usr/bin/env bash
set -e

if [ $# -ne 4 ]
then
    echo "Provide the port where lotus and validator will be listening as first/second argument, the subnet id as third, and the validator address as fourth"
    echo "Args: [port] [validator_libp2p_port] [subnet_id] [import_validator_key_absolute_path]"
    exit 1
fi

PORT=$1
VAL_PORT=$2
SUBNETID=$3
VAL_KEY_ABSOLUTE_PATH=$4

echo "[*] Running docker container for root in port $PORT"
img=`docker run -dit --add-host host.docker.internal:host-gateway -p $PORT:1234 -p $VAL_PORT:1347 -v $VAL_KEY_ABSOLUTE_PATH:/wallet.key:ro --entrypoint "/scripts/ipc/entrypoints/eudico-subnet.sh" eudico $SUBNETID`
echo "[*] Waiting for the daemon to start"
docker exec -it $img  eudico wait-api --timeout 120s
sleep 10
echo ">>> Subnet $SUBNETID daemon running in container: $img"
token=`docker exec -it $img  eudico auth create-token --perm admin`
echo ">>> Token to $SUBNETID daemon: $token"
wallet=`docker exec -it $img  eudico wallet default`
echo ">>> Default wallet: $wallet"
val=`docker exec -it $img  eudico mir validator config validator-addr`
echo ">>> Subnet subnet validator info:"
echo $val
echo ">>> API listening in host port $PORT"
echo ">>> Validator listening in host port $VAL_PORT"
