build: oct occ oasm mkfs

oct:
	go build -o build/oct .
occ:
	go build -o build/occ ./tools/occ
oasm:
	go build -o build/oasm ./tools/oasm
mkfs:
	go build -o build/mkfs ./tools/mkfs
