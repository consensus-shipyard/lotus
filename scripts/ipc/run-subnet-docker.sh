
#!/usr/bin/env bash
set -e

if [ $# -ne 4 ]
then
    echo "Provide the port where lotus will be listening as first argument, the subnet id as second, and the validator address as third"
    echo "Args: [port] [validator_libp2p_port] [subnet_id] [import_validator_key]"
    exit 1
fi

PORT=$1
VAL_PORT=$2
SUBNETID=$3
VAL_KEY=$3

echo "[*] Running docker container for root in port $PORT"
img=`docker run -dit --add-host host.docker.internal:host-gateway -p $PORT:1234 -p $VAL_PORT:1347 --entrypoint "/scripts/ipc/entrypoints/eudico-subnet.sh" eudico $SUBNETID $VAL_KEY`
echo "[*] Waiting for the daemon to start"
docker exec -it $img  eudico wait-api --timeout 120
echo ">>> Subnet $SUBNETID daemon running in container: $img"
token=`docker exec -it $img  eudico auth create-token --perm admin`
echo ">>> Token to $SUBNETID daemon: $token"
wallet=`docker exec -it $img  eudico wallet default`
echo ">>> Default wallet: $wallet"
val=`docker exec -it $img  eudico mir validator config validator-addr`
echo ">>> Subnet subnet validator info:"
echo $val
