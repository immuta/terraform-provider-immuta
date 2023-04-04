#!/usr/bin/env bash
#
# This script builds the application from source for multiple platforms.

# Get the parent directory of where this script is.
SOURCE="${BASH_SOURCE[0]}"
while [ -h "$SOURCE" ]; do SOURCE=$(readlink "$SOURCE"); done
DIR="$(cd -P "$(dirname "$SOURCE")/.." && pwd)"

# Change into that directory
cd "$DIR"

# Get the short Git commit
GIT_COMMIT=$(git rev-parse --short=7 HEAD)

# Determine the arch/os combos we're building for
GOOS_LIST=${GOOS_LIST:-linux darwin}
GOARCH_LIST=${GOARCH_LIST:-"amd64"}

# Cleanup old builds
echo "==> Removing old directory..."
rm -f bin/*
rm -rf pkg/*
rm -rf releases/*
mkdir -p bin/ pkg/ releases/

# instruct go build to build statically linked binaries
export CGO_ENABLED=0

# Allow LD_FLAGS to be appended during development compilations.
# We may want in the future to get rid of development flags using:
#   -s: to omit the symbol table and debug information
#   -w: to omit the DWARF symbol table
LD_FLAGS="-X github.com/instacart/terraform-provider-immuta/version.ProviderVersion=${GIT_COMMIT} $LD_FLAGS"

# Build!
echo "==> Building..."
for GOOS_ITEM in $GOOS_LIST; do
  for GOARCH_ITEM in $GOARCH_LIST; do
    echo " --> ${GOOS_ITEM}_${GOARCH_ITEM}"
    GOOS=${GOOS_ITEM} GOARCH=${GOARCH_ITEM} go build \
      -mod vendor \
      -ldflags "${LD_FLAGS}" \
      -o "pkg/${GOOS_ITEM}_${GOARCH_ITEM}/terraform-provider-immuta" \
      .

    echo "    -> Releasing ${GOOS_ITEM}_${GOARCH_ITEM}.tar.gz..."
    tar -C "pkg/${GOOS_ITEM}_${GOARCH_ITEM}" -czf "releases/${GOOS_ITEM}_${GOARCH_ITEM}.tar.gz" .
  done
done

# Move all the compiled things to the $GOPATH/bin
GOPATH=${GOPATH:-$(go env GOPATH)}
OLDIFS=$IFS
IFS=: MAIN_GOPATH=($GOPATH)
IFS=$OLDIFS

# Create GOPATH/bin if it doesn't exist
if [ ! -d $MAIN_GOPATH/bin ]; then
  echo "==> Creating GOPATH/bin directory..."
  mkdir -p $MAIN_GOPATH/bin
fi

# Copy our OS/ARCH specific binary to the GOPATH/bin
DEV_PLATFORM="./pkg/$(go env GOOS)_$(go env GOARCH)"
if [[ -d $DEV_PLATFORM ]]; then
  for F in $(find ${DEV_PLATFORM} -mindepth 1 -maxdepth 1 -type f); do
    cp ${F} bin/
    cp ${F} ${MAIN_GOPATH}/bin/
  done
fi

# Done!
echo
echo "==> Results:"
ls -hl bin/
ls -hl releases/
