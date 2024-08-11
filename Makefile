SHELL = bash

build:
	go build

fmt:
	gofmt -s -w -l .

checkfmt:
	test -z "$$(gofmt -l .)"
