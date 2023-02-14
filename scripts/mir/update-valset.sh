#!/bin/bash

INDEX=$1

cp -f ./mir-config/configs/valset_$INDEX.json ~/.lotus-local-net0/mir.validators
cp -f ./mir-config/configs/valset_$INDEX.json ~/.lotus-local-net1/mir.validators
cp -f ./mir-config/configs/valset_$INDEX.json ~/.lotus-local-net2/mir.validators
cp -f ./mir-config/configs/valset_$INDEX.json ~/.lotus-local-net3/mir.validators
cp -f ./mir-config/configs/valset_$INDEX.json ~/.lotus-local-net4/mir.validators
