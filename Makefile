ASSET = bin/vncd
SRC = $(shell find . -name *.go)
$(ASSET): $(dir $(ASSET)) $(SRC)
	docker run -it --rm \
						 -v $(shell pwd)/src:/go/src \
						 -v $(dir $(abspath $(ASSET))):/output \
						 -e CGO_ENABLED=0 \
						 -e GOOS=linux \
						 -w /go/src/vncproxy/cmd \
						 golang:latest go build \
						 		-a -installsuffix cgo \
								-o /output/$(notdir $(ASSET)) .

$(dir $(ASSET)):
	mkdir -p $(dir $(ASSET))

clean:
	rm -rf $(dir $(ASSET))
