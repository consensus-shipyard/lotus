# IPC infra scripts
This directory includes a set of convenient scripts to light the burden of running the infrastructure required to run a full IPC deployment when debugging and testing. These scripts could also be used for inspiration to run your own production infrastructure for IPC.

The scrips are organized as follows:
- The scripts on the root of this directories are all you need to deploy the basic infrastructure you need to start tinkering with IPC and the [IPC agent](https://github.com/consensus-shipyard/ipc-agent). These scripts run a set of docker containers that bundle the validators and daemons that you need to run minimal deployments of a rootnet and subnet nodes.
- `entrypoint` includes all entrypoint scripts used by the docker images to run rootnet and subnet nodes.
- `src` includes smaller scripts with simple deployment stages that are used by the entrypoints and the root scripts to orchestrate the deployments.

If you are a power user looking to debug or extend the basic IPC deployments feel free to dig into `src/` and `entrypoints/` or open an issue with your request. We may be missing a lot of documentation so help us populate it.

## Usage
All these scripts assume the existence that the docker image for `eudico` exists in your local environment. If this is not the case you can build it yourself going to the root path of this repo (where the [Dockerfile](../../Dockerfile) lives) and running:

```bash
docker build -t eudico .
```

### Minimal rootnet deployment
In order to run IPC and test the deployment of subnets, we first need to run a rootnet. Running a Mir-based rootnet with a single validator for IPC using Docker is as simple as running the following script:
```
./scripts/ipc/run-root-docker-1val.sh <API_PORT> <VALIDATOR_LIBP2P_PORT>
```
As an argument we need to pass the port where we want the lotus daemon for rootnet to be listening and the default libp2p port used by the unique validator. The result of the scripts is the deployment of this single validator subnet and the api token and default wallet that we can use to interact with this network through the IPC agent: 
```
$ ./scripts/ipc/run-root-docker-1val.sh 1235 1239
[*] Running docker container for root in port 1235
[*] Waiting for the daemon to start
Not online yet... (could not get API info for FullNode: could not get api endpoint: API not running (no endpoint))
Not online yet... (could not get API info for FullNode: could not get api endpoint: API not running (no endpoint))
=== (this process may take several minutes until all the infrastructure is deployed)

>>> Root daemon running in container: 4f8f0577143c1a5de4111b4053f8d485c8aaabbba462e7e5465f628f039ecba9 (friendly name: ipc_root_1235)
>>> Token to /root daemon: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0._-2xzek2c3QzYqt5MWdQYrpRtK_Kqi8uEu3bvcm0i40
>>> Default wallet: t1cp4q4lqsdhob23ysywffg2tvbmar5cshia4rweq
```
The information about the token to the daemon and the default wallet will be used to configure the [IPC agent](https://github.com/consensus-shipyard/ipc-agent) to interact with the rootnet. The docker container will be named `ipc_root_<API_PORT>`. Refer to the agent's documentation for further information about how to run a subnet and interact with IPC.

### Run a subnet node
To run a subnet node in docker bundled with a daemon and a validator you can use: 
```
./scripts/ipc/run-subnet-docker.sh <API_PORT> <VALIDATOR_LIBP2P_PORT> <SUBNET_ID> <ABSOLUTE_PATH_VAL_KEY>
```
This command runs a new node for a subnet `<SUBNET_ID>` listening at `<API_PORT>` and the validator listening at port `<VALIDATOR_LIBP2P_PORT>`. The last argument is the private key of the address that the validator of the subnet will use as its default wallet. Usually this will be the wallet address from the root that you used to join the subnet. To get the private key for an address into a file you can run the following command against your root (or parent) node:
```
./eudico wallet export --lotus-json <ADDRESS>
```
The result of deploying the subnet node should output something like this:
```
$ ./scripts/ipc/run-subnet-docker.sh 1239 1349 /root/t01002 /home/workspace/pl/lotus/scripts/ipc/src/wallet.key
[*] Running docker container for /root/t01002 in port 1239
[*] Waiting for the daemon to start
Not online yet... (could not get API info for FullNode: could not get api endpoint: API not running (no endpoint))
Not online yet... (could not get API info for FullNode: could not get api endpoint: API not running (no endpoint))
=== (this process may take several minutes until all the infrastructure is deployed)

>>> Subnet /root/t01002 daemon running in container: 196f07aa68fedb48f6c14a0eb2aad3e139438cb515e0b238689b88c8bd440e8a (friendly name: ipc_root_t01002_1239)
>>> Token to /root/t01002 daemon: eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJBbGxvdyI6WyJyZWFkIiwid3JpdGUiLCJzaWduIiwiYWRtaW4iXX0.dR5L79UIi1ZoPlvaW1Taxyc4nujxRw2elCU85yvi7vc
>>> Default wallet: t1cp4q4lqsdhob23ysywffg2tvbmar5cshia4rweq
>>> Subnet subnet validator info:
 t1cp4q4lqsdhob23ysywffg2tvbmar5cshia4rweq@/ip4/127.0.0.1/tcp/1347/p2p/12D3KooWNWKXfw86CswD9yqZgPu5Dp2mhAcFRPXpXFe53aARYtTJJYtTJ
>>> API listening in host port 1239
>>> Validator listening in host port 1349
```
> Bear in mind that this scripts expects the absolute path of the key as an argument, if you use the relative path the script will fail.

This script deploys a daemon and a validator for your subnet, however, it is not started mining automatically (as it was the case for the root node), because one need to first join a subnet to start mining in a subnet. Again, all of the output of the execution of your node will then become useful to configure you IPC agent with the subnet. The resulting container docker will be named `ipc_<SUBNET_ID>_<API_PORT>` (with `/` replaced by `_` so as to yield a valid name). Refer to the [IPC agent](https://github.com/consensus-shipyard/ipc-agent) for documentation on how to run a subnet over this infrastructure.

### Mining in a subnet
To initialize the mining process in a subnet node directly after all of the relevant operations to join a subnet were successfully, you just need to run the following script passing as argument the docker container where your subnet node is running: 
```
./scripts/ipc/mine-subnet.sh <CONTAINER_ID>
```

### Troubleshooting the deployment
If at any point these scripts do not behave as expected, or you are running out of patience while they deploy and you want to check how they are doing, we suggest you:
- Check for running containers using `docker ps`. You may use the listed container name instead of CONTAINER_ID in the steps below.
- Listen to the logs of the underlying container through `docker logs -f <CONTAINER_ID>`
- You attach to the container to interact with it. We multiplex the daemon and validator processes inside the container with `tmux` so you will have to trigger a bash in the container and attach to its tmux session to interact with it: 
```
# attach to container
docker exec -it <CONTAINER_ID> bash
# inside the container's bash
tmux a
```
