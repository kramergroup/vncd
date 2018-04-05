GITHUB_API=https://api.github.com/repos/kramergroup/vncd
GITHUB_USER=DenisKramer

ASSET = bin/vncd

SRC = $(shell find . -name *.go)
$(ASSET): $(dir $(ASSET)) $(SRC)
	docker run -it --rm \
						 -v $(shell pwd):/go/src/github.com/kramergroup/vncd \
						 -v $(dir $(abspath $(ASSET))):/output \
						 -e CGO_ENABLED=0 \
						 -e GOOS=linux \
						 -w /go/src/github.com/kramergroup/vncd/cmd \
						 golang:latest bash -c "go get .. && go build -a -installsuffix cgo -o /output/$(notdir $(ASSET))"

$(dir $(ASSET)):
	mkdir -p $(dir $(ASSET))

clean:
	rm -rf $(dir $(ASSET))

.PHONY: release
release: create-release upload-release

.PHONY: create-release
create-release: VERSION=$(shell jq -r .tag_name release/release.json)
create-release:
	git tag -a ${VERSION} -m "Release ${VERSION}"
	git push
	mkdir -p release/vncd-${VERSION}
	cp bin/vncd assets/startvnc.sh assets/install.sh release/vncd-${VERSION}
	cd release/vncd-${VERSION} && zip ../vncd-${VERSION}.zip *
	cd release/vncd-${VERSION} && tar cvzf ../vncd-${VERSION}.tar.gz *
	rm -rf release/vncd-${VERSION}
	curl --user ${GITHUB_USER} -H "Content-Type: application/json" -X POST -d @release/release.json ${GITHUB_API}/releases

.PHONY: upload-release
upload-release: VERSION=$(shell jq -r .tag_name release/release.json)
upload-release: ID=$(shell curl https://api.github.com/repos/kramergroup/vncd/releases\?tag_name\=v0.1.0 | jq '.[0].id')
upload-release:
	curl --user ${GITHUB_USER} -H "Content-Type: application/zip" -X POST --data-binary "@release/vncd-${VERSION}.tar.gz" \
			https://uploads.github.com/repos/kramergroup/vncd/releases/${ID}/assets?name=vncd-${VERSION}.tar.gz
	curl --user ${GITHUB_USER} -H "Content-Type: application/tar+gzip" -X POST --data-binary "@release/vncd-${VERSION}.tar.gz" \
		  https://uploads.github.com/repos/kramergroup/vncd/releases/${ID}/assets?name=vncd-${VERSION}.zip
