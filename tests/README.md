# Eudico End-to-End Tests
TBD

## Docker

To build a docker file for M1 you should use the following:
```shell
docker build -t lotus -f Dockerfile --build-arg FFI_BUILD_FROM_SOURCE=1 .
```