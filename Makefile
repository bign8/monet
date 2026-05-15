.PHONY: build run clean

build: monet
	sudo setcap cap_net_raw=eip monet

monet: $(shell find . -name '*.go')
	go build -o monet .

run: build
	./monet

clean:
	rm -f monet
