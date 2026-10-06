#!/bin/sh
# Insta Deploy installer.
#
# Installs the control plane (dashboard, API, database) with Docker
# Compose and starts it. Review this script before running it; it needs
# Docker with Compose v2.24+ and git.
#
#   sh install.sh                     # from a checkout: installs in place
#   INSTA_DEPLOY_REPO=<git url> sh install.sh   # clones into ./insta-deploy
#   INSTALL_DIR=/opt/insta-deploy ...           # choose where
set -eu

REPO="${INSTA_DEPLOY_REPO:-}"
DIR="${INSTALL_DIR:-./insta-deploy}"

say() { printf '\033[1m==> %s\033[0m\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || die "Docker is not installed. See https://docs.docker.com/engine/install/"
docker compose version >/dev/null 2>&1 || die "Docker Compose v2 is not available (docker compose version failed)."
command -v git >/dev/null 2>&1 || die "git is not installed."

if [ -f docker-compose.yml ] && [ -d deploy ] && [ -d agent ] && [ -z "$REPO" ]; then
  DIR=.
  say "Installing from this checkout"
elif [ -d "$DIR/.git" ]; then
  say "Updating $DIR"
  git -C "$DIR" pull --ff-only
else
  [ -n "$REPO" ] || die "set INSTA_DEPLOY_REPO to the Insta Deploy Git repository URL (or run this from a checkout)."
  say "Downloading Insta Deploy into $DIR"
  git clone --depth 1 "$REPO" "$DIR"
fi
cd "$DIR"

say "Building and starting (this takes a few minutes the first time)"
docker compose up -d --build

say "Done!"
echo "  Dashboard: http://localhost:3000"
echo "  API docs:  http://localhost:8080/docs"
echo
echo "Next: open the dashboard and create your account. The setup guide"
echo "walks you through connecting Pangolin and your first machine."
