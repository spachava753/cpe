#!/bin/sh
set -eu

repo=${CPE_INSTALL_REPO:-spachava753/cpe}
bin=${CPE_INSTALL_BIN:-cpe}
version=${CPE_INSTALL_VERSION:-latest}
install_dir=${CPE_INSTALL_DIR:-"$HOME/.local/bin"}

die() {
  printf '%s\n' "$1" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  darwin|linux) ;;
  *) die "unsupported OS: $os" ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=x86_64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $arch" ;;
esac

need_cmd tar
need_cmd grep

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL "$1" -o "$2"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  die "required command not found: curl or wget"
fi

archive="${bin}_${os}_${arch}.tar.gz"
if [ "$version" = "latest" ]; then
  base_url="https://github.com/${repo}/releases/latest/download"
else
  base_url="https://github.com/${repo}/releases/download/${version}"
fi

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT INT TERM

fetch "${base_url}/${archive}" "${tmp_dir}/${archive}"
fetch "${base_url}/checksums.txt" "${tmp_dir}/checksums.txt"

checksum_line=$(grep "[[:space:]]${archive}$" "${tmp_dir}/checksums.txt" || true)
if [ -z "$checksum_line" ]; then
  die "checksum for ${archive} not found"
fi

if command -v sha256sum >/dev/null 2>&1; then
  printf '%s\n' "$checksum_line" | (cd "$tmp_dir" && sha256sum -c - >/dev/null)
elif command -v shasum >/dev/null 2>&1; then
  printf '%s\n' "$checksum_line" | (cd "$tmp_dir" && shasum -a 256 -c - >/dev/null)
else
  die "required command not found: sha256sum or shasum"
fi

tar -xzf "${tmp_dir}/${archive}" -C "$tmp_dir"
[ -f "${tmp_dir}/${bin}" ] || die "${bin} not found in ${archive}"

mkdir -p "$install_dir"
if command -v install >/dev/null 2>&1; then
  install -m 0755 "${tmp_dir}/${bin}" "${install_dir}/${bin}"
else
  cp "${tmp_dir}/${bin}" "${install_dir}/${bin}"
  chmod 0755 "${install_dir}/${bin}"
fi

printf 'installed %s to %s\n' "$bin" "${install_dir}/${bin}"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) printf 'note: %s is not on PATH\n' "$install_dir" >&2 ;;
esac
