#!/bin/bash

set -e

# Start k8s-ci before run this command
# docker run --privileged -v /tmp/z:/var/lib/containers -it --rm -v $PWD:/work -w /work byjg/k8s-ci

if [ -z "$DOCKER_USERNAME" ]  || [ -z "$DOCKER_PASSWORD" ] || [ -z "$DOCKER_REGISTRY" ]
then
  echo You need to setup \$DOCKER_USERNAME, \$DOCKER_PASSWORD and \$DOCKER_REGISTRY before run this command.
  exit 1
fi

buildah login --username $DOCKER_USERNAME --password $DOCKER_PASSWORD $DOCKER_REGISTRY

podman run --rm --events-backend=file --cgroup-manager=cgroupfs --privileged docker://multiarch/qemu-user-static --reset -p yes

IMAGE="byjg/static-httpserver"

# The version stamped into the binary. The build context has no .git, so the
# Makefile's git describe finds nothing and it has to be passed in, the same way
# the GitHub workflow does it.
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo dev)

# Image tags follow the same scheme as the workflow: sitting on a release tag
# publishes :X.Y.Z, :X.Y and :latest, anything else publishes :latest alone.
RELEASE_TAG=$(git describe --exact-match --tags 2>/dev/null || true)
if [ -n "$RELEASE_TAG" ]; then
  SEMVER=${RELEASE_TAG#v}
  TAGS=("$SEMVER" "${SEMVER%.*}" "latest")
else
  TAGS=("latest")
fi

echo "Building $IMAGE version $VERSION, tags: ${TAGS[*]}"

MANIFEST="$IMAGE:${TAGS[0]}"

# A manifest left behind by an earlier run makes create fail.
buildah manifest rm "$MANIFEST" 2>/dev/null || true
buildah manifest create "$MANIFEST"

buildah bud --arch arm64 --os linux --build-arg VERSION="$VERSION" --iidfile /tmp/iid-arm64 -f Dockerfile -t "$IMAGE:${TAGS[0]}-arm64" .
buildah bud --arch amd64 --os linux --build-arg VERSION="$VERSION" --iidfile /tmp/iid-amd64 -f Dockerfile -t "$IMAGE:${TAGS[0]}-amd64" .

buildah manifest add "$MANIFEST" --arch arm64 --os linux --variant v8 "$(cat /tmp/iid-arm64)"
buildah manifest add "$MANIFEST" --arch amd64 --os linux "$(cat /tmp/iid-amd64)"

for tag in "${TAGS[@]}"; do
  buildah manifest push --all --format v2s2 "$MANIFEST" "docker://$IMAGE:$tag"
done

