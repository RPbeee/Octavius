build: oct occ oasm

oct:
	go build -o build/oct .
occ:
	go build -o build/occ ./tools/occ
oasm:
	go build -o build/oasm ./tools/oasm
