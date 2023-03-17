all: docker gen up tests clean

test: gen up tests clean

docker:
	docker build --tag eudico/e2e -f ./Dockerfile ../..

docker_on_mac:
	docker build --tag eudico/e2e -f ./Dockerfile --build-arg FFI_BUILD_FROM_SOURCE=1 ../..

gen:
	go run ./generator/cmd -ip=192.168.10.2 -n=4 -output="./testdata/mir/"

tests:
	go test -v -count=1 -timeout=60m ./tests

up:
	docker compose up -d

down:
	docker compose down

clean:
	rm -rf ./testdata

.PHONY: all docker gen up down tests clean