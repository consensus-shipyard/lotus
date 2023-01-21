#!/bin/bash

new_validator="f1by6b6zpawpvw34yzunaoxnumfvi4aokl7bmvncy@/ip4/127.0.0.1/tcp/62725/p2p/12D3KooWSCU1PKHpCgCj6jqugQQyVyHZz7noerok5Gs5gQZWFkJy"
echo $new_validator >> ~/.lotus-local-net0/mir.validators
echo $new_validator >> ~/.lotus-local-net1/mir.validators
echo $new_validator >> ~/.lotus-local-net2/mir.validators
echo $new_validator >> ~/.lotus-local-net3/mir.validators
