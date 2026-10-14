#!/usr/bin/env bash
# Builds the mapd image from the repository root and pushes it to the ECR
# repository Terraform created, tagged with the current git commit. Prints
# the tag, for terraform apply -var image_tag=<tag>.
#
# Usage: deploy/push.sh   (run after terraform has created the repository)
set -euo pipefail

DEPLOY="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$DEPLOY")"

REPO="$(terraform -chdir="$DEPLOY" output -raw ecr_repository_url)"
REGION="$(cut -d. -f4 <<<"$REPO")"
TAG="$(git -C "$ROOT" rev-parse --short HEAD)"
# A build with uncommitted changes must not pass for the commit.
if [[ -n "$(git -C "$ROOT" status --porcelain)" ]]; then
    TAG="$TAG-dirty-$(date +%Y%m%d%H%M%S)"
fi

aws ecr get-login-password --region "$REGION" \
    | docker login --username AWS --password-stdin "${REPO%%/*}"

# arm64, to match the task definition's runtime platform.
docker build --platform linux/arm64 -t "$REPO:$TAG" "$ROOT"
docker push "$REPO:$TAG"

echo
echo "Pushed $REPO:$TAG"
echo "Deploy it with: terraform -chdir=deploy apply -var image_tag=$TAG"
