# Eudico End-to-End Tests
Creates and run end-to-end test suites using in Docker Compose.

## Docker

To build a docker file for M1 you should use the following:
```shell
docker build -t lotus -f Dockerfile --build-arg FFI_BUILD_FROM_SOURCE=1 .
```