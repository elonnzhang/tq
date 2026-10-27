#!/bin/sh
# Builds tq and tqmd for each platform there are releases for, and packs
# each platform's pair, with the README, into dist/tq_VERSION_OS_ARCH.tar.gz,
# or .zip for Windows. Their SHA-256 sums go in dist/tq_VERSION_checksums.txt.
# VERSION is the one tq --version prints.
set -eu

platforms="darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"

cd "$(dirname "$0")/.."
root=$(pwd)
version=$(sed -n 's/^const version = "\(.*\)"$/\1/p' internal/cli/cli.go)
if [ -z "$version" ]; then
	echo "dist.sh: no version in internal/cli/cli.go" >&2
	exit 1
fi
mkdir -p dist
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for p in $platforms; do
	os=${p%/*}
	arch=${p#*/}
	name=tq_${version}_${os}_${arch}
	ext=
	if [ "$os" = windows ]; then
		ext=.exe
	fi
	mkdir "$work/$name"
	for cmd in tq:. tqmd:./cmd/tqmd; do
		CGO_ENABLED=0 GOOS=$os GOARCH=$arch go build -trimpath -ldflags='-s -w' \
			-o "$work/$name/${cmd%%:*}$ext" "${cmd#*:}"
	done
	cp README.md "$work/$name/"
	if [ "$os" = windows ]; then
		rm -f "dist/$name.zip" # zip adds to an archive that is there
		(cd "$work" && zip -qr "$root/dist/$name.zip" "$name")
	else
		tar -C "$work" -czf "dist/$name.tar.gz" "$name"
	fi
done

cd dist
if command -v sha256sum >/dev/null; then
	sha256sum tq_"$version"_*.tar.gz tq_"$version"_*.zip
else
	shasum -a 256 tq_"$version"_*.tar.gz tq_"$version"_*.zip
fi >"tq_${version}_checksums.txt"
ls -l tq_"$version"_*
