#!/bin/bash

INDEX=$1

cp -f ./configs/valset_$INDEX.json ~/.lotus-local-net0/mir.validators
cp -f ./configs/valset_$INDEX.json ~/.lotus-local-net1/mir.validators
cp -f ./configs/valset_$INDEX.json ~/.lotus-local-net2/mir.validators
cp -f ./configs/valset_$INDEX.json ~/.lotus-local-net3/mir.validators
