all: docker gen up tests clean

test: gen up run-tests-locally clean

test-ci: gen up run-tests-on-ci down

docker:
	docker build --tag eudico/e2e -f ./Dockerfile ..

docker_on_mac:
	docker build --tag eudico/e2e -f ./Dockerfile --build-arg FFI_BUILD_FROM_SOURCE=1 ..

gen:
	go run ./generator/cmd -manifest="./networks/simple.toml"

run-tests-locally:
	go test -v -count=1 -timeout=20m ./...

run-tests-on-ci:
	go test -c -o tests.app ./tests
	sudo ./tests.app -test.v

up:
	BUILDKIT_PROGRESS=plain docker compose up -d

down:
	docker compose down

clean:
	rm -rf ./testdata/mir/node*

.PHONY: all docker gen up down tests clean