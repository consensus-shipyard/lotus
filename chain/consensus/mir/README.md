# Mir Consensus

The code in this package integrates the Mir framework with the implementation over Mir
of a pBFT consensus. This consensus can be used as an alternative consensus to FilecoinEC
in Lotus.

## Requirements
Lotus and [Mir](https://github.com/filecoin-project/mir) requirements must be satisfied.
The most important one is Go1.18+, with that you are probably able to figure out the rest yourself.

## Install
To use Mir in lotus, you need to compile the code with `make spacenet`. This will output
`lotus`, `lotus-miner` for their use with Mir and a `mir-validator` process. This last process
is the one that needs to be run by validators to produce new blocks in the network.
```
git clone git@github.com:filecoin-project/lotus.git
cd lotus
git submodule update --init --recursive
make spacenet
```

## Run

The easiest way to run a three-nodes network is to leverage the scripts provided in `./scripts/mir`. To run the network you just need to run the following scripts in different terminals:
```
# Terminal 1 (daemon for node 0)
./scripts/mir/daemon.sh 0
# Terminal 2 (daemon for node 1)
./scripts/mir/daemon.sh 1
# Terminal 3 (daemon for node 2)
./scripts/mir/daemon.sh 2
# Terminal 4 (daemon for node 0)
./scripts/mir/validator.sh 0
# Terminal 5 (daemon for node 1)
./scripts/mir/validator.sh 1
# Terminal 6 (daemon for node 2)
./scripts/mir/validator.sh 2
```
If you rather deploy your own custom network, follow the steps provided in the [mir validator README](../../../cmd/mir-validator) to learn how to configure and deploy your own validators,
and how to set up the membership list.

### Automated 4-node network
If you don't even want to know what is happening under-the-hood, and you just want to run a 4-node network fast, run `./scripts/mir/4-node-net.sh`.

## Reconfiguration

A configuration consists of `configuration_number` and `validators`.
The current subnet configuration is stored in `mir.validators` file.
Please take a look at an example of a configuration [here](/scripts/mir/mir-config/node0/mir.validators).

A user is responsible for setting the correct configuration number in the configuration file.
For example, if the current configuration number is `0`, then
to add a new validator into the subnet, all users should set `configuration_number` to `1` and add
the new validator into `validators`.
If the majority of the current validators agree on this new configuration,
then the new validator will be added into the subnet.

To run a demo version of a subnet with reconfiguration take a look at [this](/scripts/mir/README.md) document.
