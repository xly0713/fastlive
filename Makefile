.PHONY: all
all: build

# build
.PHONY: build
build:
	go build -o bin/fastlive cmd/main.go

# clean
.PHONY: clean
clean:
	@rm -f bin/*