build: oct occ oasm

oct:
	go build -o build/oct .
occ:
	cd tools/occ
	go build -o build/occ .
oasm:
	cd tools/oasm
	go build -o build/oasm .
